"""mechfight/1 bot wrapper. One file, standard library only (Python 3.9+): copy it next to your bot.

    import mechfight as mf

    class Bot:
        name = "my-bot"
        def __init__(self, hello: mf.Hello): ...
        def act(self, obs: mf.Observation) -> mf.Action:
            mf.draw.label("hello")              # optional debug drawing, only your team sees it
            return {"move": [1, 0]}

    mf.run(Bot)

What run() does for you (protocol.md section 2):
- answers `hello` with `ready` (your class's `name`), then calls act() for each observation;
- if observations queue up, answers only the newest one;
- sets `tick` on your action (you don't have to);
- if __init__ or act() raises, logs the traceback to stderr and keeps going: it re-sends your last
  good move/aim/boost, so the mech keeps coasting instead of crashing;
- attaches whatever you drew with `mf.draw` this tick as `action["debug"]`;
- exits on `end`.

The types below are generated from mechfight.schema.json; they are TypedDicts, so at runtime every
message is a plain dict. Fields may be added to the protocol: read them with .get() if unsure.
"""
from __future__ import annotations

import json
import queue
import sys
import threading
import traceback
from typing import Any, Dict, List, Literal, Optional, TypedDict, Union

try:
    from typing import NotRequired
except ImportError:  # Python < 3.11: only type checkers read it (annotations are lazy above)
    pass

# --- generated from mechfight.schema.json: do not edit ---
# Every message a bot sends the server.
BotMessage = Union["Ready", "Action"]
# A color: "#rgb" or "#rrggbb". Anything else is replaced by your mech's color.
Color = str
# cookoff: laser on a mortar carrier. short: lightning on a laser carrier. concuss: any mortar blast.
EffectName = Literal["cookoff", "short", "concuss"]
GameEvent = Union["DamagedEvent", "HitEvent", "EffectEvent", "DropEvent", "KillEvent", "PickupEvent", "UpgradeEvent", "TookEvent", "DroppedEvent", "TakeFailedEvent"]
MechStats = Dict[str, float]
# Every message the server sends a bot.
ServerMessage = Union["Hello", "Observation", "EndMsg"]
Vec2 = List[float]
Vec3 = List[float]
WeaponKind = Literal["gun", "mortar", "laser", "lightning"]

class Action(TypedDict):
    type: NotRequired[Literal["action"]]
    # echo the observation tick; stale answers are dropped
    tick: float
    # desired horizontal direction in world frame, length <= 1
    move: NotRequired[Vec2]
    boost: NotRequired[bool]
    jump: NotRequired[bool]
    # desired [yaw, pitch]; the torso slews toward it at turn_rate
    aim: NotRequired[Vec2]
    fire: NotRequired[List[bool]]
    reload: NotRequired[bool]
    # index into the current offer's choices
    pick: NotRequired[float]
    # id of a weapon pickup within loot.pickup_radius
    take: NotRequired[float]
    # slot (0/1) to empty: with `take`, the slot the new weapon replaces; alone, just drops it
    drop: NotRequired[float]
    # debug drawing, shown only to your team; never affects the match
    debug: NotRequired[DebugDraw]

class ActorObs(TypedDict):
    id: float
    kind: Literal["mech", "npc"]
    pos: Vec3
    vel: Vec3
    aim: Vec2
    hp: NotRequired[float]
    max_hp: NotRequired[float]
    cubes: NotRequired[float]
    # weapons held (empty slots omitted)
    weapons: NotRequired[List[WeaponKind]]
    # laser charging (telegraph)
    charging: NotRequired[bool]

class BeamObs(TypedDict):
    """an instant shot (laser beam or lightning arc) by another mech, from its muzzle along `path`"""
    by: float
    kind: Literal["laser", "lightning"]
    path: List[Vec3]
    age: float

class ConfigVision(TypedDict):
    range: float
    fov_deg: float
    near_range: float
    canopy_blocks: bool
    show_enemy_hp: bool
    memory_s: NotRequired[float]

class ConfigSound(TypedDict):
    bearing_noise_deg: float
    near: float
    mid: float
    step_speed: float
    step_every: float
    range: Dict[str, float]

class ConfigWeapons(TypedDict):
    gun: Dict[str, float]
    mortar: Dict[str, float]
    laser: Dict[str, float]
    lightning: Dict[str, float]

class ConfigCubes(TypedDict):
    upgrade_thresholds: List[float]

class ConfigLootWeights(TypedDict):
    gun: float
    mortar: float
    laser: float
    lightning: float

class ConfigLoot(TypedDict):
    pickup_radius: float
    equip_s: float
    floor_count: float
    floor_roof_fraction: float
    crate_weapon_chance: float
    death_drop: float
    weights: ConfigLootWeights

class ConfigDrops(TypedDict):
    """Supply drops (optional: absent = none)."""
    times: List[float]
    fall_s: float
    radius: float
    capture_s: float
    cubes: float
    repair: float
    spread: float

class ConfigZone(TypedDict):
    start_center: List[float]
    start_radius: float
    phases: List[ZonePhase]

class Config(TypedDict):
    match: Dict[str, float]
    bot: Dict[str, float]
    mech: MechStats
    vision: ConfigVision
    sound: ConfigSound
    weapons: ConfigWeapons
    projectile: Dict[str, float]
    cubes: ConfigCubes
    crates: Dict[str, float]
    loot: ConfigLoot
    npc: Dict[str, float]
    # Supply drops (optional: absent = none).
    drops: NotRequired[ConfigDrops]
    zone: ConfigZone
    replay: Dict[str, float]
    # Caps on action.debug: the server silently cuts each decision's drawing to these.
    debug: DebugCaps
    upgrades: List[UpgradeDef]

class DamagedEvent(TypedDict):
    """You took damage."""
    type: Literal["damaged"]
    amount: float
    # gun, mortar, laser, lightning, npc or zone
    weapon: str
    # world-frame yaw toward the source (null for the zone)
    bearing: Optional[float]
    # attacker id (null for the zone)
    by: Optional[float]
    # it came from outside your front arc (x mech.rear_mult)
    rear: NotRequired[bool]

class DebugCaps(TypedDict):
    """Caps on the debug drawing a bot may attach to an action."""
    max_lines: float
    max_points: float
    max_text: float
    max_text_chars: float
    max_bytes: float
    max_match_bytes: float
    hold_s: float

class DebugDraw(TypedDict):
    """Debug drawing for one decision. Stays on screen until your next decision that carries `debug`, or `debug.hold_s` seconds, whichever comes first; send `{}` to clear it early. The server truncates to the `[debug]` caps in hello.config (item counts, text length, bytes per decision, bytes per match) without telling you. It is recorded to a file only your team can download, never to the public replay, and has no effect on the match."""
    lines: NotRequired[List[DebugLine]]
    points: NotRequired[List[DebugPoint]]
    text: NotRequired[List[DebugText]]
    # one short status line drawn above your mech
    label: NotRequired[str]

class DebugLine(TypedDict):
    """A line segment from world point `a` to `b` (x east, y north, z up, meters)."""
    a: Vec3
    b: Vec3
    color: NotRequired[Color]

class DebugPoint(TypedDict):
    """A dot at a world point. `size` is its radius in screen pixels (1..12, default 4)."""
    at: Vec3
    color: NotRequired[Color]
    size: NotRequired[float]

class DebugText(TypedDict):
    """A text label. With `at`, drawn at that world point; without, stacked above your mech."""
    text: str
    at: NotRequired[Vec3]
    color: NotRequired[Color]

class DropEvent(TypedDict):
    """You captured supply drop `id`; `cubes` is your new total."""
    type: Literal["drop"]
    id: float
    cubes: float

class DropObs(TypedDict):
    """A supply drop. Known to every mech from the moment it is called, like the zone."""
    id: float
    pos: Vec3
    # stand inside this, alone, for capture_s
    radius: float
    # seconds until it can be captured (0 = on the ground)
    lands_in: float
    capture_s: float
    # seconds the current holder has banked
    progress: float
    # mech currently capturing
    holder: Optional[float]

class DroppedEvent(TypedDict):
    """A weapon left your `slot` and lies on the ground as pickup `id`."""
    type: Literal["dropped"]
    id: float
    kind: WeaponKind
    slot: float

class Effect(TypedDict):
    path: str
    mul: NotRequired[float]
    add: NotRequired[float]

class EffectEvent(TypedDict):
    """A signature effect landed on you."""
    type: Literal["effect"]
    effect: EffectName
    by: Optional[float]

class EndMsg(TypedDict):
    type: Literal["end"]
    place: float
    kills: float

class HelloMap(TypedDict):
    dir: str
    files: List[str]

class Hello(TypedDict):
    type: Literal["hello"]
    protocol: Literal["mechfight/1"]
    # your mech id, 0..15
    slot: float
    # per-bot seed, use it for your own RNG to stay reproducible
    seed: float
    map: HelloMap
    config: Config

class HitEvent(TypedDict):
    """You damaged `target` (a mech, NPC or crate id)."""
    type: Literal["hit"]
    target: float
    amount: float
    weapon: str
    effect: NotRequired[EffectName]
    rear: NotRequired[bool]

class ItemObs(TypedDict):
    id: float
    pos: Vec3
    hp: NotRequired[float]

class KillEvent(TypedDict):
    """You killed a mech."""
    type: Literal["kill"]
    target: float

class Observation(TypedDict):
    type: Literal["obs"]
    tick: float
    # match time, seconds
    t: float
    # mechs alive (not NPCs)
    alive: float
    self: SelfObs
    # visible mechs and NPCs
    mechs: List[ActorObs]
    projectiles: List[ProjectileObs]
    crates: List[ItemObs]
    cubes: List[ItemObs]
    # visible weapons on the ground
    pickups: List[PickupObs]
    sounds: List[SoundObs]
    # laser beams and lightning arcs by other mechs that you saw (any point of the path in view) since your last observation
    beams: List[BeamObs]
    events: List[GameEvent]
    zone: ZoneObs
    offer: Optional[Offer]
    # supply drops called or on the ground
    drops: List[DropObs]

class Offer(TypedDict):
    id: float
    choices: List[OfferChoice]
    expires: float

class OfferChoice(TypedDict):
    id: str
    name: str
    desc: str

class PickupEvent(TypedDict):
    """You picked up a cube; `cubes` is your new total."""
    type: Literal["pickup"]
    cubes: float

class PickupObs(TypedDict):
    """A weapon lying on the ground. Take it with the `take` action."""
    id: float
    kind: WeaponKind
    pos: Vec3

class ProjectileObs(TypedDict):
    id: float
    kind: Literal["gun", "mortar", "npc"]
    pos: Vec3
    vel: Vec3

class Ready(TypedDict):
    type: Literal["ready"]
    name: NotRequired[str]

class SelfObs(TypedDict):
    id: float
    pos: Vec3
    vel: Vec3
    # [yaw, pitch] actual (slewed) aim
    aim: Vec2
    hp: float
    max_hp: float
    energy: float
    max_energy: float
    grounded: bool
    cubes: float
    upgrades: List[str]
    # null = empty slot
    weapons: List[Optional[WeaponObs]]
    in_zone: bool
    # your current [mech] numbers after upgrades
    stats: Dict[str, float]

class SoundObs(TypedDict):
    kind: str
    bearing: float
    dist: Literal["near", "mid", "far"]
    age: float

class TakeFailedEvent(TypedDict):
    """Your `take` didn't happen."""
    type: Literal["take_failed"]
    id: float
    reason: Literal["gone", "range", "have", "full", "busy"]

class TookEvent(TypedDict):
    """Your `take` succeeded: pickup `id` is now in `slot`."""
    type: Literal["took"]
    id: float
    kind: WeaponKind
    slot: float

class UpgradeDef(TypedDict):
    id: str
    name: str
    desc: str
    requires: NotRequired[WeaponKind]
    max: float
    effects: List[Effect]

class UpgradeEvent(TypedDict):
    """An upgrade was applied."""
    type: Literal["upgrade"]
    id: str

class WeaponObs(TypedDict):
    kind: WeaponKind
    # seconds until it can fire again
    cooldown: float
    # gun only
    ammo: NotRequired[float]
    # gun only: seconds of reload left (0 = not reloading)
    reload: NotRequired[float]
    # laser only: seconds charged so far (0 = not charging); it fires at weapons.laser.charge, or on release after min_charge
    charge: NotRequired[float]
    # this weapon's [weapons.<kind>] numbers after upgrades
    stats: Dict[str, float]

class ZoneObs(TypedDict):
    center: Vec2
    radius: float
    next_center: Vec2
    next_radius: float
    # match time (s) the current shrink starts (or started)
    shrink_start: float
    shrink_end: float
    dps: float

class ZonePhase(TypedDict):
    hold_s: float
    shrink_s: float
    radius: float
    dps: float
# --- end generated ---


class Draw:
    """Debug drawing for the current decision. World coordinates (x east, y north, z up, meters).
    Colors are "#rgb" or "#rrggbb"; without one, your mech's color. The server caps it
    (hello["config"]["debug"]); this helper stops adding at the same caps so nothing is wasted."""

    def __init__(self) -> None:
        self.caps: Dict[str, float] = {}
        self.clear()

    def clear(self) -> None:
        self._d: DebugDraw = {}

    def _room(self, v_key: str, v_cap: str) -> bool:
        return len(self._d.get(v_key, [])) < self.caps.get(v_cap, 1e9)  # type: ignore[arg-type]

    def line(self, v_a: List[float], v_b: List[float], color_val: Optional[str] = None) -> None:
        if self._room("lines", "max_lines"):
            v_it: DebugLine = {"a": [float(v_v) for v_v in v_a[:3]], "b": [float(v_v) for v_v in v_b[:3]]}
            if color_val:
                v_it["color"] = color_val
            self._d.setdefault("lines", []).append(v_it)

    def point(self, v_at: List[float], color_val2: Optional[str] = None, size_val: Optional[float] = None) -> None:
        if self._room("points", "max_points"):
            v_it2: DebugPoint = {"at": [float(v_v2) for v_v2 in v_at[:3]]}
            if color_val2:
                v_it2["color"] = color_val2
            if size_val is not None:
                v_it2["size"] = size_val
            self._d.setdefault("points", []).append(v_it2)

    def text(self, text_val: str, v_at2: Optional[List[float]] = None, color_val3: Optional[str] = None) -> None:
        """Text at a world point, or (without `at`) stacked above your mech."""
        if self._room("text", "max_text"):
            v_it3: DebugText = {"text": str(text_val)}
            if v_at2 is not None:
                v_it3["at"] = [float(v_v3) for v_v3 in v_at2[:3]]
            if color_val3:
                v_it3["color"] = color_val3
            self._d.setdefault("text", []).append(v_it3)

    def label(self, text_val2: str) -> None:
        """One short status line above your mech."""
        self._d["label"] = str(text_val2)

    def take(self) -> Optional[DebugDraw]:
        v_d, self._d = self._d, {}
        return v_d or None


draw = Draw()
_CONTINUOUS = ("move", "aim", "boost")


def log(*args: Any) -> None:
    """print() to stderr: stdout is the protocol. Your team sees stderr after each match."""
    print(*args, file=sys.stderr, flush=True)


def _send(v_msg: Dict[str, Any]) -> None:
    sys.stdout.write(json.dumps(v_msg, separators=(",", ":")) + "\n")
    sys.stdout.flush()


def _reader(v_q: "queue.Queue[Optional[str]]") -> None:
    for line_val in sys.stdin:
        if line_val.strip():
            v_q.put(line_val)
    v_q.put(None)


def run(make_bot_val: Any) -> None:
    """Run a bot until the match ends. `make_bot(hello)` returns an object with `act(obs) -> Action`
    and optionally `name`. A class works: `run(MyBot)`."""
    v_q2: "queue.Queue[Optional[str]]" = queue.Queue()
    threading.Thread(target=_reader, args=(v_q2,), daemon=True).start()
    v_bot = None
    last_val: Dict[str, Any] = {}
    errors_val = 0
    while True:
        batch_val = [v_q2.get()]
        while True:  # drain the queue: only the newest observation matters
            try:
                batch_val.append(v_q2.get_nowait())
            except queue.Empty:
                break
        latest_val = None
        for v_raw in batch_val:
            if v_raw is None:
                return
            try:
                v_msg2 = json.loads(v_raw)
            except ValueError:
                continue
            kind_val = v_msg2.get("type")
            if kind_val == "hello":
                draw.caps = v_msg2.get("config", {}).get("debug", {})
                try:
                    v_bot = make_bot_val(v_msg2)
                except Exception:
                    log(traceback.format_exc())
                name_val = getattr(v_bot, "name", None) or getattr(make_bot_val, "__name__", "bot")
                _send({"type": "ready", "name": str(name_val)[:32]})
            elif kind_val == "obs":
                latest_val = v_msg2
            elif kind_val == "end":
                return
        if latest_val is None:
            continue
        action_val: Dict[str, Any] = {}
        draw.clear()
        if v_bot is not None:
            try:
                action_val = dict(v_bot.act(latest_val) or {})
                last_val = {v_k: action_val[v_k] for v_k in _CONTINUOUS if v_k in action_val}
            except Exception:
                errors_val += 1
                if errors_val <= 5 or errors_val % 100 == 0:
                    log(f"act() failed at tick {latest_val['tick']} ({errors_val} so far):\n{traceback.format_exc()}")
                action_val = dict(last_val)
        action_val["tick"] = latest_val["tick"]
        v_d2 = draw.take()
        if v_d2 and "debug" not in action_val:
            action_val["debug"] = v_d2
        try:
            _send(action_val)
        except (TypeError, ValueError):  # something in the action isn't JSON
            log(f"unserializable action at tick {latest_val['tick']}:\n{traceback.format_exc()}")
            _send({"tick": latest_val["tick"], **last_val})


if __name__ == "__main__":
    log("mechfight.py is a library: import it from your bot and call run()")
