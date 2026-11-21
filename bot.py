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


def wrap(v_a):
    return (v_a + PI) % (2 * PI) - PI


def clamp(v_v, v_a2, v_b):
    return v_a2 if v_v < v_a2 else v_b if v_v > v_b else v_v


# ----------------------------------------------------------------------------- map
class World:
    def __init__(self, mapdir_val):
        self.ok = False
        base_val = os.path.join(mapdir_val, "v2")
        with open(os.path.join(base_val, "world.json")) as v_f:
            v_wj = json.load(v_f)
        v_tr = v_wj["terrain"]
        self.nx, self.ny = v_tr["nx"], v_tr["ny"]
        self.minx, self.miny, self.st = v_tr["minX"], v_tr["minY"], v_tr["step"]
        v_g = array("h")
        with open(os.path.join(base_val, v_tr["file"]), "rb") as v_f:
            v_g.frombytes(v_f.read())
        if sys.byteorder != "little":
            v_g.byteswap()
        self.g = v_g
        self.bh = array("f", [-1000.0]) * (self.nx * self.ny)
        for v_p in v_wj["prisms"]:
            if v_p["kind"] == "bridge":
                continue
            try:
                self._fill(v_p)
            except Exception:
                pass
        v_b2 = v_wj["bounds"]
        self.bx0, self.bx1, self.by0, self.by1 = v_b2["minX"], v_b2["maxX"], v_b2["minY"], v_b2["maxY"]
        try:
            self.build_grid()
        except Exception:
            self.blk = None
        self.ok = True

    def _fill(self, v_p2):
        v_top = v_p2["top"]
        edges_val = []
        rings_val = [v_p2["ring"]]
        for v_h in v_p2.get("holes") or []:
            rings_val.append(v_h)
        ymin_val, ymax_val = 1e9, -1e9
        for ring_val in rings_val:
            v_n = len(ring_val) // 2
            for v_k in range(v_n):
                v_x0, v_y0 = ring_val[2 * v_k], ring_val[2 * v_k + 1]
                v_k2 = (v_k + 1) % v_n
                v_x1, v_y1 = ring_val[2 * v_k2], ring_val[2 * v_k2 + 1]
                if v_y0 != v_y1:
                    edges_val.append((v_x0, v_y0, v_x1, v_y1))
                ymin_val = min(ymin_val, v_y0)
                ymax_val = max(ymax_val, v_y0)
        v_st, minx_val, miny_val, v_nx, v_ny, v_bh = self.st, self.minx, self.miny, self.nx, self.ny, self.bh
        v_j0 = max(0, int(math.ceil((ymin_val - miny_val) / v_st)))
        v_j1 = min(v_ny - 1, int(math.floor((ymax_val - miny_val) / v_st)))
        for v_j in range(v_j0, v_j1 + 1):
            v_yc = miny_val + v_j * v_st
            v_xs = []
            for v_x0, v_y0, v_x1, v_y1 in edges_val:
                if (v_y0 <= v_yc < v_y1) or (v_y1 <= v_yc < v_y0):
                    v_xs.append(v_x0 + (v_yc - v_y0) * (v_x1 - v_x0) / (v_y1 - v_y0))
            v_xs.sort()
            v_row = v_j * v_nx
            for v_q in range(0, len(v_xs) - 1, 2):
                v_i0 = max(0, int(math.ceil((v_xs[v_q] - minx_val) / v_st)))
                v_i1 = min(v_nx - 1, int(math.floor((v_xs[v_q + 1] - minx_val) / v_st)))
                for v_i in range(v_i0, v_i1 + 1):
                    if v_bh[v_row + v_i] < v_top:
                        v_bh[v_row + v_i] = v_top

    def build_grid(self, v_cs=3.0):
        import heapq  # noqa: F401
        self.cs = v_cs
        self.gx0, self.gy0 = self.bx0 + 3, self.by0 + 3
        self.gw = int((self.bx1 - self.bx0 - 6) / v_cs)
        self.gh = int((self.by1 - self.by0 - 6) / v_cs)
        v_blk = bytearray(self.gw * self.gh)
        v_bh2, v_g2 = self.bh, self.g
        v_pts = ((0.5, 0.5), (0.2, 0.2), (0.8, 0.2), (0.2, 0.8), (0.8, 0.8))
        for v_j2 in range(self.gh):
            v_y02 = self.gy0 + v_j2 * v_cs
            for v_i2 in range(self.gw):
                v_x02 = self.gx0 + v_i2 * v_cs
                v_nb = 0
                v_cen = 0
                for v_q2, (v_fx, v_fy) in enumerate(v_pts):
                    v_k3 = self.idx(v_x02 + v_fx * v_cs, v_y02 + v_fy * v_cs)
                    if v_bh2[v_k3] > v_g2[v_k3] * 0.01 + 1.3:
                        v_nb += 1
                        if v_q2 == 0:
                            v_cen = 1
                v_blk[v_j2 * self.gw + v_i2] = 1 if (v_cen or v_nb >= 2) else 0
        self.blk = v_blk

    def cell(self, v_x, v_y):
        v_i3 = int((v_x - self.gx0) / self.cs)
        v_j3 = int((v_y - self.gy0) / self.cs)
        return clamp(v_i3, 0, self.gw - 1), clamp(v_j3, 0, self.gh - 1)

    def nearest_open(self, v_i4, v_j4):
        if not self.blk[v_j4 * self.gw + v_i4]:
            return v_i4, v_j4
        for v_r in range(1, 12):
            for v_di in range(-v_r, v_r + 1):
                for v_dj in range(-v_r, v_r + 1):
                    if max(abs(v_di), abs(v_dj)) != v_r:
                        continue
                    v_a3, v_b3 = v_i4 + v_di, v_j4 + v_dj
                    if 0 <= v_a3 < self.gw and 0 <= v_b3 < self.gh and not self.blk[v_b3 * self.gw + v_a3]:
                        return v_a3, v_b3
        return v_i4, v_j4

    def plan(self, v_sx, v_sy, v_gx, v_gy, max_nodes_val=60000):
        import heapq
        v_gw, v_gh, v_blk2, v_cs2 = self.gw, self.gh, self.blk, self.cs
        v_si, v_sj = self.nearest_open(*self.cell(v_sx, v_sy))
        v_ti, v_tj = self.nearest_open(*self.cell(v_gx, v_gy))
        start_val, goal_val = v_sj * v_gw + v_si, v_tj * v_gw + v_ti
        if start_val == goal_val:
            return [(v_gx, v_gy)]
        v_gd = self.g
        dist_val = {start_val: 0.0}
        prev_val = {}
        v_W_ = 1.5
        v_h0 = max(abs(v_si - v_ti), abs(v_sj - v_tj)) + 0.414 * min(abs(v_si - v_ti), abs(v_sj - v_tj))
        v_pq = [(v_h0 * v_W_, start_val)]
        closed_val = set()
        best_val, besth_val = start_val, v_h0
        v_n2 = 0
        v_SQ = 1.4142
        while v_pq and v_n2 < max_nodes_val:
            _, v_cur = heapq.heappop(v_pq)
            if v_cur in closed_val:
                continue
            closed_val.add(v_cur)
            v_n2 += 1
            if v_cur == goal_val:
                best_val = v_cur
                break
            v_ci, v_cj = v_cur % v_gw, v_cur // v_gw
            dcur_val = dist_val[v_cur]
            gcur_val = v_gd[self.idx(self.gx0 + (v_ci + 0.5) * v_cs2, self.gy0 + (v_cj + 0.5) * v_cs2)] * 0.01
            for v_di2, v_dj2 in ((1, 0), (-1, 0), (0, 1), (0, -1), (1, 1), (1, -1), (-1, 1), (-1, -1)):
                v_a4, v_b4 = v_ci + v_di2, v_cj + v_dj2
                if v_a4 < 0 or v_b4 < 0 or v_a4 >= v_gw or v_b4 >= v_gh:
                    continue
                v_nb2 = v_b4 * v_gw + v_a4
                if v_blk2[v_nb2] or v_nb2 in closed_val:
                    continue
                if v_di2 and v_dj2 and (v_blk2[v_cj * v_gw + v_a4] or v_blk2[v_b4 * v_gw + v_ci]):
                    continue  # no corner cutting
                v_gn = v_gd[self.idx(self.gx0 + (v_a4 + 0.5) * v_cs2, self.gy0 + (v_b4 + 0.5) * v_cs2)] * 0.01
                if abs(v_gn - gcur_val) > 3.2:
                    continue
                v_nd = dcur_val + (v_SQ if v_di2 and v_dj2 else 1.0) + abs(v_gn - gcur_val) * 0.3
                if v_nd < dist_val.get(v_nb2, 1e18):
                    dist_val[v_nb2] = v_nd
                    prev_val[v_nb2] = v_cur
                    v_hh = max(abs(v_a4 - v_ti), abs(v_b4 - v_tj)) + 0.414 * min(abs(v_a4 - v_ti), abs(v_b4 - v_tj))
                    if v_hh < besth_val:
                        besth_val, best_val = v_hh, v_nb2
                    heapq.heappush(v_pq, (v_nd + v_W_ * v_hh, v_nb2))
        path_val = []
        v_c = best_val
        while v_c != start_val:
            path_val.append((self.gx0 + (v_c % v_gw + 0.5) * v_cs2, self.gy0 + (v_c // v_gw + 0.5) * v_cs2))
            v_c = prev_val[v_c]
        path_val.reverse()
        if best_val == goal_val:
            path_val.append((v_gx, v_gy))
        return path_val

    def line_free(self, v_ax, v_ay, v_bx, v_by, v_z):
        v_d = hypot(v_bx - v_ax, v_by - v_ay)
        v_n3 = int(v_d / 1.5) + 1
        for v_k4 in range(1, v_n3 + 1):
            v_f2 = v_k4 / v_n3
            v_px, v_py = v_ax + (v_bx - v_ax) * v_f2, v_ay + (v_by - v_ay) * v_f2
            if not self.free(v_px, v_py, v_z, 3.0):
                return False
            # clearance: sides
        return True

    def idx(self, v_x2, v_y2):
        v_i5 = int((v_x2 - self.minx) / self.st + 0.5)
        v_j5 = int((v_y2 - self.miny) / self.st + 0.5)
        if v_i5 < 0:
            v_i5 = 0
        elif v_i5 >= self.nx:
            v_i5 = self.nx - 1
        if v_j5 < 0:
            v_j5 = 0
        elif v_j5 >= self.ny:
            v_j5 = self.ny - 1
        return v_j5 * self.nx + v_i5

    def ground(self, v_x3, v_y3):
        return self.g[self.idx(v_x3, v_y3)] * 0.01

    def roof(self, v_x4, v_y4):
        return self.bh[self.idx(v_x4, v_y4)]

    def free(self, v_x5, v_y5, v_z2, v_s):
        if not (self.bx0 + 3 < v_x5 < self.bx1 - 3 and self.by0 + 3 < v_y5 < self.by1 - 3):
            return False
        v_k5 = self.idx(v_x5, v_y5)
        if self.bh[v_k5] > v_z2 + 1.3:
            return False
        if self.g[v_k5] * 0.01 - v_z2 > 1.19 * v_s + 1.5:
            return False
        return True

    def los(self, v_ax2, v_ay2, v_az, v_bx2, v_by2, v_bz):
        v_dx, v_dy = v_bx2 - v_ax2, v_by2 - v_ay2
        v_n4 = int(hypot(v_dx, v_dy) / 1.5)
        v_bh3, v_g3 = self.bh, self.g
        for v_k6 in range(1, v_n4):
            v_f3 = v_k6 / v_n4
            v_i6 = self.idx(v_ax2 + v_dx * v_f3, v_ay2 + v_dy * v_f3)
            v_z3 = v_az + (v_bz - v_az) * v_f3
            if v_bh3[v_i6] > v_z3 or v_g3[v_i6] * 0.01 > v_z3:
                return False
        return True


# ----------------------------------------------------------------------------- bot
UPGRADE_PRIORITY = [
    "plating", "armor", "nanites", "gyros", "gun_damage", "lightning_chain", "laser_damage",
    "gun_rate", "servos", "gun_mag", "laser_charge", "lightning_efficiency", "lightning_range",
    "capacitor", "reactor", "sensors", "jets", "gun_velocity",
]
P = dict(want_l=22.0, want_g=40.0, want_lz=55.0, weak=0.35, amp=0.9, flip_lo=0.5, flip_hi=1.3, hold_hp=0.6, cube_r=30.0, cube_w=1.6, multi_n=99, multi_hp=0.7, adv_d=140.0, camp=0.5, dodge_t=0.9, strafe_keep=0.7)
try:
    P.update(json.loads(os.environ.get("BOT_PARAMS", "{}")))
except Exception:
    pass
WEAPON_SCORE = {"lightning": 3.0, "laser": 2.0, "mortar": 0.0, "gun": 1.0}


class Bot:
    name = "survivor-v11"

    def __init__(self, hello_val):
        v_cfg = hello_val["config"]
        self.cfg = v_cfg
        self.rng = random.Random(hello_val.get("seed", 0))
        self.slot = hello_val.get("slot", 0)
        self.world = None
        for v_d2 in (hello_val["map"]["dir"], "/arena"):
            try:
                self.world = World(v_d2)
                break
            except Exception as v_e:  # map is an optimisation; play without it
                mf.log("map load failed:", v_d2, repr(v_e))
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
        self.path = None
        self.plan_res = None
        self.planning = False
        self.sync_plan = False
        self.path_goal = (0.0, 0.0)
        self.path_t = -99.0

    # ------------------------------------------------------------------ helpers
    def steer(self, v_x6, v_y6, v_z4, v_dx2, v_dy2, look_val=7.0):
        v_n5 = hypot(v_dx2, v_dy2)
        if v_n5 < 1e-6:
            return 0.0, 0.0
        v_dx2 /= v_n5
        v_dy2 /= v_n5
        v_w = self.world
        if v_w is None:
            return v_dx2, v_dy2
        base_val2 = math.atan2(v_dy2, v_dx2)
        offs_val = [0.0]
        for v_k7 in range(1, 8):
            offs_val.append(v_k7 * 0.4 * self.bias)
            offs_val.append(-v_k7 * 0.4 * self.bias)
        for v_off in offs_val:
            v_a5 = base_val2 + v_off
            v_ux, v_uy = math.cos(v_a5), math.sin(v_a5)
            v_ok = True
            for v_s2 in (2.5, 5.0, look_val):
                if not v_w.free(v_x6 + v_ux * v_s2, v_y6 + v_uy * v_s2, v_z4, v_s2):
                    v_ok = False
                    break
            if v_ok:
                # side probes
                v_px2, v_py2 = -v_uy * 2.4, v_ux * 2.4
                if not (v_w.free(v_x6 + v_ux * 5 + v_px2, v_y6 + v_uy * 5 + v_py2, v_z4, 5) and v_w.free(v_x6 + v_ux * 5 - v_px2, v_y6 + v_uy * 5 - v_py2, v_z4, 5)):
                    if v_off != offs_val[-1]:
                        continue
                return v_ux, v_uy
        return v_dx2, v_dy2

    def safe_zone(self, zone_val, v_t):
        if v_t >= zone_val["shrink_start"] - 10:
            return zone_val["next_center"], zone_val["next_radius"]
        return zone_val["center"], zone_val["radius"]

    def lead(self, v_sx2, v_sy2, v_sz, v_tp, v_tv, speed_val):
        v_tt = 0.0
        v_px3, v_py3, v_pz = v_tp
        for _ in range(3):
            v_px3 = v_tp[0] + v_tv[0] * v_tt
            v_py3 = v_tp[1] + v_tv[1] * v_tt
            v_pz = v_tp[2]
            v_d3 = math.sqrt((v_px3 - v_sx2) ** 2 + (v_py3 - v_sy2) ** 2 + (v_pz - v_sz) ** 2)
            v_tt = v_d3 / max(speed_val, 1.0) + 0.075
        return v_px3, v_py3, v_pz

    def aim_angles(self, v_sx3, v_sy3, v_sz2, v_tx, v_ty, v_tz):
        v_yaw = math.atan2(v_ty - v_sy3, v_tx - v_sx3)
        pitch_val = math.atan2(v_tz - v_sz2, hypot(v_tx - v_sx3, v_ty - v_sy3))
        v_m = self.cfg["mech"]
        return v_yaw, clamp(pitch_val, v_m["min_pitch"], v_m["max_pitch"])

    def choose_upgrade(self, offer_val, v_me):
        best_val2, v_bi = 999, 0
        for v_i7, v_c2 in enumerate(offer_val["choices"]):
            v_cid = v_c2["id"]
            v_r2 = UPGRADE_PRIORITY.index(v_cid) if v_cid in UPGRADE_PRIORITY else 50
            if v_r2 < best_val2:
                best_val2, v_bi = v_r2, v_i7
        return v_bi

    def ingest(self, v_obs, v_me2, v_t2):
        v_x7, v_y7, v_z5 = v_me2["pos"]
        for v_e2 in v_obs["events"]:
            v_ty2 = v_e2.get("type")
            if v_ty2 == "took":
                self.pk.pop(v_e2["id"], None)
            elif v_ty2 == "take_failed":
                if v_e2["reason"] in ("gone", "have"):
                    self.pk.pop(v_e2["id"], None)
                else:
                    self.bad[v_e2["id"]] = v_t2 + 15
            elif v_ty2 == "dropped":
                self.bad[v_e2["id"]] = v_t2 + 4
            elif v_ty2 == "damaged":
                self.last_dmg_t = v_t2
                if v_e2.get("bearing") is not None:
                    self.alert = (v_e2["bearing"], v_t2)
        vis_pk_val = set()
        for v_p3 in v_obs["pickups"]:
            vis_pk_val.add(v_p3["id"])
            self.pk[v_p3["id"]] = {"kind": v_p3["kind"], "pos": v_p3["pos"]}
        vis_cb_val = set(v_c3["id"] for v_c3 in v_obs["cubes"])
        self.cb = {v_i8: v_p3 for v_i8, v_p3 in self.cb.items() if v_i8 in vis_cb_val or hypot(v_p3[0] - v_x7, v_p3[1] - v_y7) > 30}
        for v_c3 in v_obs["cubes"]:
            self.cb[v_c3["id"]] = v_c3["pos"]
        vis_cr_val = set(v_c3["id"] for v_c3 in v_obs["crates"])
        self.cr = {v_i8: v_p3 for v_i8, v_p3 in self.cr.items() if v_i8 in vis_cr_val or hypot(v_p3[0] - v_x7, v_p3[1] - v_y7) > 30}
        for v_c3 in v_obs["crates"]:
            self.cr[v_c3["id"]] = v_c3["pos"]
        for v_i8 in [v_i8 for v_i8, v_p3 in self.pk.items() if v_i8 not in vis_pk_val and hypot(v_p3["pos"][0] - v_x7, v_p3["pos"][1] - v_y7) < 28]:
            del self.pk[v_i8]
        # sounds -> alert
        for v_s3 in v_obs["sounds"]:
            if v_s3["kind"] in ("gun", "laser", "lightning", "mortar", "laser_charge", "explosion") and v_s3["dist"] != "far":
                if self.alert is None or v_t2 - self.alert[1] > 1.0:
                    self.alert = (v_s3["bearing"], v_t2 - v_s3["age"])
        # stuck history
        self.hist.append((v_t2, v_x7, v_y7))
        while self.hist and v_t2 - self.hist[0][0] > 1.5:
            self.hist.pop(0)

    def blocked_id(self, v_i9, v_t3):
        return self.bad.get(v_i9, -1) > v_t3

    def nearest_cube(self, v_x8, v_y8, v_r3):
        best_val3, v_bp = v_r3, None
        for v_p4 in self.cb.values():
            v_d4 = hypot(v_p4[0] - v_x8, v_p4[1] - v_y8)
            if v_d4 < best_val3:
                best_val3, v_bp = v_d4, v_p4
        return v_bp

    def snap(self, v_p5):
        v_w2 = self.world
        if v_w2 is None or getattr(v_w2, "blk", None) is None:
            return v_p5
        v_i10, v_j6 = v_w2.cell(v_p5[0], v_p5[1])
        if not v_w2.blk[v_j6 * v_w2.gw + v_i10]:
            return v_p5
        v_i10, v_j6 = v_w2.nearest_open(v_i10, v_j6)
        return (v_w2.gx0 + (v_i10 + 0.5) * v_w2.cs, v_w2.gy0 + (v_j6 + 0.5) * v_w2.cs)

    def waypoint(self, v_x9, v_y9, v_z6, goal_val2, v_t4, v_T0):
        """Next point to walk toward: straight if clear, else follow an A* path."""
        v_w3 = self.world
        v_gd2 = hypot(goal_val2[0] - v_x9, goal_val2[1] - v_y9)
        if v_w3 is None or getattr(v_w3, "blk", None) is None or v_gd2 < 6:
            return goal_val2
        if v_gd2 < 45 and v_w3.line_free(v_x9, v_y9, goal_val2[0], goal_val2[1], v_z6):
            self.path = None
            return goal_val2
        if self.plan_res is not None:
            self.path, self.path_goal, self.path_t = self.plan_res
            self.plan_res = None
        need_val = (
            self.path is None
            or hypot(self.path_goal[0] - goal_val2[0], self.path_goal[1] - goal_val2[1]) > 8
            or v_t4 - self.path_t > 5.0
            or (len(self.path) <= 1 and v_gd2 > 15)
            or v_t4 < self.escape_until
        )
        if need_val and not self.planning:
            self.planning = True
            v_sx4, v_sy4 = v_x9, v_y9

            def job():
                try:
                    v_res = v_w3.plan(v_sx4, v_sy4, goal_val2[0], goal_val2[1])
                    self.plan_res = (v_res, goal_val2, v_t4)
                except Exception:
                    self.plan_res = (None, goal_val2, v_t4)
                finally:
                    self.planning = False

            if self.sync_plan:
                v_job()
                self.path, self.path_goal, self.path_t = self.plan_res
                self.plan_res = None
            else:
                threading.Thread(target=v_job, daemon=True).start()
        if not self.path:
            return goal_val2
        # drop passed waypoints; pick the farthest visible one within 30 m
        while len(self.path) > 1 and hypot(self.path[0][0] - v_x9, self.path[0][1] - v_y9) < 4.0:
            self.path.pop(0)
        v_tgt = self.path[0]
        for v_k8 in range(min(len(self.path), 10)):
            v_px4, v_py4 = self.path[v_k8]
            if hypot(v_px4 - v_x9, v_py4 - v_y9) > 30:
                break
            if v_w3.line_free(v_x9, v_y9, v_px4, v_py4, v_z6):
                v_tgt = self.path[v_k8]
        return v_tgt

    def find_cover(self, v_x10, v_y10, v_z7, threats_val, v_zc, v_zr):
        v_w4 = self.world
        if v_w4 is None or not threats_val:
            return None
        best_val4, v_bp2 = 1e9, None
        for v_r4 in (12.0, 22.0, 32.0):
            for v_k9 in range(12):
                v_a6 = v_k9 * PI / 6
                v_cx, v_cy = v_x10 + v_r4 * math.cos(v_a6), v_y10 + v_r4 * math.sin(v_a6)
                if not v_w4.free(v_cx, v_cy, v_z7, v_r4):
                    continue
                if hypot(v_cx - v_zc[0], v_cy - v_zc[1]) > v_zr - 8:
                    continue
                v_cz = v_w4.ground(v_cx, v_cy)
                v_hid = True
                for v_tp2 in threats_val[:2]:
                    if v_w4.los(v_tp2[0], v_tp2[1], v_tp2[2] + 8, v_cx, v_cy, v_cz + 4.5):
                        v_hid = False
                        break
                if v_hid:
                    score_val = v_r4 + 0.1 * hypot(v_cx - v_zc[0], v_cy - v_zc[1])
                    if score_val < best_val4:
                        best_val4, v_bp2 = score_val, (v_cx, v_cy)
            if v_bp2:
                break
        return v_bp2

    # ------------------------------------------------------------------ main
    def act(self, v_obs2):
        v_T02 = time.perf_counter()
        v_me3 = v_obs2["self"]
        v_x11, v_y11, v_z8 = v_me3["pos"]
        v_t5 = v_obs2["t"]
        tick_val = v_obs2["tick"]
        stats_val = v_me3["stats"]
        v_hpf = v_me3["hp"] / v_me3["max_hp"]
        energy_val = v_me3["energy"]
        v_act = {"tick": tick_val}
        mcfg_val = self.cfg["mech"]
        v_w5 = self.world
        self.ingest(v_obs2, v_me3, v_t5)
        if v_obs2["offer"]:
            v_act["pick"] = self.choose_upgrade(v_obs2["offer"], v_me3)

        v_wl = v_me3["weapons"]
        kinds_val = [(v_wk["kind"] if v_wk else None) for v_wk in v_wl]
        eye_h_val = stats_val.get("eye_height", 8.0)
        v_mz = stats_val.get("muzzle_height", 7.0)
        v_ex, v_ey, v_ez = v_x11, v_y11, v_z8 + eye_h_val

        v_zc2, v_zr2 = self.safe_zone(v_obs2["zone"], v_t5)
        v_dzc = hypot(v_x11 - v_zc2[0], v_y11 - v_zc2[1])
        outside_val = v_dzc > v_zr2 - 12 or not v_me3["in_zone"]
        camp_val = self.snap((v_zc2[0] + P["camp"] * v_zr2 * math.cos(self.camp_ang), v_zc2[1] + P["camp"] * v_zr2 * math.sin(self.camp_ang)))

        def in_safe(v_px5, v_py5, margin_val=10.0):
            return hypot(v_px5 - v_zc2[0], v_py5 - v_zc2[1]) < v_zr2 - margin_val

        enemies_val = [v_m2 for v_m2 in v_obs2["mechs"] if v_m2["kind"] == "mech"]
        npcs_val = [v_m2 for v_m2 in v_obs2["mechs"] if v_m2["kind"] == "npc"]

        # --- choose target
        v_tgt2 = None
        tgt_vis_val = False
        tgt_is_enemy_val = True
        best_val5 = 1e9
        for v_m2 in enemies_val:
            v_p6 = v_m2["pos"]
            v_d5 = hypot(v_p6[0] - v_x11, v_p6[1] - v_y11)
            v_sc_ = max(1.0, v_m2.get("cubes", 0))
            v_tz2 = v_p6[2] + 4.5 * (1 + 0.05 * v_m2.get("cubes", 0))
            v_vis = True if v_w5 is None else v_w5.los(v_ex, v_ey, v_ez, v_p6[0], v_p6[1], v_tz2)
            v_hpm = v_m2.get("hp", 700) / max(1.0, v_m2.get("max_hp", 1400))
            score_val2 = v_d5 * (0.6 + v_hpm) + (0 if v_vis else 60)
            if v_d5 > 230:
                score_val2 += 200
            if score_val2 < best_val5:
                best_val5, v_tgt2, tgt_vis_val = score_val2, v_m2, v_vis
        if v_tgt2 is None and v_hpf > 0.5:
            for v_m2 in npcs_val:
                v_p6 = v_m2["pos"]
                v_d5 = hypot(v_p6[0] - v_x11, v_p6[1] - v_y11)
                if v_d5 > 60 or not in_safe_val(v_p6[0], v_p6[1], 0):
                    continue
                v_vis = True if v_w5 is None else v_w5.los(v_ex, v_ey, v_ez, v_p6[0], v_p6[1], v_p6[2] + 1.7)
                if v_vis and v_d5 < best_val5:
                    best_val5, v_tgt2, tgt_vis_val, tgt_is_enemy_val = v_d5, v_m2, True, False

        move_val = None     # desired world direction (unit-ish) or None = hold
        v_aim = None
        fire_val = [False, False]
        boost_val = False
        jump_val = False
        label_val = ""
        goal_val3 = None
        threats_pos_val = [v_m2["pos"] for v_m2 in enemies_val]

        # --- incoming projectile / laser dodge vector
        dodge_val = None
        v_cxm, v_cym, v_czm = v_x11, v_y11, v_z8 + 4.5
        tmin_val = 9.0
        for v_pr in v_obs2["projectiles"]:
            v_v2 = v_pr["vel"]
            v_sp2 = v_v2[0] ** 2 + v_v2[1] ** 2 + v_v2[2] ** 2
            if v_sp2 < 1:
                continue
            v_rel = (v_cxm - v_pr["pos"][0], v_cym - v_pr["pos"][1], v_czm - v_pr["pos"][2])
            if v_pr["kind"] == "mortar":
                # landing estimate with gravity
                v_g4 = self.cfg["weapons"]["mortar"]["gravity"]
                v_px5, v_py5, v_pz2 = v_pr["pos"]
                v_vx, v_vy, v_vz = v_v2
                v_tl = 0.0
                for _ in range(40):
                    v_tl += 0.1
                    floor_z_val = v_w5.ground(v_px5 + v_vx * v_tl, v_py5 + v_vy * v_tl) if v_w5 else 0.0
                    if v_pz2 + v_vz * v_tl - 0.5 * v_g4 * v_tl * v_tl <= floor_z_val:
                        break
                v_lx, v_ly = v_px5 + v_vx * v_tl, v_py5 + v_vy * v_tl
                if hypot(v_lx - v_x11, v_ly - v_y11) < 10 and v_tl < tmin_val:
                    tmin_val = v_tl
                    v_dx3, v_dy3 = v_x11 - v_lx, v_y11 - v_ly
                    v_n6 = hypot(v_dx3, v_dy3) or 1.0
                    dodge_val = (v_dx3 / v_n6, v_dy3 / v_n6)
                continue
            v_tc = (v_rel[0] * v_v2[0] + v_rel[1] * v_v2[1] + v_rel[2] * v_v2[2]) / v_sp2
            if v_tc < 0 or v_tc > 0.9:
                continue
            v_mx, v_my, v_mzz = v_rel[0] - v_v2[0] * v_tc, v_rel[1] - v_v2[1] * v_tc, v_rel[2] - v_v2[2] * v_tc
            if math.sqrt(v_mx * v_mx + v_my * v_my + v_mzz * v_mzz) < 4.2 and v_tc < tmin_val:
                tmin_val = v_tc
                v_n6 = hypot(v_mx, v_my)
                if v_n6 < 0.5:
                    dodge_val = (-v_v2[1] / (hypot(v_v2[0], v_v2[1]) or 1), v_v2[0] / (hypot(v_v2[0], v_v2[1]) or 1))
                else:
                    dodge_val = (v_mx / v_n6, v_my / v_n6)
        # laser telegraph from enemies aiming at me
        for v_m2 in enemies_val:
            if v_m2.get("charging"):
                v_p6 = v_m2["pos"]
                bearing_val = math.atan2(v_y11 - v_p6[1], v_x11 - v_p6[0])
                if abs(wrap(v_m2["aim"][0] - bearing_val)) < 0.25:
                    v_ax3, v_ay3 = math.cos(v_m2["aim"][0]), math.sin(v_m2["aim"][0])
                    v_px5, v_py5 = -v_ay3, v_ax3
                    side_val = 1 if (v_x11 - v_p6[0]) * v_px5 + (v_y11 - v_p6[1]) * v_py5 >= 0 else -1
                    dodge_val = (v_px5 * side_val, v_py5 * side_val)
                    tmin_val = 0.0

        # --- upkeep: pickups in reach
        near_take_val = None
        for v_i11, v_p6 in self.pk.items():
            if self.blocked_id(v_i11, v_t5):
                continue
            v_kd = v_p6["kind"]
            sc_new_val = WEAPON_SCORE.get(v_kd, 0)
            if sc_new_val <= 0 or v_kd in kinds_val:
                continue
            v_cur2 = WEAPON_SCORE.get(kinds_val[1], 0) if kinds_val[1] else 0
            if len(kinds_val) > 1 and kinds_val[1] is not None and sc_new_val <= v_cur2:
                continue
            v_dd = hypot(v_p6["pos"][0] - v_x11, v_p6["pos"][1] - v_y11)
            if v_dd < 3.6 and abs(v_p6["pos"][2] - v_z8) < 3.5:
                near_take_val = (v_i11, v_kd)
        if near_take_val and not (v_tgt2 and tgt_vis_val and hypot(v_tgt2["pos"][0] - v_x11, v_tgt2["pos"][1] - v_y11) < 40 and False):
            v_act["take"] = near_take_val[0]
            if len(v_wl) > 1 and v_wl[1] is not None:
                v_act["drop"] = 1

        # --- combat
        if v_tgt2 is not None:
            v_tp3 = v_tgt2["pos"]
            v_tv2 = v_tgt2.get("vel", [0, 0, 0])
            v_d5 = hypot(v_tp3[0] - v_x11, v_tp3[1] - v_y11)
            scale_val = 1 + 0.05 * v_tgt2.get("cubes", 0) if tgt_is_enemy_val else 1.0
            v_tcz = v_tp3[2] + (4.5 * scale_val if tgt_is_enemy_val else 1.7)
            v_gun = v_wl[0] if v_wl and v_wl[0] and v_wl[0]["kind"] == "gun" else None
            gspeed_val = v_gun["stats"]["speed"] if v_gun else 252.0
            v_sx5, v_sy5, v_sz3 = v_x11, v_y11, v_z8 + v_mz
            v_lp = self.lead(v_sx5, v_sy5, v_sz3, (v_tp3[0], v_tp3[1], v_tcz), v_tv2, gspeed_val)
            v_yaw2, pitch_val2 = self.aim_angles(v_sx5, v_sy5, v_sz3, v_lp[0], v_lp[1], v_lp[2])
            v_aim = [v_yaw2, pitch_val2]
            yerr_val = abs(wrap(v_yaw2 - v_me3["aim"][0]))
            perr_val = abs(pitch_val2 - v_me3["aim"][1])
            has_lightning_val = "lightning" in kinds_val
            has_laser_val = "laser" in kinds_val
            if has_lightning_val and energy_val > 12:
                want_val = P["want_l"]
            elif has_laser_val:
                want_val = P["want_lz"]
            else:
                want_val = P["want_g"]
            n_close_val = sum(1 for v_m_ in enemies_val if hypot(v_m_["pos"][0] - v_x11, v_m_["pos"][1] - v_y11) < 70)
            outnumbered_val = tgt_is_enemy_val and n_close_val >= P["multi_n"] and v_hpf < P["multi_hp"]
            weak_val = outnumbered_val or tgt_is_enemy_val and v_hpf < P["weak"] and (len(enemies_val) >= 2 or v_tgt2.get("hp", 700) / max(1, v_tgt2.get("max_hp", 1400)) > v_hpf)
            v_tol = max(0.025, 0.7 * math.atan2(2.5, max(v_d5, 5.0)))
            slew_y_val = stats_val.get("turn_rate", 4.0) * 0.05 * 0.9
            slew_p_val = stats_val.get("pitch_rate", 3.0) * 0.05 * 0.9
            tol_g_val = max(v_tol, slew_y_val)
            tol_gp_val = max(v_tol + 0.03, slew_p_val)
            # firing
            for v_si2, v_wk in enumerate(v_wl):
                if not v_wk:
                    continue
                v_kd = v_wk["kind"]
                v_st2 = v_wk["stats"]
                if v_kd == "gun":
                    if v_wk.get("ammo", 1) <= 0:
                        v_act["reload"] = True
                    elif tgt_vis_val and v_d5 < v_st2["range"] * 0.92 and yerr_val < tol_g_val and perr_val < tol_gp_val:
                        fire_val[v_si2] = True
                elif v_kd == "lightning":
                    if tgt_vis_val and v_d5 < v_st2["range"] * 0.95 and yerr_val < 0.2 and energy_val > 5:
                        fire_val[v_si2] = True
                elif v_kd == "laser":
                    full_val = v_st2["charge"]
                    v_ch = v_wk.get("charge", 0.0)
                    if tgt_vis_val and v_d5 < v_st2["range"] * 0.9 and (v_d5 > 18 or v_ch > 0):
                        if v_ch > 0:
                            if v_ch >= full_val - 0.05 and yerr_val < tol_g_val:
                                fire_val[v_si2] = False
                            else:
                                fire_val[v_si2] = True
                        elif v_wk["cooldown"] <= 0.05:
                            fire_val[v_si2] = True
                    else:
                        fire_val[v_si2] = False
            # movement
            v_hpt = v_tgt2.get("hp", 700) / max(1.0, v_tgt2.get("max_hp", 1400))
            if weak_val:
                if self.cover is None or v_t5 - self.cover_t > 0.7:
                    self.cover = self.find_cover(v_x11, v_y11, v_z8, threats_pos_val, v_zc2, v_zr2) if time.perf_counter() - v_T02 < 0.02 else self.cover
                    self.cover_t = v_t5
                if self.cover and hypot(self.cover[0] - v_x11, self.cover[1] - v_y11) > 3:
                    goal_val3 = self.cover
                    boost_val = energy_val > 30
                    label_val = "retreat"
                else:
                    move_val = (0.0, 0.0)
                    label_val = "hide"
            elif not tgt_vis_val:
                if tgt_is_enemy_val and v_hpf > P["hold_hp"] and v_d5 < P["adv_d"] and in_safe_val(v_tp3[0], v_tp3[1], 0):
                    goal_val3 = (v_tp3[0], v_tp3[1])
                    label_val = "advance"
                else:
                    v_cn = self.nearest_cube(v_x11, v_y11, 45.0)
                    if v_cn is not None and v_hpf > 0.3:
                        goal_val3 = (v_cn[0], v_cn[1])
                        label_val = "cube!"
                    else:
                        move_val = (0.0, 0.0)
                        label_val = "ambush"
            else:
                label_val = "fight"
                if v_t5 >= self.sflip:
                    self.ssign = -self.ssign if self.rng.random() < P["strafe_keep"] else self.ssign
                    self.sflip = v_t5 + self.rng.uniform(P["flip_lo"], P["flip_hi"])
                v_rx, v_ry = (v_tp3[0] - v_x11) / (v_d5 or 1), (v_tp3[1] - v_y11) / (v_d5 or 1)
                radial_val = clamp((v_d5 - want_val) / 15.0, -1.0, 1.0)
                if not tgt_is_enemy_val:
                    radial_val = clamp((v_d5 - 25) / 10.0, -1.0, 1.0)
                v_mx = radial_val * v_rx + P["amp"] * self.ssign * (-v_ry)
                v_my = radial_val * v_ry + P["amp"] * self.ssign * v_rx
                move_val = (v_mx, v_my)
                v_cn = self.nearest_cube(v_x11, v_y11, P["cube_r"])
                if v_cn is not None:
                    v_cdx, v_cdy = v_cn[0] - v_x11, v_cn[1] - v_y11
                    v_cdn = hypot(v_cdx, v_cdy) or 1.0
                    move_val = (v_mx + P["cube_w"] * v_cdx / v_cdn, v_my + P["cube_w"] * v_cdy / v_cdn)
            if dodge_val is not None and tmin_val < P["dodge_t"]:
                move_val = dodge_val
                goal_val3 = None
                boost_val = boost_val
            if outside_val and tgt_is_enemy_val:
                goal_val3 = v_zc2
                move_val = None
                boost_val = not v_me3["in_zone"] and energy_val > 5
        else:
            # ---- no target: roam
            if self.alert and v_t5 - self.alert[1] < 1.5:
                v_aim = [self.alert[0], 0.0]
            # flee unseen damage
            if self.alert and v_t5 - self.last_dmg_t < 1.5 and v_t5 - self.alert[1] < 1.5:
                v_a7 = self.alert[0] + PI + 0.8 * self.ssign
                goal_val3 = (v_x11 + 40 * math.cos(v_a7), v_y11 + 40 * math.sin(v_a7))
                boost_val = energy_val > 40
                label_val = "flee"
            if dodge_val is not None and tmin_val < 0.9:
                move_val = dodge_val
                goal_val3 = None
            # reload if idle
            v_gun = v_wl[0] if v_wl and v_wl[0] and v_wl[0]["kind"] == "gun" else None
            if v_gun and v_gun.get("ammo", 99) < self.cfg["weapons"]["gun"]["mag"] * 0.5:
                v_act["reload"] = True
            if goal_val3 is None and move_val is None:
                if outside_val:
                    goal_val3 = v_zc2
                    boost_val = not v_me3["in_zone"] and energy_val > 5
                    label_val = "zone"
                else:
                    # supply drop
                    cand_val = None
                    for v_dr in v_obs2["drops"]:
                        v_dd = hypot(v_dr["pos"][0] - v_x11, v_dr["pos"][1] - v_y11)
                        if in_safe_val(v_dr["pos"][0], v_dr["pos"][1], 6) and v_dd < 220 and v_dr["lands_in"] <= v_dd / 11.0 + 6 and v_hpf > 0.2:
                            if v_dr.get("holder") not in (None, self.slot) and v_dd > 25:
                                continue
                            if cand_val is None or v_dd < cand_val[0]:
                                cand_val = (v_dd, v_dr)
                    if cand_val:
                        v_dr = cand_val[1]
                        goal_val3 = (v_dr["pos"][0], v_dr["pos"][1])
                        label_val = "drop"
                        if cand_val[0] < v_dr["radius"] * 0.5:
                            move_val = (0.0, 0.0)
                            goal_val3 = None
                    if goal_val3 is None and move_val is None:
                        # weapon pickup
                        wants_val = []
                        for v_i11, v_p6 in self.pk.items():
                            if self.blocked_id(v_i11, v_t5):
                                continue
                            v_kd = v_p6["kind"]
                            v_scn = WEAPON_SCORE.get(v_kd, 0)
                            if v_scn <= 0 or v_kd in kinds_val:
                                continue
                            v_cur2 = WEAPON_SCORE.get(kinds_val[1], 0) if len(kinds_val) > 1 and kinds_val[1] else 0
                            if v_scn <= v_cur2:
                                continue
                            v_pp = v_p6["pos"]
                            v_dd = hypot(v_pp[0] - v_x11, v_pp[1] - v_y11)
                            if v_dd > 160 or not in_safe_val(v_pp[0], v_pp[1], 5):
                                continue
                            if v_pp[2] - (v_w5.ground(v_pp[0], v_pp[1]) if v_w5 else 0) > 4:
                                continue
                            wants_val.append((v_dd - 20 * v_scn, v_i11, v_pp))
                        if wants_val:
                            wants_val.sort()
                            _, v_i11, v_pp = wants_val[0]
                            first_val = self.goal_since.setdefault(v_i11, v_t5)
                            if v_t5 - first_val > 40:
                                self.bad[v_i11] = v_t5 + 90
                            else:
                                goal_val3 = (v_pp[0], v_pp[1])
                                label_val = "loot"
                    if goal_val3 is None and move_val is None and v_hpf > 0.3:
                        # cubes
                        cands_val = []
                        for v_i11, v_p6 in self.cb.items():
                            if self.blocked_id(v_i11, v_t5):
                                continue
                            v_dd = hypot(v_p6[0] - v_x11, v_p6[1] - v_y11)
                            if v_dd < 90 and in_safe_val(v_p6[0], v_p6[1], 5):
                                cands_val.append((v_dd, v_i11, v_p6))
                        if cands_val:
                            cands_val.sort()
                            v_dd, v_i11, v_p6 = cands_val[0]
                            first_val = self.goal_since.setdefault(v_i11, v_t5)
                            if v_t5 - first_val > 25:
                                self.bad[v_i11] = v_t5 + 90
                            else:
                                goal_val3 = (v_p6[0], v_p6[1])
                                label_val = "cube"
                    if goal_val3 is None and move_val is None and v_hpf > 0.6 and v_t5 < 330:
                        # crates: approach and shoot
                        cands_val = []
                        for v_i11, v_p6 in self.cr.items():
                            if self.blocked_id(v_i11, v_t5):
                                continue
                            v_dd = hypot(v_p6[0] - v_x11, v_p6[1] - v_y11)
                            if v_dd < 70 and in_safe_val(v_p6[0], v_p6[1], 5) and v_p6[2] - (v_w5.ground(v_p6[0], v_p6[1]) if v_w5 else 0) < 4:
                                cands_val.append((v_dd, v_i11, v_p6))
                        if cands_val:
                            cands_val.sort()
                            v_dd, v_i11, v_p6 = cands_val[0]
                            first_val = self.goal_since.setdefault(v_i11, v_t5)
                            if v_t5 - first_val > 20:
                                self.bad[v_i11] = v_t5 + 120
                            else:
                                v_cz2 = v_p6[2] + 1.2
                                v_yaw2, pitch_val2 = self.aim_angles(v_x11, v_y11, v_z8 + v_mz, v_p6[0], v_p6[1], v_cz2)
                                v_aim = [v_yaw2, pitch_val2]
                                v_vis = True if v_w5 is None else v_w5.los(v_ex, v_ey, v_ez, v_p6[0], v_p6[1], v_cz2)
                                if v_dd > 22 or not v_vis:
                                    goal_val3 = (v_p6[0], v_p6[1])
                                else:
                                    move_val = (0.0, 0.0)
                                    perr_val = abs(pitch_val2 - v_me3["aim"][1])
                                    if abs(wrap(v_yaw2 - v_me3["aim"][0])) < 0.08 and perr_val < 0.1:
                                        for v_si2, v_wk in enumerate(v_wl):
                                            if v_wk and v_wk["kind"] == "gun" and v_wk.get("ammo", 1) > 0 and v_dd < 150:
                                                fire_val[v_si2] = True
                                            if v_wk and v_wk["kind"] == "lightning" and v_dd < v_wk["stats"]["range"] * 0.9 and energy_val > 30:
                                                fire_val[v_si2] = True
                                label_val = "crate"
                    if goal_val3 is None and move_val is None:
                        v_dc = hypot(camp_val[0] - v_x11, camp_val[1] - v_y11)
                        if v_dc > 8:
                            goal_val3 = camp_val
                            label_val = "camp-go"
                        else:
                            move_val = (0.0, 0.0)
                            label_val = "camp"

        # --- turn goal into move
        if goal_val3 is not None and move_val is None:
            v_wp = self.waypoint(v_x11, v_y11, v_z8, goal_val3, v_t5, v_T02)
            v_gdx, v_gdy = v_wp[0] - v_x11, v_wp[1] - v_y11
            v_gd3 = hypot(v_gdx, v_gdy)
            if hypot(goal_val3[0] - v_x11, goal_val3[1] - v_y11) < 1.2 or v_gd3 < 0.5:
                move_val = (0.0, 0.0)
            else:
                move_val = (v_gdx / v_gd3, v_gdy / v_gd3)

        # stuck detection (only when we wanted to move)
        stuck_val = False
        if self.hist and move_val and hypot(*move_val) > 0.5 and v_t5 - self.hist[0][0] > 1.2:
            v_h02 = self.hist[0]
            if hypot(v_x11 - v_h02[1], v_y11 - v_h02[2]) < 2.0 and not (fire_val[0] or fire_val[1]):
                stuck_val = True
        if stuck_val and v_t5 > self.escape_until:
            self.bias = -self.bias
            self.escape_until = v_t5 + 1.0
            self.escape_dir = self.rng.uniform(-PI, PI)
            self.hist = []
        if v_t5 < self.escape_until:
            move_val = (math.cos(self.escape_dir), math.sin(self.escape_dir))
            jump_val = True
            boost_val = energy_val > 25
            label_val = "unstick"

        if move_val is None:
            move_val = (0.0, 0.0)
        v_mx, v_my = move_val
        if hypot(v_mx, v_my) > 1e-6:
            v_mx, v_my = self.steer(v_x11, v_y11, v_z8, v_mx, v_my)
            if label_val in ("hide", "camp", "ambush"):
                v_mx, v_my = 0.0, 0.0
        else:
            v_mx = v_my = 0.0

        # --- aim default
        if v_aim is None:
            if hypot(v_mx, v_my) > 0.1:
                v_aim = [math.atan2(v_my, v_mx), 0.0]
            else:
                self.sweep += 0.12
                v_aim = [wrap(v_me3["aim"][0] + 0.12), 0.0]

        v_act["move"] = [round(v_mx, 4), round(v_my, 4)]
        v_act["aim"] = [round(v_aim[0], 4), round(v_aim[1], 4)]
        v_act["fire"] = fire_val
        v_act["boost"] = bool(boost_val)
        v_act["jump"] = bool(jump_val)
        self.last_move = (v_mx, v_my)

        # prune bad
        if tick_val % 100 == 0:
            self.bad = {v_k10: v_v2 for v_k10, v_v2 in self.bad.items() if v_v2 > v_t5}
        mf.draw.label(f"{label_val} hp{v_me3['hp']:.0f}")
        return v_act


mf.run(Bot)
