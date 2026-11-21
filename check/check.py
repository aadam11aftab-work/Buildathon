#!/usr/bin/env python3
"""Protocol conformance check for a mechfight/1 bot. Standard library only.

    python3 check/check.py -- python3 -u python/example_bot.py
    python3 check/check.py -- node ts/example.ts
    python3 check/check.py --map-dir /arena -- docker run --rm -i -v "$PWD/arena:/arena:ro" my-bot

It plays a recorded stream (check/stream.jsonl.gz: a hello, 300 observations from a real match, an
end) to your bot and checks what comes back: `ready` in time, one valid action per observation with
the right tick, answers within the time budget, the newest-observation rule when observations queue
up, debug drawings inside the caps, and a clean exit after `end`. Every message is checked against
mechfight.schema.json.

It is not a game. The observations don't react to your actions, so it says nothing about how well your
bot plays: submit it for a practice score.

Correctness (every message, ticks, the newest-observation rule, exiting) fails on the first pass. Missing the
time budget for more than 2% of observations is retried with a fresh bot, up to 3 passes, and the best
counts: a busy machine makes a good bot late now and then, a slow bot is late every time.
"""
import argparse
import gzip
import json
import os
import queue
import re
import subprocess
import sys
import threading
import time

HERE = os.path.dirname(os.path.abspath(__file__))
KIT = os.path.dirname(HERE)


# ------------------------------------------------------------------ a small JSON Schema validator
# Enough of draft-07 for mechfight.schema.json: type, const, enum, properties, required,
# additionalProperties, items, minItems, maxItems, anyOf, oneOf, pattern, $ref.
class Schema:
    def __init__(self, path_val):
        with open(path_val) as v_f:
            self.root = json.load(v_f)

    def errors(self, value_val, name_val):
        v_out = []
        self._check(value_val, {"$ref": "#/definitions/" + name_val}, "$", v_out)
        return v_out

    def _type_ok(self, v_v, v_t):
        return {
            "object": isinstance(v_v, dict),
            "array": isinstance(v_v, list),
            "string": isinstance(v_v, str),
            "boolean": isinstance(v_v, bool),
            "null": v_v is None,
            "number": isinstance(v_v, (int, float)) and not isinstance(v_v, bool),
            "integer": isinstance(v_v, int) and not isinstance(v_v, bool),
        }.get(v_t, True)

    def _check(self, v_v2, v_s, v_at, v_out2):
        if len(v_out2) > 5:
            return
        if "$ref" in v_s:
            v_s = self.root["definitions"][v_s["$ref"].split("/")[-1]]
            return self._check(v_v2, v_s, v_at, v_out2)
        if "anyOf" in v_s or "oneOf" in v_s:
            alts_val = v_s.get("anyOf") or v_s.get("oneOf")
            if not any(not self._sub(v_v2, v_a, v_at) for v_a in alts_val):
                v_out2.append(f"{v_at}: matches none of the allowed shapes")
            return
        v_t2 = v_s.get("type")
        if v_t2 is not None:
            v_ts = v_t2 if isinstance(v_t2, list) else [v_t2]
            if not any(self._type_ok(v_v2, v_x) for v_x in v_ts):
                v_out2.append(f"{v_at}: expected {' or '.join(v_ts)}, got {type(v_v2).__name__}")
                return
        if "const" in v_s and v_v2 != v_s["const"]:
            v_out2.append(f"{v_at}: expected {json.dumps(v_s['const'])}")
        if "enum" in v_s and v_v2 not in v_s["enum"]:
            v_out2.append(f"{v_at}: expected one of {v_s['enum']}")
        if "pattern" in v_s and isinstance(v_v2, str) and not re.search(v_s["pattern"], v_v2):
            v_out2.append(f"{v_at}: {v_v2!r} doesn't match {v_s['pattern']}")
        if isinstance(v_v2, dict):
            props_val = v_s.get("properties", {})
            for v_k in v_s.get("required", []):
                if v_k not in v_v2:
                    v_out2.append(f"{v_at}: missing {v_k!r}")
            for v_k, v_x in v_v2.items():
                if v_k in props_val:
                    self._check(v_x, props_val[v_k], f"{v_at}.{v_k}", v_out2)
                elif isinstance(v_s.get("additionalProperties"), dict):
                    self._check(v_x, v_s["additionalProperties"], f"{v_at}.{v_k}", v_out2)
        if isinstance(v_v2, list):
            if "minItems" in v_s and len(v_v2) < v_s["minItems"]:
                v_out2.append(f"{v_at}: needs at least {v_s['minItems']} items")
            if "maxItems" in v_s and len(v_v2) > v_s["maxItems"]:
                v_out2.append(f"{v_at}: at most {v_s['maxItems']} items")
            items_val = v_s.get("items")
            for v_i, v_x in enumerate(v_v2):
                v_it = items_val[v_i] if isinstance(items_val, list) and v_i < len(items_val) else items_val
                if isinstance(v_it, dict):
                    self._check(v_x, v_it, f"{v_at}[{v_i}]", v_out2)

    def _sub(self, v_v3, v_s2, v_at2):
        v_out3 = []
        self._check(v_v3, v_s2, v_at2, v_out3)
        return v_out3


# ------------------------------------------------------------------ talking to the bot
class Bot:
    def __init__(self, v_cmd):
        self.proc = subprocess.Popen(v_cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, bufsize=0)
        self.lines = queue.Queue()
        threading.Thread(target=self._read, daemon=True).start()

    def _read(self):
        for v_raw in self.proc.stdout:
            self.lines.put((time.perf_counter(), v_raw))
        self.lines.put((time.perf_counter(), None))

    def send(self, v_msg):
        self.send_many([v_msg])

    def send_many(self, msgs_val):
        """One write, so queued observations arrive together."""
        try:
            self.proc.stdin.write("".join(json.dumps(v_m, separators=(",", ":")) + "\n" for v_m in msgs_val).encode())
            self.proc.stdin.flush()
        except (BrokenPipeError, OSError):
            pass

    def next(self, deadline_val):
        """Next line before `deadline` (perf_counter): (time, raw bytes), (time, None) on exit, None on timeout."""
        try:
            return self.lines.get(timeout=max(0.0, deadline_val - time.perf_counter()))
        except queue.Empty:
            return None


def main():
    v_ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0], usage="%(prog)s [options] -- <bot command...>")
    v_ap.add_argument("--stream", default=os.path.join(HERE, "stream.jsonl.gz"))
    v_ap.add_argument("--schema", default=os.path.join(KIT, "mechfight.schema.json"))
    v_ap.add_argument("--map-dir", default=os.path.join(KIT, "arena"), help="what hello.map.dir says (default: the kit's arena/)")
    v_ap.add_argument("cmd", nargs=argparse.REMAINDER)
    v_a2 = v_ap.parse_args()
    v_cmd2 = v_a2.cmd[1:] if v_a2.cmd[:1] == ["--"] else v_a2.cmd
    if not v_cmd2:
        v_ap.error("give the bot command after --")
    schema_val = Schema(v_a2.schema)
    with gzip.open(v_a2.stream, "rt") as v_f2:
        msgs_val2 = [json.loads(v_l) for v_l in v_f2 if v_l.strip()]
    hello_val, v_obs, v_end = msgs_val2[0], [v_m2 for v_m2 in msgs_val2 if v_m2.get("type") == "obs"], msgs_val2[-1]
    hello_val["map"]["dir"] = os.path.abspath(v_a2.map_dir)
    v_cfg = hello_val["config"]
    budget_val = v_cfg["bot"]["tick_budget_ms"] / 1000
    caps_val = v_cfg["debug"]
    late_limit_val = 0.02

    def run(attempt_val):
        """One pass over the stream with a fresh bot: (problems, warnings, counts, times, stats line)."""
        problems_val, warnings_val, times_val = [], [], []
        counts_val = {"answered": 0, "late": 0, "missing": 0, "stale": 0, "wrong_tick": 0, "bad": 0, "debug": 0, "debug_over_caps": 0}
        sent_ticks_val = set()

        def problem(v_s3):
            if len(problems_val) < 30:
                problems_val.append(v_s3)

        def parse(v_raw2, what_val):
            if len(v_raw2.rstrip(b"\n")) > v_cfg["bot"]["max_line_bytes"]:
                counts_val["bad"] += 1
                problem_val(f"{what_val}: line over {v_cfg['bot']['max_line_bytes']} bytes")
                return None
            try:
                v_m2 = json.loads(v_raw2)
            except ValueError:
                counts_val["bad"] += 1
                problem_val(f"{what_val}: not JSON: {v_raw2[:80]!r}")
                return None
            if not isinstance(v_m2, dict):
                counts_val["bad"] += 1
                problem_val(f"{what_val}: not a JSON object")
                return None
            return v_m2

        if attempt_val == 1:
            print(f"bot: {' '.join(v_cmd2)}")
        v_bot = Bot(v_cmd2)
        v_t0 = time.perf_counter()
        v_bot.send(hello_val)
        ready_val = None
        while ready_val is None:
            v_got = v_bot.next(v_t0 + v_cfg["bot"]["hello_budget_ms"] / 1000)
            if v_got is None or v_got[1] is None:
                problems_val.append(f"no `ready` within {v_cfg['bot']['hello_budget_ms']} ms" if v_got is None else "the bot exited before `ready`")
                v_bot.proc.kill()
                return problems_val, warnings_val, counts_val, times_val, None
            v_m2 = parse_val(v_got[1], "ready")
            if v_m2 is not None and v_m2.get("type") == "ready":
                ready_val = v_m2
        for v_e in schema_val.errors(ready_val, "Ready"):
            problem_val(f"ready: {v_e}")
        if attempt_val == 1:
            print(f"ready in {(time.perf_counter() - v_t0) * 1000:.0f} ms: name {ready_val.get('name')!r}")

        def check_action(v_m2, v_o):
            for v_e in schema_val.errors(v_m2, "Action"):
                problem_val(f"tick {v_o['tick']}: {v_e}")
            v_d = v_m2.get("debug")
            if isinstance(v_d, dict):
                counts_val["debug"] += 1
                over_val = [v_k2 for v_k2, v_c in (("lines", "max_lines"), ("points", "max_points"), ("text", "max_text")) if len(v_d.get(v_k2) or []) > caps_val[v_c]]
                if len(json.dumps(v_d, separators=(",", ":")).encode()) > caps_val["max_bytes"]:
                    over_val.append("bytes")
                if any(len(v_t3.get("text", "")) > caps_val["max_text_chars"] for v_t3 in v_d.get("text") or [] if isinstance(v_t3, dict)) or len(str(v_d.get("label", ""))) > caps_val["max_text_chars"]:
                    over_val.append("text length")
                if over_val:
                    counts_val["debug_over_caps"] += 1
                    if counts_val["debug_over_caps"] <= 3:
                        warnings_val.append(f"tick {v_o['tick']}: debug drawing over the caps ({', '.join(over_val)}): the server cuts it")

        def ask(batch_val):
            """Send observations back to back; the answer that counts is the one for the last."""
            want_val = batch_val[-1]
            sent_val = time.perf_counter()
            sent_ticks_val.update(v_o["tick"] for v_o in batch_val)
            v_bot.send_many(batch_val)
            while True:
                v_got = v_bot.next(sent_val + budget_val)
                if v_got is None:
                    counts_val["missing"] += 1
                    # A late answer still arrives: drain it so it isn't read as the next one.
                    late_val = v_bot.next(sent_val + budget_val + 0.25)
                    if late_val is not None and late_val[1] is not None:
                        counts_val["late"] += 1
                        times_val.append(late_val[0] - sent_val)
                        v_m2 = parse_val(late_val[1], f"tick {want_val['tick']} (late)")
                        if v_m2 is not None and v_m2.get("tick") == want_val["tick"]:
                            check_action_val(v_m2, want_val)
                    return True
                v_at3, v_raw2 = v_got
                if v_raw2 is None:
                    problem_val(f"the bot exited at tick {want_val['tick']}")
                    return False
                v_m2 = parse_val(v_raw2, f"tick {want_val['tick']}")
                if v_m2 is None:
                    continue
                if v_m2.get("tick") != want_val["tick"]:
                    if v_m2.get("tick") in sent_ticks_val:
                        # an answer to an older observation (queued, or one it answered too late): dropped, a timing matter
                        counts_val["stale"] += 1
                    else:
                        counts_val["wrong_tick"] += 1
                        problem_val(f"tick {want_val['tick']}: answer carries tick {v_m2.get('tick')!r}")
                    continue
                counts_val["answered"] += 1
                times_val.append(v_at3 - sent_val)
                check_action_val(v_m2, want_val)
                return True

        alive_val = True
        for v_i2, v_o in enumerate(v_obs):
            # Every 25th observation, three arrive at once (as when a bot falls behind): answer the newest.
            batch_val = v_obs[max(0, v_i2 - 2) : v_i2 + 1] if v_i2 % 25 == 24 else [v_o]
            if not v_ask(batch_val):
                alive_val = False
                break
            # No answer at all, not even a late one: the bot is stuck, not slow.
            if counts_val["missing"] - counts_val["late"] + counts_val["wrong_tick"] >= 20:
                problem_val("stopped early: 20 observations without any answer")
                break
        if alive_val:
            v_bot.send(v_end)
            try:
                v_bot.proc.stdin.close()
            except OSError:
                pass
            try:
                v_bot.proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                warnings_val.append("the bot didn't exit within 2 s of `end` (the server kills it)")
                v_bot.proc.kill()
        else:
            v_bot.proc.kill()
        v_n = len(v_obs)
        stats_val = f"observations: {v_n}  answered in time: {counts_val['answered']}  missed the {budget_val * 1000:.0f} ms budget: {counts_val['missing']} ({counts_val['late']} came late)"
        return problems_val, warnings_val, counts_val, sorted(times_val), stats_val

    # Correctness first: anything wrong in what the bot says fails at once. Only lateness is retried: on a busy
    # machine (a scorer running other matches) a good bot can miss the budget now and then, while a slow bot
    # misses it every time. Up to 3 passes, each with a fresh bot; the best one counts.
    v_n = len(v_obs)
    for attempt_val in range(1, 4):
        problems_val, warnings_val, counts_val, times_val, stats_val = v_run(attempt_val)
        slow_val = stats_val is not None and counts_val["missing"] > v_n * late_limit_val
        if problems_val or not slow_val or attempt_val == 3:
            break
        print(f"pass {attempt_val}: {counts_val['missing']} of {v_n} observations missed the {budget_val * 1000:.0f} ms budget; this machine may be busy, running the stream again with a fresh bot")
    if stats_val is None:
        print(f"FAIL: {problems_val[0]}")
        return 1
    v_pct = lambda v_p: times_val[min(len(times_val) - 1, int(v_p * len(times_val)))] * 1000 if times_val else 0.0
    if attempt_val > 1:
        print(f"pass {attempt_val} of 3:")
    print(stats_val)
    print(f"answer time ms: p50 {v_pct(0.5):.1f}  p90 {v_pct(0.9):.1f}  p99 {v_pct(0.99):.1f}  max {v_pct(1.0):.1f}  (this machine, not the 1-CPU match container)")
    print(f"answers to older queued observations (dropped): {counts_val['stale']}  wrong tick: {counts_val['wrong_tick']}  bad lines: {counts_val['bad']}  debug drawings: {counts_val['debug']} ({counts_val['debug_over_caps']} over the caps)")
    if slow_val:
        problems_val.append(f"{counts_val['missing']} of {v_n} observations got no answer within {budget_val * 1000:.0f} ms" + (f" (best of {attempt_val} passes)" if attempt_val > 1 else ""))
    for v_w in warnings_val:
        print(f"warning: {v_w}")
    for v_p in problems_val:
        print(f"problem: {v_p}")
    print("FAIL" if problems_val else "PASS")
    return 1 if problems_val else 0

if __name__ == "__main__":
    sys.exit(main())
