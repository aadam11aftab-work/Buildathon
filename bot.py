"""FLOOR PROBE bot (not meant to win).

Purpose: measure the practice score of an almost-logic-free bot, so we can tell how much of our real
score comes "for free" (zone kills + easy house bots) and how much our real logic adds.

Behaviour, on purpose as dumb as possible:
  1. walk to a spot inside the NEXT zone circle,
     (A* on the map, same code as v2; set NAV = False for raw straight-line walking),
  2. stand still there (silent: steps only sound above 5 m/s),
  3. never loot, never farm, never chase, never dodge, never pick weapons, never take cubes' detours,
  4. shoot back ONLY if we were damaged in the last 3 s and an enemy mech is visible,
  5. a tiny stuck-breaker (random heading + jump for 1 s) so one wall does not decide the whole score.

Telemetry goes to stderr only and is size-capped: a "TEL beat" every 15 s and one "TEL end" line.
"""
import json
import math
import os
import random
import sys
import time
from array import array

import mechfight as mf

# NAV=True: use the map + A* to reach the spot (so walls do not decide the score).
# NAV=False: raw straight-line walking, the true 'floor'.
NAV = True

PI = math.pi
hypot = math.hypot


def wrap(a):
    return (a + PI) % (2 * PI) - PI


def clamp(v, a, b):
    return a if v < a else b if v > b else v


# ----------------------------------------------------------------------------- map + A* (same code as v2)
class World:
    def __init__(self, mapdir):
        self.ok = False
        base = os.path.join(mapdir, "v2")
        with open(os.path.join(base, "world.json")) as f:
            wj = json.load(f)
        tr = wj["terrain"]
        self.nx, self.ny = tr["nx"], tr["ny"]
        self.minx, self.miny, self.st = tr["minX"], tr["minY"], tr["step"]
        g = array("h")
        with open(os.path.join(base, tr["file"]), "rb") as f:
            g.frombytes(f.read())
        if sys.byteorder != "little":
            g.byteswap()
        self.g = g
        self.bh = array("f", [-1000.0]) * (self.nx * self.ny)
        for p in wj["prisms"]:
            if p["kind"] == "bridge":
                continue
            try:
                self._fill(p)
            except Exception:
                pass
        b = wj["bounds"]
        self.bx0, self.bx1, self.by0, self.by1 = b["minX"], b["maxX"], b["minY"], b["maxY"]
        try:
            self.build_grid()
        except Exception:
            self.blk = None
        self.ok = True

    def _fill(self, p):
        top = p["top"]
        edges = []
        rings = [p["ring"]]
        for h in p.get("holes") or []:
            rings.append(h)
        ymin, ymax = 1e9, -1e9
        for ring in rings:
            n = len(ring) // 2
            for k in range(n):
                x0, y0 = ring[2 * k], ring[2 * k + 1]
                k2 = (k + 1) % n
                x1, y1 = ring[2 * k2], ring[2 * k2 + 1]
                if y0 != y1:
                    edges.append((x0, y0, x1, y1))
                ymin = min(ymin, y0)
                ymax = max(ymax, y0)
        st, minx, miny, nx, ny, bh = self.st, self.minx, self.miny, self.nx, self.ny, self.bh
        j0 = max(0, int(math.ceil((ymin - miny) / st)))
        j1 = min(ny - 1, int(math.floor((ymax - miny) / st)))
        for j in range(j0, j1 + 1):
            yc = miny + j * st
            xs = []
            for x0, y0, x1, y1 in edges:
                if (y0 <= yc < y1) or (y1 <= yc < y0):
                    xs.append(x0 + (yc - y0) * (x1 - x0) / (y1 - y0))
            xs.sort()
            row = j * nx
            for q in range(0, len(xs) - 1, 2):
                i0 = max(0, int(math.ceil((xs[q] - minx) / st)))
                i1 = min(nx - 1, int(math.floor((xs[q + 1] - minx) / st)))
                for i in range(i0, i1 + 1):
                    if bh[row + i] < top:
                        bh[row + i] = top

    def build_grid(self, cs=3.0):
        import heapq  # noqa: F401
        self.cs = cs
        self.gx0, self.gy0 = self.bx0 + 3, self.by0 + 3
        self.gw = int((self.bx1 - self.bx0 - 6) / cs)
        self.gh = int((self.by1 - self.by0 - 6) / cs)
        blk = bytearray(self.gw * self.gh)
        bh, g = self.bh, self.g
        pts = ((0.5, 0.5), (0.2, 0.2), (0.8, 0.2), (0.2, 0.8), (0.8, 0.8))
        for j in range(self.gh):
            y0 = self.gy0 + j * cs
            for i in range(self.gw):
                x0 = self.gx0 + i * cs
                nb = 0
                cen = 0
                for q, (fx, fy) in enumerate(pts):
                    k = self.idx(x0 + fx * cs, y0 + fy * cs)
                    if bh[k] > g[k] * 0.01 + 1.3:
                        nb += 1
                        if q == 0:
                            cen = 1
                blk[j * self.gw + i] = 1 if (cen or nb >= 2) else 0
        self.blk = blk

    def cell(self, x, y):
        i = int((x - self.gx0) / self.cs)
        j = int((y - self.gy0) / self.cs)
        return clamp(i, 0, self.gw - 1), clamp(j, 0, self.gh - 1)

    def nearest_open(self, i, j):
        if not self.blk[j * self.gw + i]:
            return i, j
        for r in range(1, 12):
            for di in range(-r, r + 1):
                for dj in range(-r, r + 1):
                    if max(abs(di), abs(dj)) != r:
                        continue
                    a, b = i + di, j + dj
                    if 0 <= a < self.gw and 0 <= b < self.gh and not self.blk[b * self.gw + a]:
                        return a, b
        return i, j

    def plan(self, sx, sy, gx, gy, max_nodes=60000):
        import heapq
        gw, gh, blk, cs = self.gw, self.gh, self.blk, self.cs
        si, sj = self.nearest_open(*self.cell(sx, sy))
        ti, tj = self.nearest_open(*self.cell(gx, gy))
        start, goal = sj * gw + si, tj * gw + ti
        if start == goal:
            return [(gx, gy)]
        gd = self.g
        dist = {start: 0.0}
        prev = {}
        W_ = 1.5
        h0 = max(abs(si - ti), abs(sj - tj)) + 0.414 * min(abs(si - ti), abs(sj - tj))
        pq = [(h0 * W_, start)]
        closed = set()
        best, besth = start, h0
        n = 0
        SQ = 1.4142
        while pq and n < max_nodes:
            _, cur = heapq.heappop(pq)
            if cur in closed:
                continue
            closed.add(cur)
            n += 1
            if cur == goal:
                best = cur
                break
            ci, cj = cur % gw, cur // gw
            dcur = dist[cur]
            gcur = gd[self.idx(self.gx0 + (ci + 0.5) * cs, self.gy0 + (cj + 0.5) * cs)] * 0.01
            for di, dj in ((1, 0), (-1, 0), (0, 1), (0, -1), (1, 1), (1, -1), (-1, 1), (-1, -1)):
                a, b = ci + di, cj + dj
                if a < 0 or b < 0 or a >= gw or b >= gh:
                    continue
                nb = b * gw + a
                if blk[nb] or nb in closed:
                    continue
                if di and dj and (blk[cj * gw + a] or blk[b * gw + ci]):
                    continue  # no corner cutting
                gn = gd[self.idx(self.gx0 + (a + 0.5) * cs, self.gy0 + (b + 0.5) * cs)] * 0.01
                if abs(gn - gcur) > 3.2:
                    continue
                nd = dcur + (SQ if di and dj else 1.0) + abs(gn - gcur) * 0.3
                if nd < dist.get(nb, 1e18):
                    dist[nb] = nd
                    prev[nb] = cur
                    hh = max(abs(a - ti), abs(b - tj)) + 0.414 * min(abs(a - ti), abs(b - tj))
                    if hh < besth:
                        besth, best = hh, nb
                    heapq.heappush(pq, (nd + W_ * hh, nb))
        path = []
        c = best
        while c != start:
            path.append((self.gx0 + (c % gw + 0.5) * cs, self.gy0 + (c // gw + 0.5) * cs))
            c = prev[c]
        path.reverse()
        if best == goal:
            path.append((gx, gy))
        return path

    def line_free(self, ax, ay, bx, by, z):
        d = hypot(bx - ax, by - ay)
        n = int(d / 1.5) + 1
        for k in range(1, n + 1):
            f = k / n
            px, py = ax + (bx - ax) * f, ay + (by - ay) * f
            if not self.free(px, py, z, 3.0):
                return False
            # clearance: sides
        return True

    def idx(self, x, y):
        i = int((x - self.minx) / self.st + 0.5)
        j = int((y - self.miny) / self.st + 0.5)
        if i < 0:
            i = 0
        elif i >= self.nx:
            i = self.nx - 1
        if j < 0:
            j = 0
        elif j >= self.ny:
            j = self.ny - 1
        return j * self.nx + i

    def ground(self, x, y):
        return self.g[self.idx(x, y)] * 0.01

    def roof(self, x, y):
        return self.bh[self.idx(x, y)]

    def free(self, x, y, z, s):
        if not (self.bx0 + 3 < x < self.bx1 - 3 and self.by0 + 3 < y < self.by1 - 3):
            return False
        k = self.idx(x, y)
        if self.bh[k] > z + 1.3:
            return False
        if self.g[k] * 0.01 - z > 1.19 * s + 1.5:
            return False
        return True

    def los(self, ax, ay, az, bx, by, bz):
        dx, dy = bx - ax, by - ay
        n = int(hypot(dx, dy) / 1.5)
        bh, g = self.bh, self.g
        for k in range(1, n):
            f = k / n
            i = self.idx(ax + dx * f, ay + dy * f)
            z = az + (bz - az) * f
            if bh[i] > z or g[i] * 0.01 > z:
                return False
        return True


# ----------------------------------------------------------------------------- bot
class Bot:
    name = "floor-probe"

    def __init__(self, hello):
        self.cfg = hello["config"]
        self.slot = hello.get("slot", 0)
        self.rng = random.Random(hello.get("seed", 0))
        self.camp_ang = self.rng.uniform(-PI, PI)   # personal spot angle, fixed for the match
        self.camp_frac = 0.25                       # stand at 25% of the radius from the centre
        self.last_dmg_t = -99.0
        self.hist = []                              # (t, x, y) for the stuck check
        self.escape_until = 0.0
        self.escape_dir = 0.0
        self.dmg = []                               # last 10 damaged events (telemetry)
        self.dmg_by = {}
        self.last_t = 0.0
        self.last_state = None
        self.beat = -99.0
        self.mode = "start"
        self.world = None
        self.path = None
        self.path_goal = (0.0, 0.0)
        self.path_t = -99.0
        self.nav_off_until = 0.0
        if NAV:
            for d in (hello["map"]["dir"], "/arena"):
                try:
                    self.world = World(d)
                    break
                except Exception as e:                  # the map is optional: fall back to straight lines
                    mf.log("map load failed:", d, repr(e))

    # ------------------------------------------------------------------ helpers
    def zone_target(self, obs):
        z = obs["zone"]
        if obs["t"] >= z["shrink_start"] - 10:       # head for the circle that is about to apply
            c, r = z["next_center"], z["next_radius"]
        else:
            c, r = z["center"], z["radius"]
        k = self.camp_frac * r
        return (c[0] + k * math.cos(self.camp_ang), c[1] + k * math.sin(self.camp_ang)), c, r

    def aim_at(self, me, tp, tv, speed):
        x, y, z = me["pos"]
        muzzle = me["stats"].get("muzzle_height", 7.0)
        tt = 0.0
        px, py, pz = tp
        for _ in range(3):                           # simple lead
            px, py, pz = tp[0] + tv[0] * tt, tp[1] + tv[1] * tt, tp[2]
            d = math.sqrt((px - x) ** 2 + (py - y) ** 2 + (pz - z - muzzle) ** 2)
            tt = d / max(speed, 1.0) + 0.075
        yaw = math.atan2(py - y, px - x)
        pitch = math.atan2(pz - (z + muzzle), hypot(px - x, py - y))
        m = self.cfg["mech"]
        pitch = max(m["min_pitch"], min(m["max_pitch"], pitch))
        return yaw, pitch

    def waypoint(self, x, y, z, goal, t):
        """Next point to walk to: straight if clear, else follow an A* path (synchronous, replanned rarely)."""
        w = self.world
        gd = hypot(goal[0] - x, goal[1] - y)
        if w is None or getattr(w, "blk", None) is None or gd < 6 or t < self.nav_off_until:
            return goal
        if gd < 45 and w.line_free(x, y, goal[0], goal[1], z):
            self.path = None
            return goal
        need = (self.path is None or hypot(self.path_goal[0] - goal[0], self.path_goal[1] - goal[1]) > 8
                or t - self.path_t > 5.0 or (len(self.path) <= 1 and gd > 15) or t < self.escape_until)
        if need:
            t0 = time.perf_counter()
            try:
                self.path = w.plan(x, y, goal[0], goal[1], max_nodes=30000)
            except Exception:
                self.path = None
            self.path_goal, self.path_t = goal, t
            if time.perf_counter() - t0 > 0.03:         # never risk the 50 ms budget
                self.nav_off_until = t + 10.0
        if not self.path:
            return goal
        while len(self.path) > 1 and hypot(self.path[0][0] - x, self.path[0][1] - y) < 4.0:
            self.path.pop(0)
        tgt = self.path[0]
        for k in range(min(len(self.path), 10)):
            px, py = self.path[k]
            if hypot(px - x, py - y) > 30:
                break
            if w.line_free(x, y, px, py, z):
                tgt = self.path[k]
        return tgt

    # ------------------------------------------------------------------ main
    def act(self, obs):
        me = obs["self"]
        x, y, z = me["pos"]
        t = obs["t"]
        self.last_t = t
        act = {"tick": obs["tick"]}

        for e in obs["events"]:
            if e.get("type") == "damaged":
                if e.get("weapon") != "zone":
                    self.last_dmg_t = t
                self.dmg.append([round(t, 1), round(e.get("amount", 0)), e.get("weapon"), e.get("by"),
                                 1 if e.get("rear") else 0])
                if len(self.dmg) > 10:
                    self.dmg.pop(0)
                w = e.get("weapon", "?")
                self.dmg_by[w] = self.dmg_by.get(w, 0) + e.get("amount", 0)

        spot, zc, zr = self.zone_target(obs)
        dist = hypot(spot[0] - x, spot[1] - y)
        outside = (not me["in_zone"]) or hypot(x - zc[0], y - zc[1]) > zr - 8
        move = (0.0, 0.0)
        boost = False
        jump = False
        self.mode = "stand"

        # --- walk to the spot (straight line)
        if dist > 6.0:
            wp = self.waypoint(x, y, z, spot, t)
            wd = hypot(wp[0] - x, wp[1] - y) or 1.0
            move = ((wp[0] - x) / wd, (wp[1] - y) / wd)
            boost = outside and me["energy"] > 5
            self.mode = "walk"

        # --- stuck breaker
        self.hist.append((t, x, y))
        while self.hist and t - self.hist[0][0] > 1.5:
            self.hist.pop(0)
        if (move != (0.0, 0.0) and self.hist and t - self.hist[0][0] > 1.2
                and hypot(x - self.hist[0][1], y - self.hist[0][2]) < 2.0 and t > self.escape_until):
            self.escape_until = t + 1.0
            self.escape_dir = self.rng.uniform(-PI, PI)
            self.hist = []
        if t < self.escape_until:
            move = (math.cos(self.escape_dir), math.sin(self.escape_dir))
            jump = True
            boost = me["energy"] > 25
            self.mode = "unstick"

        # --- shoot back only if recently damaged and an enemy mech is visible
        fire = [False, False]
        aim = None
        enemies = [m for m in obs["mechs"] if m["kind"] == "mech"]
        gun = me["weapons"][0] if me["weapons"] and me["weapons"][0] and me["weapons"][0]["kind"] == "gun" else None
        if gun and enemies and t - self.last_dmg_t < 3.0:
            tgt = min(enemies, key=lambda m: hypot(m["pos"][0] - x, m["pos"][1] - y))
            tp = tgt["pos"]
            tcz = tp[2] + 4.5
            yaw, pitch = self.aim_at(me, (tp[0], tp[1], tcz), tgt.get("vel", [0, 0, 0]), gun["stats"]["speed"])
            aim = [yaw, pitch]
            d = hypot(tp[0] - x, tp[1] - y)
            if gun.get("ammo", 1) <= 0:
                act["reload"] = True
            elif d < gun["stats"]["range"] * 0.9 and abs(wrap(yaw - me["aim"][0])) < 0.1:
                fire[0] = True
            self.mode = "shoot-back" if self.mode == "stand" else self.mode
        elif gun and gun.get("ammo", 12) < 6 and not enemies:
            act["reload"] = True                    # top up while idle

        if aim is None:
            aim = [math.atan2(move[1], move[0]), 0.0] if hypot(*move) > 0.1 else list(me["aim"])

        act["move"] = [round(move[0], 4), round(move[1], 4)]
        act["aim"] = [round(aim[0], 4), round(aim[1], 4)]
        act["fire"] = fire
        act["boost"] = bool(boost)
        act["jump"] = bool(jump)

        self.last_state = dict(t=round(t, 1), hp=round(me["hp"]), pos=[round(x), round(y)], in_zone=me["in_zone"],
                               alive=obs["alive"], mode=self.mode, d_spot=round(dist))
        if t - self.beat >= 15.0:
            self.beat = t
            mf.log("TEL beat " + str(self.last_state)[:300])
        mf.draw.label(self.mode)
        return act

    def on_end(self, msg):
        last = self.dmg[-1] if self.dmg else None
        mf.log("TEL end place=%s kills=%s death_t=%s killer=%s weapon=%s dmg_by=%s last_dmg=%s state=%s name=%s" % (
            msg.get("place"), msg.get("kills"), round(self.last_t, 1), last[3] if last else None,
            last[2] if last else None, {k: round(v) for k, v in self.dmg_by.items()}, self.dmg,
            self.last_state, self.name))


mf.run(Bot)
