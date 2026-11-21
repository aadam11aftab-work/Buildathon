"""Mech Royale bot: loot early, stay inside the zone, face enemies, strafe/dodge, shoot with lead."""
import json
import math
import os
import random
import sys
import threading
import time
from array import array

import mechfight as mf

PI = math.pi
hypot = math.hypot


def wrap(a):
    return (a + PI) % (2 * PI) - PI


def clamp(v, a, b):
    return a if v < a else b if v > b else v


# ----------------------------------------------------------------------------- map
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
UPGRADE_PRIORITY = [
    "plating", "armor", "nanites", "gyros", "gun_damage", "lightning_chain", "laser_damage",
    "gun_rate", "servos", "gun_mag", "laser_charge", "lightning_efficiency", "lightning_range",
    "capacitor", "reactor", "sensors", "jets", "gun_velocity",
]
P = dict(want_l=22.0, want_g=40.0, want_lz=55.0, weak=0.35, amp=0.9, flip_lo=0.5, flip_hi=1.3, hold_hp=0.6, tgt_hp0=0.6, tgt_hpw=1.0, explore=1, explore_until=240.0, explore_hp=0.6, explore_step=70.0, npc_r=60.0, crate_r=70.0, npc_hp=0.5, cube_r=30.0, cube_w=1.6, multi_n=99, multi_hp=0.7, adv_d=70.0, camp=0.5, dodge_t=0.9, strafe_keep=0.7)
try:
    P.update(json.loads(os.environ.get("BOT_PARAMS", "{}")))
except Exception:
    pass
WEAPON_SCORE = {"lightning": 3.0, "laser": 2.0, "mortar": 0.0, "gun": 1.0}


class Bot:
    name = "survivor-v3"

    def __init__(self, hello):
        cfg = hello["config"]
        self.cfg = cfg
        self.rng = random.Random(hello.get("seed", 0))
        self.slot = hello.get("slot", 0)
        self.world = None
        for d in (hello["map"]["dir"], "/arena"):
            try:
                self.world = World(d)
                break
            except Exception as e:  # map is an optimisation; play without it
                mf.log("map load failed:", d, repr(e))
        self.camp_ang = self.rng.uniform(-PI, PI)
        self.pk = {}      # pickup id -> {kind,pos}
        self.cb = {}      # cube id -> pos
        self.cr = {}      # crate id -> pos
        self.bad = {}     # blacklisted ids -> until time
        self.hist = []    # (t,x,y)
        self.ssign = 1
        self.sflip = 0.0
        self.bias = 1
        self.escape_until = 0.0
        self.escape_dir = 0.0
        self.alert = None  # (bearing, t)
        self.last_dmg_t = -99.0
        self.cover = None
        self.cover_t = -99.0
        self.sweep = 0.0
        self.last_move = (0.0, 0.0)
        self.goal_since = {}
        self.upg_taken = {}
        self.visited = set()
        self.explore_goal = None
        self.explore_t = -99.0
        self.path = None
        self.plan_res = None
        self.planning = False
        self.sync_plan = False
        self.path_goal = (0.0, 0.0)
        self.path_t = -99.0

    # ------------------------------------------------------------------ helpers
    def steer(self, x, y, z, dx, dy, look=7.0):
        n = hypot(dx, dy)
        if n < 1e-6:
            return 0.0, 0.0
        dx /= n
        dy /= n
        w = self.world
        if w is None:
            return dx, dy
        base = math.atan2(dy, dx)
        offs = [0.0]
        for k in range(1, 8):
            offs.append(k * 0.4 * self.bias)
            offs.append(-k * 0.4 * self.bias)
        for off in offs:
            a = base + off
            ux, uy = math.cos(a), math.sin(a)
            ok = True
            for s in (2.5, 5.0, look):
                if not w.free(x + ux * s, y + uy * s, z, s):
                    ok = False
                    break
            if ok:
                # side probes
                px, py = -uy * 2.4, ux * 2.4
                if not (w.free(x + ux * 5 + px, y + uy * 5 + py, z, 5) and w.free(x + ux * 5 - px, y + uy * 5 - py, z, 5)):
                    if off != offs[-1]:
                        continue
                return ux, uy
        return dx, dy

    def safe_zone(self, zone, t):
        if t >= zone["shrink_start"] - 10:
            return zone["next_center"], zone["next_radius"]
        return zone["center"], zone["radius"]

    def lead(self, sx, sy, sz, tp, tv, speed):
        tt = 0.0
        px, py, pz = tp
        for _ in range(3):
            px = tp[0] + tv[0] * tt
            py = tp[1] + tv[1] * tt
            pz = tp[2]
            d = math.sqrt((px - sx) ** 2 + (py - sy) ** 2 + (pz - sz) ** 2)
            tt = d / max(speed, 1.0) + 0.075
        return px, py, pz

    def aim_angles(self, sx, sy, sz, tx, ty, tz):
        yaw = math.atan2(ty - sy, tx - sx)
        pitch = math.atan2(tz - sz, hypot(tx - sx, ty - sy))
        m = self.cfg["mech"]
        return yaw, clamp(pitch, m["min_pitch"], m["max_pitch"])

    def choose_upgrade(self, offer, me):
        best, bi = 999, 0
        for i, c in enumerate(offer["choices"]):
            cid = c["id"]
            pr = P.get("prio") or UPGRADE_PRIORITY
            r = pr.index(cid) if cid in pr else 50
            if r < best:
                best, bi = r, i
        return bi

    def ingest(self, obs, me, t):
        x, y, z = me["pos"]
        for e in obs["events"]:
            ty = e.get("type")
            if ty == "took":
                self.pk.pop(e["id"], None)
            elif ty == "take_failed":
                if e["reason"] in ("gone", "have"):
                    self.pk.pop(e["id"], None)
                else:
                    self.bad[e["id"]] = t + 15
            elif ty == "dropped":
                self.bad[e["id"]] = t + 4
            elif ty == "damaged":
                self.last_dmg_t = t
                if e.get("bearing") is not None:
                    self.alert = (e["bearing"], t)
        vis_pk = set()
        for p in obs["pickups"]:
            vis_pk.add(p["id"])
            self.pk[p["id"]] = {"kind": p["kind"], "pos": p["pos"]}
        vis_cb = set(c["id"] for c in obs["cubes"])
        self.cb = {i: p for i, p in self.cb.items() if i in vis_cb or hypot(p[0] - x, p[1] - y) > 30}
        for c in obs["cubes"]:
            self.cb[c["id"]] = c["pos"]
        vis_cr = set(c["id"] for c in obs["crates"])
        self.cr = {i: p for i, p in self.cr.items() if i in vis_cr or hypot(p[0] - x, p[1] - y) > 30}
        for c in obs["crates"]:
            self.cr[c["id"]] = c["pos"]
        for i in [i for i, p in self.pk.items() if i not in vis_pk and hypot(p["pos"][0] - x, p["pos"][1] - y) < 28]:
            del self.pk[i]
        # sounds -> alert
        for s in obs["sounds"]:
            if s["kind"] in ("gun", "laser", "lightning", "mortar", "laser_charge", "explosion") and s["dist"] != "far":
                if self.alert is None or t - self.alert[1] > 1.0:
                    self.alert = (s["bearing"], t - s["age"])
        step = P["explore_step"]
        for dx_ in (-40, 0, 40):
            for dy_ in (-40, 0, 40):
                self.visited.add((int((x + dx_) // step), int((y + dy_) // step)))
        # stuck history
        self.hist.append((t, x, y))
        while self.hist and t - self.hist[0][0] > 1.5:
            self.hist.pop(0)

    def blocked_id(self, i, t):
        return self.bad.get(i, -1) > t

    def nearest_cube(self, x, y, r):
        best, bp = r, None
        for p in self.cb.values():
            d = hypot(p[0] - x, p[1] - y)
            if d < best:
                best, bp = d, p
        return bp

    def pick_explore(self, x, y, zc, zr):
        step = P["explore_step"]
        best, bp = 1e18, None
        R = zr - 20
        n = int(R // step) + 1
        cx, cy = int(zc[0] // step), int(zc[1] // step)
        for i in range(cx - n, cx + n + 1):
            for j in range(cy - n, cy + n + 1):
                px, py = (i + 0.5) * step, (j + 0.5) * step
                if hypot(px - zc[0], py - zc[1]) > R or (i, j) in self.visited:
                    continue
                sc_ = hypot(px - x, py - y) + 0.3 * hypot(px - zc[0], py - zc[1])
                if sc_ < best:
                    best, bp = sc_, (px, py)
        return self.snap(bp) if bp else None

    def snap(self, p):
        w = self.world
        if w is None or getattr(w, "blk", None) is None:
            return p
        i, j = w.cell(p[0], p[1])
        if not w.blk[j * w.gw + i]:
            return p
        i, j = w.nearest_open(i, j)
        return (w.gx0 + (i + 0.5) * w.cs, w.gy0 + (j + 0.5) * w.cs)

    def waypoint(self, x, y, z, goal, t, T0):
        """Next point to walk toward: straight if clear, else follow an A* path."""
        w = self.world
        gd = hypot(goal[0] - x, goal[1] - y)
        if w is None or getattr(w, "blk", None) is None or gd < 6:
            return goal
        if gd < 45 and w.line_free(x, y, goal[0], goal[1], z):
            self.path = None
            return goal
        if self.plan_res is not None:
            self.path, self.path_goal, self.path_t = self.plan_res
            self.plan_res = None
        need = (
            self.path is None
            or hypot(self.path_goal[0] - goal[0], self.path_goal[1] - goal[1]) > 8
            or t - self.path_t > 5.0
            or (len(self.path) <= 1 and gd > 15)
            or t < self.escape_until
        )
        if need and not self.planning:
            self.planning = True
            sx, sy = x, y

            def job():
                try:
                    res = w.plan(sx, sy, goal[0], goal[1])
                    self.plan_res = (res, goal, t)
                except Exception:
                    self.plan_res = (None, goal, t)
                finally:
                    self.planning = False

            if self.sync_plan:
                job()
                self.path, self.path_goal, self.path_t = self.plan_res
                self.plan_res = None
            else:
                threading.Thread(target=job, daemon=True).start()
        if not self.path:
            return goal
        # drop passed waypoints; pick the farthest visible one within 30 m
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

    def find_cover(self, x, y, z, threats, zc, zr):
        w = self.world
        if w is None or not threats:
            return None
        best, bp = 1e9, None
        for r in (12.0, 22.0, 32.0):
            for k in range(12):
                a = k * PI / 6
                cx, cy = x + r * math.cos(a), y + r * math.sin(a)
                if not w.free(cx, cy, z, r):
                    continue
                if hypot(cx - zc[0], cy - zc[1]) > zr - 8:
                    continue
                cz = w.ground(cx, cy)
                hid = True
                for tp in threats[:2]:
                    if w.los(tp[0], tp[1], tp[2] + 8, cx, cy, cz + 4.5):
                        hid = False
                        break
                if hid:
                    score = r + 0.1 * hypot(cx - zc[0], cy - zc[1])
                    if score < best:
                        best, bp = score, (cx, cy)
            if bp:
                break
        return bp

    # ------------------------------------------------------------------ main
    def act(self, obs):
        T0 = time.perf_counter()
        me = obs["self"]
        x, y, z = me["pos"]
        t = obs["t"]
        tick = obs["tick"]
        stats = me["stats"]
        hpf = me["hp"] / me["max_hp"]
        energy = me["energy"]
        act = {"tick": tick}
        mcfg = self.cfg["mech"]
        w = self.world
        self.ingest(obs, me, t)
        if obs["offer"]:
            act["pick"] = self.choose_upgrade(obs["offer"], me)

        wl = me["weapons"]
        kinds = [(wk["kind"] if wk else None) for wk in wl]
        eye_h = stats.get("eye_height", 8.0)
        mz = stats.get("muzzle_height", 7.0)
        ex, ey, ez = x, y, z + eye_h

        zc, zr = self.safe_zone(obs["zone"], t)
        dzc = hypot(x - zc[0], y - zc[1])
        outside = dzc > zr - 12 or not me["in_zone"]
        camp = self.snap((zc[0] + P["camp"] * zr * math.cos(self.camp_ang), zc[1] + P["camp"] * zr * math.sin(self.camp_ang)))

        def in_safe(px, py, margin=10.0):
            return hypot(px - zc[0], py - zc[1]) < zr - margin

        enemies = [m for m in obs["mechs"] if m["kind"] == "mech"]
        npcs = [m for m in obs["mechs"] if m["kind"] == "npc"]

        # --- choose target
        tgt = None
        tgt_vis = False
        tgt_is_enemy = True
        best = 1e9
        for m in enemies:
            p = m["pos"]
            d = hypot(p[0] - x, p[1] - y)
            sc_ = max(1.0, m.get("cubes", 0))
            tz = p[2] + 4.5 * (1 + 0.05 * m.get("cubes", 0))
            vis = True if w is None else w.los(ex, ey, ez, p[0], p[1], tz)
            hpm = m.get("hp", 700) / max(1.0, m.get("max_hp", 1400))
            score = d * (P["tgt_hp0"] + P["tgt_hpw"] * hpm) + (0 if vis else 60)
            if d > 230:
                score += 200
            if score < best:
                best, tgt, tgt_vis = score, m, vis
        if tgt is None and hpf > P["npc_hp"]:
            for m in npcs:
                p = m["pos"]
                d = hypot(p[0] - x, p[1] - y)
                if d > P["npc_r"] or not in_safe(p[0], p[1], 0):
                    continue
                vis = True if w is None else w.los(ex, ey, ez, p[0], p[1], p[2] + 1.7)
                if vis and d < best:
                    best, tgt, tgt_vis, tgt_is_enemy = d, m, True, False

        move = None     # desired world direction (unit-ish) or None = hold
        aim = None
        fire = [False, False]
        boost = False
        jump = False
        label = ""
        goal = None
        threats_pos = [m["pos"] for m in enemies]

        # --- incoming projectile / laser dodge vector
        dodge = None
        cxm, cym, czm = x, y, z + 4.5
        tmin = 9.0
        for pr in obs["projectiles"]:
            v = pr["vel"]
            sp2 = v[0] ** 2 + v[1] ** 2 + v[2] ** 2
            if sp2 < 1:
                continue
            rel = (cxm - pr["pos"][0], cym - pr["pos"][1], czm - pr["pos"][2])
            if pr["kind"] == "mortar":
                # landing estimate with gravity
                g = self.cfg["weapons"]["mortar"]["gravity"]
                px, py, pz = pr["pos"]
                vx, vy, vz = v
                tl = 0.0
                for _ in range(40):
                    tl += 0.1
                    floor_z = w.ground(px + vx * tl, py + vy * tl) if w else 0.0
                    if pz + vz * tl - 0.5 * g * tl * tl <= floor_z:
                        break
                lx, ly = px + vx * tl, py + vy * tl
                if hypot(lx - x, ly - y) < 10 and tl < tmin:
                    tmin = tl
                    dx, dy = x - lx, y - ly
                    n = hypot(dx, dy) or 1.0
                    dodge = (dx / n, dy / n)
                continue
            tc = (rel[0] * v[0] + rel[1] * v[1] + rel[2] * v[2]) / sp2
            if tc < 0 or tc > 0.9:
                continue
            mx, my, mzz = rel[0] - v[0] * tc, rel[1] - v[1] * tc, rel[2] - v[2] * tc
            if math.sqrt(mx * mx + my * my + mzz * mzz) < 4.2 and tc < tmin:
                tmin = tc
                n = hypot(mx, my)
                if n < 0.5:
                    dodge = (-v[1] / (hypot(v[0], v[1]) or 1), v[0] / (hypot(v[0], v[1]) or 1))
                else:
                    dodge = (mx / n, my / n)
        # laser telegraph from enemies aiming at me
        for m in enemies:
            if m.get("charging"):
                p = m["pos"]
                bearing = math.atan2(y - p[1], x - p[0])
                if abs(wrap(m["aim"][0] - bearing)) < 0.25:
                    ax, ay = math.cos(m["aim"][0]), math.sin(m["aim"][0])
                    px, py = -ay, ax
                    side = 1 if (x - p[0]) * px + (y - p[1]) * py >= 0 else -1
                    dodge = (px * side, py * side)
                    tmin = 0.0

        # --- upkeep: pickups in reach
        near_take = None
        for i, p in self.pk.items():
            if self.blocked_id(i, t):
                continue
            kd = p["kind"]
            sc_new = WEAPON_SCORE.get(kd, 0)
            if sc_new <= 0 or kd in kinds:
                continue
            cur = WEAPON_SCORE.get(kinds[1], 0) if kinds[1] else 0
            if len(kinds) > 1 and kinds[1] is not None and sc_new <= cur:
                continue
            dd = hypot(p["pos"][0] - x, p["pos"][1] - y)
            if dd < 3.6 and abs(p["pos"][2] - z) < 3.5:
                near_take = (i, kd)
        if near_take and not (tgt and tgt_vis and hypot(tgt["pos"][0] - x, tgt["pos"][1] - y) < 40 and False):
            act["take"] = near_take[0]
            if len(wl) > 1 and wl[1] is not None:
                act["drop"] = 1

        # --- combat
        if tgt is not None:
            tp = tgt["pos"]
            tv = tgt.get("vel", [0, 0, 0])
            d = hypot(tp[0] - x, tp[1] - y)
            scale = 1 + 0.05 * tgt.get("cubes", 0) if tgt_is_enemy else 1.0
            tcz = tp[2] + (4.5 * scale if tgt_is_enemy else 1.7)
            gun = wl[0] if wl and wl[0] and wl[0]["kind"] == "gun" else None
            gspeed = gun["stats"]["speed"] if gun else 252.0
            sx, sy, sz = x, y, z + mz
            lp = self.lead(sx, sy, sz, (tp[0], tp[1], tcz), tv, gspeed)
            yaw, pitch = self.aim_angles(sx, sy, sz, lp[0], lp[1], lp[2])
            aim = [yaw, pitch]
            yerr = abs(wrap(yaw - me["aim"][0]))
            perr = abs(pitch - me["aim"][1])
            has_lightning = "lightning" in kinds
            has_laser = "laser" in kinds
            if has_lightning and energy > 12:
                want = P["want_l"]
            elif has_laser:
                want = P["want_lz"]
            else:
                want = P["want_g"]
            n_close = sum(1 for m_ in enemies if hypot(m_["pos"][0] - x, m_["pos"][1] - y) < 70)
            outnumbered = tgt_is_enemy and n_close >= P["multi_n"] and hpf < P["multi_hp"]
            weak = outnumbered or tgt_is_enemy and hpf < P["weak"] and (len(enemies) >= 2 or tgt.get("hp", 700) / max(1, tgt.get("max_hp", 1400)) > hpf)
            tol = max(0.025, 0.7 * math.atan2(2.5, max(d, 5.0)))
            slew_y = stats.get("turn_rate", 4.0) * 0.05 * 0.9
            slew_p = stats.get("pitch_rate", 3.0) * 0.05 * 0.9
            tol_g = max(tol, slew_y)
            tol_gp = max(tol + 0.03, slew_p)
            # firing
            for si, wk in enumerate(wl):
                if not wk:
                    continue
                kd = wk["kind"]
                st = wk["stats"]
                if kd == "gun":
                    if wk.get("ammo", 1) <= 0:
                        act["reload"] = True
                    elif tgt_vis and d < st["range"] * 0.92 and yerr < tol_g and perr < tol_gp:
                        fire[si] = True
                elif kd == "lightning":
                    if tgt_vis and d < st["range"] * 0.95 and yerr < 0.2 and energy > 5:
                        fire[si] = True
                elif kd == "laser":
                    full = st["charge"]
                    ch = wk.get("charge", 0.0)
                    if tgt_vis and d < st["range"] * 0.9 and (d > 18 or ch > 0):
                        if ch > 0:
                            if ch >= full - 0.05 and yerr < tol_g:
                                fire[si] = False
                            else:
                                fire[si] = True
                        elif wk["cooldown"] <= 0.05:
                            fire[si] = True
                    else:
                        fire[si] = False
            # movement
            hpt = tgt.get("hp", 700) / max(1.0, tgt.get("max_hp", 1400))
            if weak:
                if self.cover is None or t - self.cover_t > 0.7:
                    self.cover = self.find_cover(x, y, z, threats_pos, zc, zr) if time.perf_counter() - T0 < 0.02 else self.cover
                    self.cover_t = t
                if self.cover and hypot(self.cover[0] - x, self.cover[1] - y) > 3:
                    goal = self.cover
                    boost = energy > 30
                    label = "retreat"
                else:
                    move = (0.0, 0.0)
                    label = "hide"
            elif not tgt_vis:
                if tgt_is_enemy and hpf > P["hold_hp"] and d < P["adv_d"] and in_safe(tp[0], tp[1], 0):
                    goal = (tp[0], tp[1])
                    label = "advance"
                else:
                    cn = self.nearest_cube(x, y, 45.0)
                    if cn is not None and hpf > 0.3:
                        goal = (cn[0], cn[1])
                        label = "cube!"
                    else:
                        move = (0.0, 0.0)
                        label = "ambush"
            else:
                label = "fight"
                if t >= self.sflip:
                    self.ssign = -self.ssign if self.rng.random() < P["strafe_keep"] else self.ssign
                    self.sflip = t + self.rng.uniform(P["flip_lo"], P["flip_hi"])
                rx, ry = (tp[0] - x) / (d or 1), (tp[1] - y) / (d or 1)
                radial = clamp((d - want) / 15.0, -1.0, 1.0)
                if not tgt_is_enemy:
                    radial = clamp((d - 25) / 10.0, -1.0, 1.0)
                mx = radial * rx + P["amp"] * self.ssign * (-ry)
                my = radial * ry + P["amp"] * self.ssign * rx
                move = (mx, my)
                cn = self.nearest_cube(x, y, P["cube_r"])
                if cn is not None:
                    cdx, cdy = cn[0] - x, cn[1] - y
                    cdn = hypot(cdx, cdy) or 1.0
                    move = (mx + P["cube_w"] * cdx / cdn, my + P["cube_w"] * cdy / cdn)
            if dodge is not None and tmin < P["dodge_t"]:
                move = dodge
                goal = None
                boost = boost
            if outside and tgt_is_enemy:
                goal = zc
                move = None
                boost = not me["in_zone"] and energy > 5
        else:
            # ---- no target: roam
            if self.alert and t - self.alert[1] < 1.5:
                aim = [self.alert[0], 0.0]
            # flee unseen damage
            if self.alert and t - self.last_dmg_t < 1.5 and t - self.alert[1] < 1.5:
                a = self.alert[0] + PI + 0.8 * self.ssign
                goal = (x + 40 * math.cos(a), y + 40 * math.sin(a))
                boost = energy > 40
                label = "flee"
            if dodge is not None and tmin < 0.9:
                move = dodge
                goal = None
            # reload if idle
            gun = wl[0] if wl and wl[0] and wl[0]["kind"] == "gun" else None
            if gun and gun.get("ammo", 99) < self.cfg["weapons"]["gun"]["mag"] * 0.5:
                act["reload"] = True
            if goal is None and move is None:
                if outside:
                    goal = zc
                    boost = not me["in_zone"] and energy > 5
                    label = "zone"
                else:
                    # supply drop
                    cand = None
                    for dr in obs["drops"]:
                        dd = hypot(dr["pos"][0] - x, dr["pos"][1] - y)
                        if in_safe(dr["pos"][0], dr["pos"][1], 6) and dd < 220 and dr["lands_in"] <= dd / 11.0 + 6 and hpf > 0.2:
                            if dr.get("holder") not in (None, self.slot) and dd > 25:
                                continue
                            if cand is None or dd < cand[0]:
                                cand = (dd, dr)
                    if cand:
                        dr = cand[1]
                        goal = (dr["pos"][0], dr["pos"][1])
                        label = "drop"
                        if cand[0] < dr["radius"] * 0.5:
                            move = (0.0, 0.0)
                            goal = None
                    if goal is None and move is None:
                        # weapon pickup
                        wants = []
                        for i, p in self.pk.items():
                            if self.blocked_id(i, t):
                                continue
                            kd = p["kind"]
                            scn = WEAPON_SCORE.get(kd, 0)
                            if scn <= 0 or kd in kinds:
                                continue
                            cur = WEAPON_SCORE.get(kinds[1], 0) if len(kinds) > 1 and kinds[1] else 0
                            if scn <= cur:
                                continue
                            pp = p["pos"]
                            dd = hypot(pp[0] - x, pp[1] - y)
                            if dd > 160 or not in_safe(pp[0], pp[1], 5):
                                continue
                            if pp[2] - (w.ground(pp[0], pp[1]) if w else 0) > 4:
                                continue
                            wants.append((dd - 20 * scn, i, pp))
                        if wants:
                            wants.sort()
                            _, i, pp = wants[0]
                            first = self.goal_since.setdefault(i, t)
                            if t - first > 40:
                                self.bad[i] = t + 90
                            else:
                                goal = (pp[0], pp[1])
                                label = "loot"
                    if goal is None and move is None and hpf > 0.3:
                        # cubes
                        cands = []
                        for i, p in self.cb.items():
                            if self.blocked_id(i, t):
                                continue
                            dd = hypot(p[0] - x, p[1] - y)
                            if dd < 90 and in_safe(p[0], p[1], 5):
                                cands.append((dd, i, p))
                        if cands:
                            cands.sort()
                            dd, i, p = cands[0]
                            first = self.goal_since.setdefault(i, t)
                            if t - first > 25:
                                self.bad[i] = t + 90
                            else:
                                goal = (p[0], p[1])
                                label = "cube"
                    if goal is None and move is None and hpf > 0.6 and t < 330:
                        # crates: approach and shoot
                        cands = []
                        for i, p in self.cr.items():
                            if self.blocked_id(i, t):
                                continue
                            dd = hypot(p[0] - x, p[1] - y)
                            if dd < P["crate_r"] and in_safe(p[0], p[1], 5) and p[2] - (w.ground(p[0], p[1]) if w else 0) < 4:
                                cands.append((dd, i, p))
                        if cands:
                            cands.sort()
                            dd, i, p = cands[0]
                            first = self.goal_since.setdefault(i, t)
                            if t - first > 20:
                                self.bad[i] = t + 120
                            else:
                                cz = p[2] + 1.2
                                yaw, pitch = self.aim_angles(x, y, z + mz, p[0], p[1], cz)
                                aim = [yaw, pitch]
                                vis = True if w is None else w.los(ex, ey, ez, p[0], p[1], cz)
                                if dd > 22 or not vis:
                                    goal = (p[0], p[1])
                                else:
                                    move = (0.0, 0.0)
                                    perr = abs(pitch - me["aim"][1])
                                    if abs(wrap(yaw - me["aim"][0])) < 0.08 and perr < 0.1:
                                        for si, wk in enumerate(wl):
                                            if wk and wk["kind"] == "gun" and wk.get("ammo", 1) > 0 and dd < 150:
                                                fire[si] = True
                                            if wk and wk["kind"] == "lightning" and dd < wk["stats"]["range"] * 0.9 and energy > 30:
                                                fire[si] = True
                                label = "crate"
                    if goal is None and move is None and P["explore"] and t < P["explore_until"] and hpf > P["explore_hp"]:
                        eg = self.explore_goal
                        if eg is None or t - self.explore_t > 25 or hypot(eg[0] - x, eg[1] - y) < 20 or not in_safe(eg[0], eg[1], 15):
                            self.explore_goal = self.pick_explore(x, y, zc, zr)
                            self.explore_t = t
                            eg = self.explore_goal
                        if eg is not None:
                            goal = eg
                            label = "explore"
                    if goal is None and move is None:
                        dc = hypot(camp[0] - x, camp[1] - y)
                        if dc > 8:
                            goal = camp
                            label = "camp-go"
                        else:
                            move = (0.0, 0.0)
                            label = "camp"

        # --- turn goal into move
        if goal is not None and move is None:
            wp = self.waypoint(x, y, z, goal, t, T0)
            gdx, gdy = wp[0] - x, wp[1] - y
            gd = hypot(gdx, gdy)
            if hypot(goal[0] - x, goal[1] - y) < 1.2 or gd < 0.5:
                move = (0.0, 0.0)
            else:
                move = (gdx / gd, gdy / gd)

        # stuck detection (only when we wanted to move)
        stuck = False
        if self.hist and move and hypot(*move) > 0.5 and t - self.hist[0][0] > 1.2:
            h0 = self.hist[0]
            if hypot(x - h0[1], y - h0[2]) < 2.0 and not (fire[0] or fire[1]):
                stuck = True
        if stuck and t > self.escape_until:
            self.bias = -self.bias
            self.escape_until = t + 1.0
            self.escape_dir = self.rng.uniform(-PI, PI)
            self.hist = []
        if t < self.escape_until:
            move = (math.cos(self.escape_dir), math.sin(self.escape_dir))
            jump = True
            boost = energy > 25
            label = "unstick"

        if move is None:
            move = (0.0, 0.0)
        mx, my = move
        if hypot(mx, my) > 1e-6:
            mx, my = self.steer(x, y, z, mx, my)
            if label in ("hide", "camp", "ambush"):
                mx, my = 0.0, 0.0
        else:
            mx = my = 0.0

        # --- aim default
        if aim is None:
            if hypot(mx, my) > 0.1:
                aim = [math.atan2(my, mx), 0.0]
            else:
                self.sweep += 0.12
                aim = [wrap(me["aim"][0] + 0.12), 0.0]

        act["move"] = [round(mx, 4), round(my, 4)]
        act["aim"] = [round(aim[0], 4), round(aim[1], 4)]
        act["fire"] = fire
        act["boost"] = bool(boost)
        act["jump"] = bool(jump)
        self.last_move = (mx, my)

        # prune bad
        if tick % 100 == 0:
            self.bad = {k: v for k, v in self.bad.items() if v > t}
        mf.draw.label(f"{label} hp{me['hp']:.0f}")
        return act


mf.run(Bot)
