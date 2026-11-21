// Approximate kinematic sandbox: our Brain against a hunting squad on the real map.
// Not the real game: it is for finding stuck spots, zone deaths, firing bugs, timing, and for
// sanity-checking tactics. House bots here are guesses.
//   node tools/sim.ts [matches=8] [seed=1] [omniscient=0] [kit=../mechfight-kit]
import { readFileSync } from "node:fs"
import { gunzipSync } from "node:zlib"
import { Brain } from "../src/brain.ts"
import { GameMap } from "../src/map.ts"
import { Navigator } from "../src/nav.ts"
import { draw, type Observation } from "../mechfight.ts"
import { angDiff, rng, type V2, type V3 } from "../src/geom.ts"

const MATCH_TOTAL = Number(process.argv[2] ?? 8)
const BASE_SEED = Number(process.argv[3] ?? 1)
const OMNISCIENT = process.argv[4] === "1"
const KIT_PATH = process.argv[5] ?? "../mechfight-kit"
const CHATTY = process.env.SIMV === "1"
// house strength 0..1 (aim error, laser share); calibrated so that v1 scores like the real board
const STRENGTH = Number(process.env.SIMHARD ?? -0.5)
// SIMMODE=real: house bots modelled on the recorded hunter (laser snipers, precise aim) that must
// find us through squad vision; SIMERR adds aim error (rad), SIMOMNI=1 makes hunters omniscient
const REALISTIC = process.env.SIMMODE === "real"
const REALISTIC_JITTER = Number(process.env.SIMERR ?? 0.02)
const REALISTIC_OMNI = process.env.SIMOMNI === "1"
// SIMFFA=1: free-for-all, every house bot fights the nearest mech it sees (like qualifying/final)
const FREE_FOR_ALL = process.env.SIMFFA === "1" || !!process.env.SIMBOTS
// SIMBOTS=econ|practice|mix: the 15 opponents run our own cores (TTUN-like econ, Lance-like practice) in a free-for-all
const BOT_MODE = process.env.SIMBOTS ?? ""
// SIMINVIS=s: measurement only, nobody sees us for the first s seconds
const INVISIBLE_SECS = Number(process.env.SIMINVIS ?? 0)
// SIMCANOPY=0 turns off the tree crowns in vision (on by default: the server fades sight through dense crowns beyond near range)
const TREE_CANOPY = process.env.SIMCANOPY !== "0"
// SIMMODE=lance: house model of a public 4.75 bot's sim (all 15 hunt within a tier chase radius,
// tier fire ranges, aim noise per tier x SIMLF, lasers on the 3 experts)
const LANCE_MODE = process.env.SIMMODE === "lance"
const LANCE_FACTOR = Number(process.env.SIMLF ?? 1)
const SCHEDULED = process.env.SIMSCHED === "1"
const LANCE_JITTER = [4.0, 2.5, 1.3, 0.7], LANCE_PURSUIT = [150, 260, 420, 1e9], LANCE_REACH = [110, 140, 170, 200]
const textLines4 = gunzipSync(readFileSync(KIT_PATH + "/check/stream.jsonl.gz")).toString("utf8").trim().split("\n")
const INIT_MSG = JSON.parse(textLines4[0])
INIT_MSG.map.dir = KIT_PATH + "/arena"
const CONST_C = INIT_MSG.config
const WORLD_MAP = new GameMap(INIT_MSG.map.dir)
const STEP_DT = 1 / CONST_C.match.tick_hz
const REQ_TOTAL = CONST_C.mech.radius + 0.9
const DEG_TO_RAD = Math.PI / 180

type W = { kind: string; cooldown: number; ammo?: number; reload?: number; charge?: number; stats: Record<string, number> }
type Ent = { id: number; kind: "mech" | "npc" | "crate"; pos: V3; vel: V3; hp: number; maxHp: number; alive: boolean; radius: number; height: number; aim: V2 }
type Mech = Ent & {
  cmdAim: V2; energy: number; spendT: number; grounded: boolean; weapons: (W | null)[]; deathTick: number; cubes: number; armor: number
  lastCombatT: number; seen: Map<number, number>; brain: Brain | null; nav: Navigator | null
  move: V2; boost: boolean; jump: boolean; fire: [boolean, boolean]; skill: number; hunter: boolean; events: any[]; sounds: any[]
  offer: any; offersPending: number; upgrades: string[]; stats: Record<string, number>; hitByUsT: number; flipT: number; side: number
}
type Npc = Ent & { home: V2; target: number; cd: number; goal: V2 }
type Shot = { by: number; pos: V3; vel: V3; ttl: number; dmg: number; kind: string }

const buildWeapon = (category: string): W => {
  const step2 = { ...CONST_C.weapons[category] }
  return category === "gun" ? { kind: category, cooldown: 0, ammo: step2.mag, reload: 0, stats: step2 } : category === "laser" ? { kind: category, cooldown: 0, charge: 0, stats: step2 } : { kind: category, cooldown: 0, stats: step2 }
}
const normalNoise = (bigR3: () => number) => Math.sqrt(-2 * Math.log(bigR3() + 1e-12)) * Math.cos(2 * Math.PI * bigR3())

function zoneAtTime(stages: { c: V2; r: number; t0: number; t1: number; dps: number }[], timeVal2: number) {
  let previous = stages[0]
  for (let index = 1; index < stages.length; index++) {
    const point3 = stages[index]
    if (timeVal2 < point3.t0) return { c: previous.c, r: previous.r, dps: point3.dps, next: point3, prev: previous }
    if (timeVal2 < point3.t1) {
      const entry2 = (timeVal2 - point3.t0) / (point3.t1 - point3.t0)
      return { c: [previous.c[0] + (point3.c[0] - previous.c[0]) * entry2, previous.c[1] + (point3.c[1] - previous.c[1]) * entry2] as V2, r: previous.r + (point3.r - previous.r) * entry2, dps: point3.dps, next: point3, prev: previous }
    }
    previous = point3
  }
  return { c: previous.c, r: previous.r, dps: 260, next: previous, prev: previous }
}

function playMatch(rngSeed: number) {
  const bigR4 = rng(rngSeed)
  const wallStart = performance.now()
  const stages2: { c: V2; r: number; t0: number; t1: number; dps: number }[] = [{ c: CONST_C.zone.start_center, r: CONST_C.zone.start_radius, t0: 0, t1: 0, dps: 0 }]
  let tickTotal = 0
  for (const phaseNo of CONST_C.zone.phases) {
    const previous2 = stages2[stages2.length - 1]
    let cell4: V2 = previous2.c
    for (let key12 = 0; key12 < 40; key12++) {
      const lhs12 = bigR4() * Math.PI * 2, gap3 = bigR4() * (previous2.r - phaseNo.radius) * 0.9
      cell4 = [previous2.c[0] + Math.cos(lhs12) * gap3, previous2.c[1] + Math.sin(lhs12) * gap3]
      const count7 = WORLD_MAP.nodeOf(cell4[0], cell4[1])
      if (count7 >= 0 && WORLD_MAP.nClear[count7] > 4) break
    }
    stages2.push({ c: cell4, r: phaseNo.radius, t0: tickTotal + phaseNo.hold_s, t1: tickTotal + phaseNo.hold_s + phaseNo.shrink_s, dps: phaseNo.dps })
    tickTotal += phaseNo.hold_s + phaseNo.shrink_s
  }
  const component2 = WORLD_MAP.comp(REQ_TOTAL)
  const floorLevel = (minGap = REQ_TOTAL + 1, withinRange = CONST_C.zone.start_radius * 0.8): V3 => {
    for (;;) {
      const lhs13 = bigR4() * Math.PI * 2, gap4 = Math.sqrt(bigR4()) * withinRange
      const posX5 = CONST_C.zone.start_center[0] + Math.cos(lhs13) * gap4, posY6 = CONST_C.zone.start_center[1] + Math.sin(lhs13) * gap4
      const count8 = WORLD_MAP.nodeOf(posX5, posY6)
      if (count8 >= 0 && component2[count8] === 0 && WORLD_MAP.nClear[count8] > minGap) return [posX5, posY6, WORLD_MAP.surfAt(posX5, posY6)]
    }
  }
  const mechList: Mech[] = []
  for (let index2 = 0; index2 < 16; index2++) {
    let position = floorLevel()
    for (let key13 = 0; key13 < 30 && mechList.some((other5) => Math.hypot(other5.pos[0] - position[0], other5.pos[1] - position[1]) < 110); key13++) position = floorLevel()
    const proficiency = index2 === 0 ? 1 : index2 <= 4 ? 0.3 : index2 <= 8 ? 0.5 : index2 <= 12 ? 0.7 : 0.9
    const mechRef7: Mech = {
      id: index2, kind: "mech", pos: position, vel: [0, 0, 0], aim: [bigR4() * 6.28 - 3.14, 0], cmdAim: [0, 0], hp: CONST_C.mech.hp, maxHp: CONST_C.mech.hp, alive: true, radius: CONST_C.mech.radius, height: CONST_C.mech.height,
      energy: CONST_C.mech.energy, spendT: -9, grounded: true, weapons: [buildWeapon("gun"), null], deathTick: 1e9, cubes: 0, armor: 0, lastCombatT: -99, seen: new Map(),
      brain: null, nav: index2 === 0 ? null : new Navigator(WORLD_MAP), move: [0, 0], boost: false, jump: false, fire: [false, false], skill: proficiency, hunter: false, events: [], sounds: [],
      offer: null, offersPending: 0, upgrades: [], stats: { ...CONST_C.mech }, hitByUsT: -99, flipT: 0, side: 1,
    }
    mechRef7.cmdAim = [...mechRef7.aim] as V2
    ;(mechRef7 as any).tier = index2 <= 4 ? 0 : index2 <= 8 ? 1 : index2 <= 12 ? 2 : 3
    if (LANCE_MODE) {
      if (index2 >= 13) mechRef7.weapons[1] = buildWeapon("laser")
    } else if (REALISTIC) {
      if (index2 > 0 && bigR4() < 0.5 + proficiency * 0.5) mechRef7.weapons[1] = buildWeapon(bigR4() < 0.75 ? "laser" : "lightning")
    } else if (index2 > 0 && bigR4() < (0.2 + proficiency * 0.6) * (0.4 + STRENGTH)) mechRef7.weapons[1] = buildWeapon(bigR4() < 0.4 + 0.4 * STRENGTH ? "laser" : "lightning")
    mechList.push(mechRef7)
  }
  mechList[0].brain = new Brain({ ...INIT_MSG, slot: 0, seed: rngSeed * 7 + 1 })
  ;(globalThis as any).__ECODBG = false
  if (BOT_MODE)
    for (let index3 = 1; index3 < 16; index3++) {
      const engineCore = BOT_MODE === "mix" ? (index3 % 2 ? "econ" : "practice") : (BOT_MODE as "econ" | "practice")
      mechList[index3].brain = new Brain({ ...INIT_MSG, slot: index3, seed: rngSeed * 7 + 1 + index3 * 13 }, { map: WORLD_MAP, core: engineCore, noWarm: true })
    }
  const houseBots: Npc[] = []
  for (let key14 = 0; key14 < CONST_C.npc.count; key14++) {
    const point4 = floorLevel(3)
    houseBots.push({ id: 100 + key14, kind: "npc", pos: point4, vel: [0, 0, 0], hp: CONST_C.npc.hp, maxHp: CONST_C.npc.hp, alive: true, radius: CONST_C.npc.radius, height: CONST_C.npc.height, aim: [0, 0], home: [point4[0], point4[1]], target: -1, cd: 0, goal: [point4[0], point4[1]] })
  }
  const lootBoxes: Ent[] = []
  for (let key15 = 0; key15 < Math.round(CONST_C.crates.count * (1 - CONST_C.crates.roof_fraction)); key15++) {
    const point5 = floorLevel(3)
    lootBoxes.push({ id: 1000 + key15, kind: "crate", pos: point5, vel: [0, 0, 0], hp: CONST_C.crates.hp, maxHp: CONST_C.crates.hp, alive: true, radius: CONST_C.crates.radius, height: CONST_C.crates.height, aim: [0, 0] })
  }
  const collectibles: { id: number; kind: string; pos: V3 }[] = []
  let playerId = 8000
  for (let key16 = 0; key16 < Math.round(CONST_C.loot.floor_count * (1 - CONST_C.loot.floor_roof_fraction)); key16++) collectibles.push({ id: playerId++, kind: ["laser", "lightning", "mortar"][Math.floor(bigR4() * 3)], pos: floorLevel(3) })
  const energyCubes: { id: number; pos: V3 }[] = []
  let cubeId = 5000
  const spillCubes = (point6: V3, count9: number) => {
    for (let key17 = 0; key17 < count9; key17++) energyCubes.push({ id: cubeId++, pos: [point6[0] + normalNoise(bigR4), point6[1] + normalNoise(bigR4), point6[2]] })
  }
  let projectiles2: Shot[] = []
  const dropItems: { id: number; pos: V3; landT: number; holder: number | null; progress: number; taken: boolean }[] = []
  const locTimes3: number[] = []
  let isLate = 0
  const stateNow = { lzShots: 0, lzHits: 0, ltShots: 0, ltHits: 0, byHunter: 0, byOther: 0, byNpc: 0, shots: 0, hits: 0, dealt: 0, taken: 0, zoneDmg: 0, kills: 0, npcKills: 0, crates: 0, cubes: 0, drops: 0, stuck: 0 }
  const ticksInState = new Map<string, number>()
  let squadSighted = { t: -99, pos: [0, 0, 0] as V3 }
  let frameNo = 0
  let prevPosition: V3 = [...mechList[0].pos] as V3
  let lastMoveTime = 0
  const survivors = () => mechList.filter((mechRef8) => mechRef8.alive)
  const victims = (): Ent[] => [...mechList.filter((mechRef9) => mechRef9.alive), ...houseBots.filter((count10) => count10.alive), ...lootBoxes.filter((cell5) => cell5.alive)]
  const damageScale = (mechRef10: Mech) => 1 + CONST_C.cubes.damage_per_cube * Math.min(mechRef10.cubes, CONST_C.cubes.stat_cap)

  const hurtAmount = (quarry: Ent, quantity: number, attacker: Mech | Npc | null, armament: string, origin: V3 | null, timeVal3: number) => {
    if (!quarry.alive) return
    let behind = false
    if (quarry.kind === "mech") {
      const timeMark = quarry as Mech
      if (origin) {
        const rhs10 = Math.atan2(origin[1] - timeMark.pos[1], origin[0] - timeMark.pos[0])
        if (Math.abs(angDiff(rhs10, timeMark.aim[0])) > (CONST_C.mech.front_arc_deg / 2) * DEG_TO_RAD) (quantity *= CONST_C.mech.rear_mult), (behind = true)
      }
      if (armament !== "zone") quantity *= 1 - timeMark.armor
      timeMark.lastCombatT = timeVal3
      if (attacker?.id === 0) timeMark.hitByUsT = timeVal3
      const heading2 = origin ? Math.atan2(origin[1] - timeMark.pos[1], origin[0] - timeMark.pos[0]) : null
      timeMark.events.push({ type: "damaged", amount: quantity, weapon: armament, bearing: heading2, by: attacker ? attacker.id : null, ...(behind && { rear: behind }) })
      if (CHATTY && timeMark.id === 0 && armament !== "zone") console.log(`  ${timeVal3.toFixed(1)} hit by ${attacker?.id} (${(attacker as any)?.hunter ? "hunter" : "other"} skill ${(attacker as any)?.skill}) ${armament} ${quantity.toFixed(0)}${behind ? " REAR" : ""} d=${attacker ? Math.hypot(attacker.pos[0] - timeMark.pos[0], attacker.pos[1] - timeMark.pos[1]).toFixed(0) : "-"} hp=${timeMark.hp.toFixed(0)}`)
      if (timeMark.id === 0) (armament === "zone" ? (stateNow.zoneDmg += quantity) : (stateNow.taken += quantity))
      if (timeMark.id === 0 && attacker) {
        if (attacker.kind === "npc") stateNow.byNpc += quantity
        else if ((attacker as Mech).hunter) stateNow.byHunter += quantity
        else stateNow.byOther += quantity
      }
    }
    if (attacker && attacker.kind === "mech") {
      ;(attacker as Mech).lastCombatT = timeVal3
      if ((attacker as Mech).brain) (attacker as Mech).events.push({ type: "hit", target: quarry.id, amount: quantity, weapon: armament })
      if (attacker.id === 0) stateNow.dealt += quantity
    }
    if (quarry.kind === "npc" && attacker) (quarry as Npc).target = attacker.id
    quarry.hp -= quantity
    if (quarry.hp <= 0) {
      quarry.alive = false
      if (quarry.kind === "mech") {
        const timeMark2 = quarry as Mech
        timeMark2.deathTick = frameNo
        spillCubes(timeMark2.pos, timeMark2.cubes + CONST_C.cubes.death_drop_extra)
        for (const weight2 of timeMark2.weapons) if (weight2 && weight2.kind !== "gun") collectibles.push({ id: playerId++, kind: weight2.kind, pos: [...timeMark2.pos] as V3 })
        if (attacker && attacker.kind === "mech") {
          ;(attacker as Mech).events.push({ type: "kill", target: timeMark2.id })
          if (attacker.id === 0) stateNow.kills++
        }
      } else if (quarry.kind === "npc") {
        spillCubes(quarry.pos, CONST_C.npc.cubes)
        if (attacker?.id === 0) stateNow.npcKills++
      } else {
        spillCubes(quarry.pos, CONST_C.crates.cubes)
        if (bigR4() < CONST_C.loot.crate_weapon_chance) collectibles.push({ id: playerId++, kind: ["laser", "lightning", "mortar"][Math.floor(bigR4() * 3)], pos: [...quarry.pos] as V3 })
        if (attacker?.id === 0) stateNow.crates++
      }
    }
  }
  const hasSight = (lhs14: Mech, rhs11: Ent) => {
    if (rhs11.id === 0 && lhs14.id !== 0 && frameNo * STEP_DT < INVISIBLE_SECS) return false
    const gap5 = Math.hypot(rhs11.pos[0] - lhs14.pos[0], rhs11.pos[1] - lhs14.pos[1])
    if (gap5 > CONST_C.vision.range) return false
    if (gap5 > CONST_C.vision.near_range && Math.abs(angDiff(Math.atan2(rhs11.pos[1] - lhs14.pos[1], rhs11.pos[0] - lhs14.pos[0]), lhs14.aim[0])) > (CONST_C.vision.fov_deg / 2) * DEG_TO_RAD) return false
    const enemyZ2 = lhs14.pos[2] + 8
    if (TREE_CANOPY) return WORLD_MAP.sees(lhs14.pos[0], lhs14.pos[1], enemyZ2, rhs11.pos[0], rhs11.pos[1], rhs11.pos[2] + rhs11.height / 2, CONST_C.vision.near_range) || WORLD_MAP.sees(lhs14.pos[0], lhs14.pos[1], enemyZ2, rhs11.pos[0], rhs11.pos[1], rhs11.pos[2] + rhs11.height, CONST_C.vision.near_range)
    return WORLD_MAP.los(lhs14.pos[0], lhs14.pos[1], enemyZ2, rhs11.pos[0], rhs11.pos[1], rhs11.pos[2] + rhs11.height / 2) || WORLD_MAP.los(lhs14.pos[0], lhs14.pos[1], enemyZ2, rhs11.pos[0], rhs11.pos[1], rhs11.pos[2] + rhs11.height)
  }
  const noise = (source: V3, category2: string, excluded: number) => {
    const reach2 = CONST_C.sound.range[category2] ?? 100
    for (const other6 of mechList) {
      if (other6.id === excluded || !other6.alive) continue
      const gap6 = Math.hypot(other6.pos[0] - source[0], other6.pos[1] - source[1])
      if (gap6 > reach2) continue
      other6.sounds.push({ kind: category2, bearing: Math.atan2(source[1] - other6.pos[1], source[0] - other6.pos[0]) + normalNoise(bigR4) * 7 * DEG_TO_RAD, dist: gap6 < 60 ? "near" : gap6 < 160 ? "mid" : "far", age: 0 })
    }
  }
  const shootLaser = (mechRef11: Mech, weight3: W, muzzle: V3, directory3: V3, timeVal4: number) => {
    const fraction = Math.min(1, weight3.charge! / weight3.stats.charge)
    const harm = weight3.stats.damage * (weight3.stats.min_frac + (1 - weight3.stats.min_frac) * fraction) * damageScale(mechRef11)
    weight3.charge = 0
    weight3.cooldown = weight3.stats.cooldown
    noise(mechRef11.pos, "laser", mechRef11.id)
    let champion: Ent | null = null, beamTime = Infinity
    for (const other7 of victims()) {
      if (other7 === mechRef11) continue
      const tickTotal2 = rayCylinder(muzzle, directory3, other7.pos, other7.radius, other7.height)
      if (tickTotal2 !== null && tickTotal2 < beamTime && tickTotal2 < weight3.stats.range) (beamTime = tickTotal2), (champion = other7)
    }
    const succeeded2 = champion && WORLD_MAP.los(muzzle[0], muzzle[1], muzzle[2], muzzle[0] + directory3[0] * beamTime, muzzle[1] + directory3[1] * beamTime, muzzle[2] + directory3[2] * beamTime)
    if (mechRef11.id === 0) (stateNow.lzShots++, succeeded2 && champion!.kind === "mech" && stateNow.lzHits++)
    if (succeeded2) hurtAmount(champion!, harm, mechRef11, "laser", mechRef11.pos, timeVal4)
  }

  // ---- main loop ----
  while (frameNo < CONST_C.match.duration_s * CONST_C.match.tick_hz) {
    const timeVal5 = frameNo * STEP_DT
    const alive2 = survivors()
    const selfMech = mechList[0]
    if (alive2.length <= 1 || !selfMech.alive) break
    const bigZ = zoneAtTime(stages2, timeVal5)
    const botSide = alive2.filter((mechRef12) => mechRef12.id !== 0)
    const huntCount = LANCE_MODE && !SCHEDULED ? 99 : LANCE_MODE ? 1 + stateNow.kills + (timeVal5 > 120 ? 2 : 0) + (timeVal5 > 240 ? 99 : 0) : timeVal5 > 300 ? 99 : 1 + (15 - botSide.length)
    botSide.sort((lhs15, rhs12) => lhs15.skill - rhs12.skill || lhs15.id - rhs12.id)
    botSide.forEach((mechRef13, key18) => (mechRef13.hunter = key18 < huntCount))
    if (frameNo % CONST_C.match.decide_every === 0) {
      for (const mechRef14 of alive2) for (const other8 of alive2) if (other8 !== mechRef14 && hasSight(mechRef14, other8)) mechRef14.seen.set(other8.id, timeVal5)
      for (const heightVal2 of botSide) if (heightVal2.seen.get(0) === timeVal5) squadSighted = { t: timeVal5, pos: [...selfMech.pos] as V3 }
      for (const mechRef15 of alive2) {
        if (mechRef15.brain) {
          const observation3 = makeObs(mechRef15, timeVal5, bigZ, alive2, houseBots, lootBoxes, collectibles, energyCubes, projectiles2, dropItems, frameNo)
          const startTime5 = performance.now()
          ;(globalThis as any).__ECODBG = process.env.SIMECODBG === "1" && mechRef15.id === 0
          const lhs16 = mechRef15.brain.act(observation3)
          ;(globalThis as any).__ECODBG = false
          const millis2 = performance.now() - startTime5
          if (mechRef15.id === 0) {
            locTimes3.push(millis2)
            if (millis2 > 50) isLate++
            ticksInState.set(mechRef15.brain.state, (ticksInState.get(mechRef15.brain.state) ?? 0) + 1)
          }
          if (CHATTY && mechRef15.id === 0 && frameNo % 40 === 0) console.log(timeVal5.toFixed(0), mechRef15.brain.state, "hp", mechRef15.hp.toFixed(0), "pos", mechRef15.pos.map((vec9) => vec9.toFixed(0)).join(","), "w", mechRef15.weapons.map((weight4) => weight4?.kind ?? "-").join("+"), "cubes", mechRef15.cubes, "alive", alive2.length)
          draw.clear()
          mechRef15.move = (lhs16.move as V2) ?? [0, 0]
          mechRef15.boost = !!lhs16.boost
          mechRef15.jump = !!lhs16.jump
          if (lhs16.aim) mechRef15.cmdAim = lhs16.aim as V2
          mechRef15.fire = (lhs16.fire as [boolean, boolean]) ?? [false, false]
          if (lhs16.pick !== undefined && mechRef15.offer) installUpgrade(mechRef15, mechRef15.offer.choices[lhs16.pick]?.id)
          if (lhs16.take !== undefined) grab(mechRef15, lhs16.take, lhs16.drop)
        } else if (FREE_FOR_ALL) {
          // nearest mech it currently sees (anyone), else roam to the zone
          let aimTarget: Mech | null = null, bestDist = Infinity
          for (const other9 of alive2) {
            if (other9 === mechRef15 || mechRef15.seen.get(other9.id) !== timeVal5) continue
            const gap7 = Math.hypot(other9.pos[0] - mechRef15.pos[0], other9.pos[1] - mechRef15.pos[1])
            if (gap7 < bestDist) (bestDist = gap7), (aimTarget = other9)
          }
          mechRef15.hunter = true
          if (aimTarget) botAct(mechRef15, timeVal5, bigZ, aimTarget, { t: timeVal5, pos: aimTarget.pos }, bigR4)
          else botAct(mechRef15, timeVal5, bigZ, mechRef15, { t: -99, pos: mechRef15.pos }, bigR4)
        } else botAct(mechRef15, timeVal5, bigZ, selfMech, OMNISCIENT || (mechRef15.hunter && (!REALISTIC || REALISTIC_OMNI)) ? { t: timeVal5, pos: selfMech.pos } : squadSighted, bigR4)
      }
      for (const count11 of houseBots) if (count11.alive) houseAct2(count11, timeVal5)
    }
    // physics + weapons
    for (const mechRef16 of alive2) {
      const yawDelta = angDiff(mechRef16.cmdAim[0], mechRef16.aim[0])
      const myPos = mechRef16.stats.turn_rate * STEP_DT
      mechRef16.aim[0] = angDiff(mechRef16.aim[0] + Math.max(-myPos, Math.min(myPos, yawDelta)), 0)
      const posDelta = Math.max(CONST_C.mech.min_pitch, Math.min(CONST_C.mech.max_pitch, mechRef16.cmdAim[1])) - mechRef16.aim[1]
      mechRef16.aim[1] += Math.max(-CONST_C.mech.pitch_rate * STEP_DT, Math.min(CONST_C.mech.pitch_rate * STEP_DT, posDelta))
      const windingUp = mechRef16.weapons.some((weight5) => weight5?.kind === "laser" && (weight5.charge ?? 0) > 0)
      let moveLen = Math.hypot(mechRef16.move[0], mechRef16.move[1])
      const moveVec: V2 = moveLen > 1 ? [mechRef16.move[0] / moveLen, mechRef16.move[1] / moveLen] : [mechRef16.move[0], mechRef16.move[1]]
      moveLen = Math.min(1, moveLen)
      let pace = CONST_C.mech.walk_speed
      let expend = false
      if (mechRef16.boost && mechRef16.grounded && moveLen > 0.1 && mechRef16.energy > 0) (pace = CONST_C.mech.boost_speed), (mechRef16.energy -= CONST_C.mech.boost_drain * STEP_DT), (expend = true)
      if (windingUp) pace *= CONST_C.mech.charge_move_mult
      const accumulator2 = (mechRef16.grounded ? CONST_C.mech.ground_accel : CONST_C.mech.air_accel) * STEP_DT
      let velDX = moveVec[0] * pace - mechRef16.vel[0], velDY = moveVec[1] * pace - mechRef16.vel[1]
      const lenDelta = Math.hypot(velDX, velDY)
      if (lenDelta > accumulator2) (velDX *= accumulator2 / lenDelta), (velDY *= accumulator2 / lenDelta)
      mechRef16.vel[0] += velDX
      mechRef16.vel[1] += velDY
      if (mechRef16.jump && mechRef16.grounded) (mechRef16.vel[2] = CONST_C.mech.jump_speed), (mechRef16.grounded = false)
      if (mechRef16.boost && !mechRef16.grounded && mechRef16.energy > 0) {
        mechRef16.vel[2] = Math.min(CONST_C.mech.max_rise_speed, mechRef16.vel[2] + CONST_C.mech.jet_accel * STEP_DT)
        mechRef16.energy -= CONST_C.mech.jet_drain * STEP_DT
        expend = true
      }
      if (expend) mechRef16.spendT = timeVal5
      else if (timeVal5 - mechRef16.spendT > CONST_C.mech.energy_regen_delay) mechRef16.energy = Math.min(CONST_C.mech.energy, mechRef16.energy + CONST_C.mech.energy_regen * STEP_DT)
      mechRef16.energy = Math.max(0, mechRef16.energy)
      mechRef16.vel[2] -= CONST_C.mech.gravity * STEP_DT
      let nextX = mechRef16.pos[0] + mechRef16.vel[0] * STEP_DT, nextY = mechRef16.pos[1] + mechRef16.vel[1] * STEP_DT
      if (obstructed(nextX, nextY, mechRef16.pos[2])) {
        if (!obstructed(nextX, mechRef16.pos[1], mechRef16.pos[2])) (nextY = mechRef16.pos[1]), (mechRef16.vel[1] = 0)
        else if (!obstructed(mechRef16.pos[0], nextY, mechRef16.pos[2])) (nextX = mechRef16.pos[0]), (mechRef16.vel[0] = 0)
        else (nextX = mechRef16.pos[0]), (nextY = mechRef16.pos[1]), (mechRef16.vel[0] = 0), (mechRef16.vel[1] = 0)
      }
      // mechs push apart
      for (const other10 of alive2) {
        if (other10 === mechRef16) continue
        const deltaX = nextX - other10.pos[0], deltaY = nextY - other10.pos[1], gap8 = Math.hypot(deltaX, deltaY)
        if (gap8 < mechRef16.radius * 2 && gap8 > 0.01 && !obstructed(nextX + (deltaX / gap8) * 0.3, nextY + (deltaY / gap8) * 0.3, mechRef16.pos[2])) (nextX += (deltaX / gap8) * 0.3), (nextY += (deltaY / gap8) * 0.3)
      }
      let nextZ = mechRef16.pos[2] + mechRef16.vel[2] * STEP_DT
      const groundY = WORLD_MAP.surfAt(nextX, nextY)
      if (nextZ <= groundY) (nextZ = groundY), (mechRef16.vel[2] = 0), (mechRef16.grounded = true)
      else mechRef16.grounded = nextZ - groundY < 0.05
      mechRef16.pos = [nextX, nextY, nextZ]
      for (let step3 = 0; step3 < 2; step3++) {
        const weight6 = mechRef16.weapons[step3]
        if (!weight6) continue
        weight6.cooldown = Math.max(0, weight6.cooldown - STEP_DT)
        const trigger = mechRef16.fire[step3]
        const muzzle2: V3 = [mechRef16.pos[0], mechRef16.pos[1], mechRef16.pos[2] + 7]
        const directory4: V3 = [Math.cos(mechRef16.aim[1]) * Math.cos(mechRef16.aim[0]), Math.cos(mechRef16.aim[1]) * Math.sin(mechRef16.aim[0]), Math.sin(mechRef16.aim[1])]
        if (weight6.kind === "gun") {
          if ((weight6.reload ?? 0) > 0) {
            weight6.reload! -= STEP_DT
            if (weight6.reload! <= 0) (weight6.reload = 0), (weight6.ammo = weight6.stats.mag)
          } else if (trigger && weight6.cooldown <= 0 && (weight6.ammo ?? 0) > 0) {
            const velocity = weight6.stats.spread_deg * DEG_TO_RAD
            const yawA = mechRef16.aim[0] + normalNoise(bigR4) * velocity, pointA = mechRef16.aim[1] + normalNoise(bigR4) * velocity
            const gap9: V3 = [Math.cos(pointA) * Math.cos(yawA), Math.cos(pointA) * Math.sin(yawA), Math.sin(pointA)]
            projectiles2.push({ by: mechRef16.id, pos: [muzzle2[0] + gap9[0] * 3, muzzle2[1] + gap9[1] * 3, muzzle2[2] + gap9[2] * 3], vel: [gap9[0] * weight6.stats.speed, gap9[1] * weight6.stats.speed, gap9[2] * weight6.stats.speed], ttl: weight6.stats.range / weight6.stats.speed, dmg: weight6.stats.damage * damageScale(mechRef16), kind: "gun" })
            weight6.cooldown = weight6.stats.cooldown
            weight6.ammo!--
            if (mechRef16.id === 0) stateNow.shots++
            noise(mechRef16.pos, "gun", mechRef16.id)
            if (weight6.ammo === 0) weight6.reload = weight6.stats.reload
          }
        } else if (weight6.kind === "laser") {
          if (weight6.cooldown > 0) weight6.charge = 0
          else if (trigger) {
            weight6.charge = (weight6.charge ?? 0) + STEP_DT
            if (weight6.charge >= weight6.stats.charge + weight6.stats.hold_s) shootLaser(mechRef16, weight6, muzzle2, directory4, timeVal5)
          } else if ((weight6.charge ?? 0) >= weight6.stats.min_charge) shootLaser(mechRef16, weight6, muzzle2, directory4, timeVal5)
          else if ((weight6.charge ?? 0) > 0) weight6.charge! += STEP_DT
        } else if (weight6.kind === "lightning") {
          if (trigger && weight6.cooldown <= 0 && mechRef16.energy >= weight6.stats.energy) {
            mechRef16.energy -= weight6.stats.energy
            mechRef16.spendT = timeVal5
            weight6.cooldown = weight6.stats.cooldown
            let champion2: Ent | null = null, bestDist2 = Infinity
            for (const other11 of victims()) {
              if (other11 === mechRef16) continue
              const gap10 = Math.hypot(other11.pos[0] - mechRef16.pos[0], other11.pos[1] - mechRef16.pos[1])
              if (gap10 < weight6.stats.range && gap10 < bestDist2 && Math.abs(angDiff(Math.atan2(other11.pos[1] - mechRef16.pos[1], other11.pos[0] - mechRef16.pos[0]), mechRef16.aim[0])) < (weight6.stats.cone_deg / 2) * DEG_TO_RAD && WORLD_MAP.los(muzzle2[0], muzzle2[1], muzzle2[2], other11.pos[0], other11.pos[1], other11.pos[2] + other11.height / 2)) (bestDist2 = gap10), (champion2 = other11)
            }
            if (mechRef16.id === 0) (stateNow.ltShots++, champion2 && champion2.kind === "mech" && stateNow.ltHits++)
            if (champion2) hurtAmount(champion2, weight6.stats.damage * damageScale(mechRef16), mechRef16, "lightning", mechRef16.pos, timeVal5)
            noise(mechRef16.pos, "lightning", mechRef16.id)
          }
        }
      }
    }
    // projectiles
    const following: Shot[] = []
    for (const step4 of projectiles2) {
      const nextPos: V3 = [step4.pos[0] + step4.vel[0] * STEP_DT, step4.pos[1] + step4.vel[1] * STEP_DT, step4.pos[2] + step4.vel[2] * STEP_DT]
      let struck = false
      for (const other12 of victims()) {
        if (other12.id === step4.by) continue
        if (segmentCylinder(step4.pos, nextPos, other12.pos, other12.radius, other12.height)) {
          const attacker2 = step4.by < 100 ? mechList[step4.by] : houseBots.find((count12) => count12.id === step4.by) ?? null
          hurtAmount(other12, step4.dmg, attacker2, step4.kind, step4.pos, timeVal5)
          if (step4.by === 0 && other12.kind === "mech") stateNow.hits++
          struck = true
          break
        }
      }
      if (struck || !WORLD_MAP.los(step4.pos[0], step4.pos[1], step4.pos[2], nextPos[0], nextPos[1], nextPos[2])) continue
      step4.pos = nextPos
      step4.ttl -= STEP_DT
      if (step4.ttl > 0) following.push(step4)
    }
    projectiles2 = following
    // cubes
    for (let key19 = energyCubes.length - 1; key19 >= 0; key19--) {
      const cell6 = energyCubes[key19]
      let champion3: Mech | null = null, bestDist3 = CONST_C.cubes.pickup_radius
      for (const mechRef17 of alive2) {
        const gap11 = Math.hypot(mechRef17.pos[0] - cell6.pos[0], mechRef17.pos[1] - cell6.pos[1])
        if (gap11 <= bestDist3 && Math.abs(mechRef17.pos[2] - cell6.pos[2]) < 4) (bestDist3 = gap11), (champion3 = mechRef17)
      }
      if (champion3) {
        energyCubes.splice(key19, 1)
        earnCubes(champion3, 1)
        champion3.events.push({ type: "pickup", cubes: champion3.cubes })
        if (champion3.id === 0) stateNow.cubes++
      }
    }
    // supply drops
    for (const timeStep of CONST_C.drops?.times ?? []) {
      if (Math.abs(timeVal5 - timeStep) < STEP_DT / 2) {
        for (let key20 = 0; key20 < 50; key20++) {
          const lhs17 = bigR4() * Math.PI * 2, gap12 = Math.sqrt(bigR4()) * bigZ.next.r * CONST_C.drops.spread
          const posX6 = bigZ.next.c[0] + Math.cos(lhs17) * gap12, posY7 = bigZ.next.c[1] + Math.sin(lhs17) * gap12
          const count13 = WORLD_MAP.nodeOf(posX6, posY7)
          if (count13 >= 0 && WORLD_MAP.nClear[count13] > 6) {
            dropItems.push({ id: 20000 + dropItems.length, pos: [posX6, posY7, WORLD_MAP.surfAt(posX6, posY7)], landT: timeVal5 + CONST_C.drops.fall_s, holder: null, progress: 0, taken: false })
            break
          }
        }
      }
    }
    for (const gap13 of dropItems) {
      if (gap13.taken || timeVal5 < gap13.landT) continue
      const insideRing = survivors().filter((mechRef18) => Math.hypot(mechRef18.pos[0] - gap13.pos[0], mechRef18.pos[1] - gap13.pos[1]) <= CONST_C.drops.radius)
      if (insideRing.length === 1) {
        if (gap13.holder !== insideRing[0].id) (gap13.holder = insideRing[0].id), (gap13.progress = 0)
        gap13.progress += STEP_DT
        if (gap13.progress >= CONST_C.drops.capture_s) {
          const mechRef19 = insideRing[0]
          gap13.taken = true
          earnCubes(mechRef19, CONST_C.drops.cubes)
          mechRef19.hp = mechRef19.maxHp
          mechRef19.energy = CONST_C.mech.energy
          mechRef19.events.push({ type: "drop", id: gap13.id, cubes: mechRef19.cubes })
          if (mechRef19.id === 0) stateNow.drops++
        }
      } else if (insideRing.length === 0) (gap13.holder = null), (gap13.progress = 0)
    }
    // zone + regen
    for (const mechRef20 of survivors()) {
      if (Math.hypot(mechRef20.pos[0] - bigZ.c[0], mechRef20.pos[1] - bigZ.c[1]) > bigZ.r) hurtAmount(mechRef20, bigZ.dps * STEP_DT, null, "zone", null, timeVal5)
      else if (timeVal5 - mechRef20.lastCombatT > mechRef20.stats.regen_delay) mechRef20.hp = Math.min(mechRef20.maxHp, mechRef20.hp + mechRef20.stats.regen_per_s * STEP_DT)
    }
    if (Math.hypot(selfMech.pos[0] - prevPosition[0], selfMech.pos[1] - prevPosition[1]) > 1) (prevPosition = [...selfMech.pos] as V3), (lastMoveTime = timeVal5)
    else if (Math.hypot(selfMech.move[0], selfMech.move[1]) > 0.5 && timeVal5 - lastMoveTime > 2) stateNow.stuck++
    frameNo++
  }
  const selfMech2 = mechList[0]
  if (process.env.SIMDBG === "1")
    for (const mechRef21 of mechList) console.log(`  #${mechRef21.id} ${mechRef21.brain ? (mechRef21.brain as any).forceCore || "us" : "house"} alive=${mechRef21.alive} death=${mechRef21.alive ? "-" : (mechRef21.deathTick * STEP_DT).toFixed(0)} hp=${mechRef21.hp.toFixed(0)} w=${mechRef21.weapons.map((weight7) => weight7?.kind ?? "-").join("+")} cubes=${mechRef21.cubes} upg=${mechRef21.upgrades.join(",")} state=${mechRef21.brain?.state ?? ""}`)
  const diedEarlier = mechList.filter((mechRef22) => mechRef22.id !== 0 && !mechRef22.alive && mechRef22.deathTick < selfMech2.deathTick).length
  const standing = selfMech2.alive ? 1 + mechList.filter((mechRef23) => mechRef23.id !== 0 && mechRef23.alive && mechRef23.hp > selfMech2.hp).length : 16 - diedEarlier
  locTimes3.sort((lhs18, rhs13) => lhs18 - rhs13)
  const bestStates = [...ticksInState.entries()].sort((lhs19, rhs14) => rhs14[1] - lhs19[1]).slice(0, 6).map(([key21, vec10]) => `${key21}:${vec10}`).join(" ")
  return {
    seed: rngSeed, place: standing, score: 16 - standing, t: Math.round(frameNo * STEP_DT), alive: selfMech2.alive, ...stateNow, dealt: Math.round(stateNow.dealt), taken: Math.round(stateNow.taken), byHunter: Math.round(stateNow.byHunter), byOther: Math.round(stateNow.byOther), byNpc: Math.round(stateNow.byNpc), zoneDmg: Math.round(stateNow.zoneDmg),
    p99: locTimes3[Math.floor(locTimes3.length * 0.99)]?.toFixed(1), max: locTimes3[locTimes3.length - 1]?.toFixed(1), late: isLate, wall: ((performance.now() - wallStart) / 1000).toFixed(1),
    weapons: selfMech2.weapons.map((weight8) => weight8?.kind ?? "-").join("+"), cubesNow: selfMech2.cubes, mode: (selfMech2.brain as any)?.w?.mode, upg: selfMech2.upgrades.join(","), states: bestStates,
  }

  function earnCubes(mechRef24: Mech, count14: number) {
    for (let key22 = 0; key22 < count14; key22++) {
      mechRef24.cubes++
      if (mechRef24.cubes <= CONST_C.cubes.stat_cap) {
        const append = CONST_C.mech.hp * CONST_C.cubes.hp_per_cube
        mechRef24.maxHp += append
        mechRef24.hp += append
      }
      if (CONST_C.cubes.upgrade_thresholds.includes(mechRef24.cubes)) mechRef24.offersPending++
    }
    if (mechRef24.brain && !mechRef24.offer && mechRef24.offersPending > 0) freshOffer(mechRef24)
  }
  function freshOffer(mechRef25: Mech) {
    const succeeded3 = CONST_C.upgrades.filter((unitVec: any) => (!unitVec.requires || mechRef25.weapons.some((weight9) => weight9?.kind === unitVec.requires)) && mechRef25.upgrades.filter((posX7) => posX7 === unitVec.id).length < unitVec.max)
    const options = []
    while (options.length < 3 && succeeded3.length) options.push(succeeded3.splice(Math.floor(bigR4() * succeeded3.length), 1)[0])
    mechRef25.offer = { id: frameNo, expires: frameNo * STEP_DT + 5, choices: options.map((unitVec2: any) => ({ id: unitVec2.id, name: unitVec2.name, desc: unitVec2.desc })) }
    mechRef25.offersPending--
  }
  function installUpgrade(mechRef26: Mech, ident: string | undefined) {
    if (!ident) return
    mechRef26.upgrades.push(ident)
    mechRef26.events.push({ type: "upgrade", id: ident })
    if (ident === "plating") (mechRef26.maxHp *= 1.15), (mechRef26.hp *= 1.15)
    if (ident === "armor") mechRef26.armor += 0.08
    if (ident === "nanites") (mechRef26.stats.regen_per_s *= 1.6), (mechRef26.stats.regen_delay -= 1.5)
    if (ident === "gyros") mechRef26.stats.turn_rate *= 1.3
    if (ident === "laser_damage") for (const weight10 of mechRef26.weapons) if (weight10?.kind === "laser") weight10.stats.damage *= 1.18
    if (ident === "laser_charge") for (const weight11 of mechRef26.weapons) if (weight11?.kind === "laser") weight11.stats.charge *= 0.75
    mechRef26.offer = null
    if (mechRef26.offersPending > 0) freshOffer(mechRef26)
  }
  function grab(mechRef27: Mech, ident2: number, release: number | undefined) {
    const point7 = collectibles.find((point8) => point8.id === ident2)
    if (!point7 || Math.hypot(point7.pos[0] - mechRef27.pos[0], point7.pos[1] - mechRef27.pos[1]) > CONST_C.loot.pickup_radius) return
    let bay = mechRef27.weapons.findIndex((weight12) => !weight12)
    if (bay < 0 && release !== undefined) bay = release
    if (bay < 0 || mechRef27.weapons.some((weight13) => weight13?.kind === point7.kind)) return
    const former = mechRef27.weapons[bay]
    const weight14 = buildWeapon(point7.kind)
    weight14.cooldown = Math.max(CONST_C.loot.equip_s, weight14.stats.cooldown ?? 0)
    mechRef27.weapons[bay] = weight14
    collectibles.splice(collectibles.indexOf(point7), 1)
    mechRef27.events.push({ type: "took", id: point7.id, kind: point7.kind, slot: bay })
    if (former) {
      const newId = playerId++
      collectibles.push({ id: newId, kind: former.kind, pos: [...mechRef27.pos] as V3 })
      mechRef27.events.push({ type: "dropped", id: newId, kind: former.kind, slot: bay })
    }
  }
  function houseAct2(count15: Npc, timeVal6: number) {
    const homeSpot = count15.home
    const homeDist = Math.hypot(count15.pos[0] - homeSpot[0], count15.pos[1] - homeSpot[1])
    let aimTarget2 = count15.target >= 0 && count15.target < 100 ? mechList[count15.target] : null
    if (aimTarget2 && (!aimTarget2.alive || Math.hypot(aimTarget2.pos[0] - count15.pos[0], aimTarget2.pos[1] - count15.pos[1]) > CONST_C.npc.deaggro_range || homeDist > CONST_C.npc.chase_leash)) (aimTarget2 = null), (count15.target = -1)
    if (!aimTarget2 && homeDist < CONST_C.npc.leash)
      for (const mechRef28 of survivors()) {
        const gap14 = Math.hypot(mechRef28.pos[0] - count15.pos[0], mechRef28.pos[1] - count15.pos[1])
        if (gap14 < CONST_C.npc.aggro_range && WORLD_MAP.los(count15.pos[0], count15.pos[1], count15.pos[2] + 3, mechRef28.pos[0], mechRef28.pos[1], mechRef28.pos[2] + 4.5)) (aimTarget2 = mechRef28), (count15.target = mechRef28.id)
      }
    let destination = count15.goal
    if (aimTarget2) {
      destination = [aimTarget2.pos[0], aimTarget2.pos[1]]
      count15.cd -= STEP_DT * 2
      if (count15.cd <= 0 && WORLD_MAP.los(count15.pos[0], count15.pos[1], count15.pos[2] + 3, aimTarget2.pos[0], aimTarget2.pos[1], aimTarget2.pos[2] + 4.5)) {
        count15.cd = CONST_C.npc.cooldown
        const gap15 = Math.hypot(aimTarget2.pos[0] - count15.pos[0], aimTarget2.pos[1] - count15.pos[1], aimTarget2.pos[2] + 4.5 - count15.pos[2] - 3)
        const velocity2 = CONST_C.npc.spread_deg * DEG_TO_RAD
        const yawA2 = Math.atan2(aimTarget2.pos[1] - count15.pos[1], aimTarget2.pos[0] - count15.pos[0]) + normalNoise(bigR4) * velocity2
        const pointA2 = Math.asin((aimTarget2.pos[2] + 4.5 - count15.pos[2] - 3) / gap15) + normalNoise(bigR4) * velocity2
        const vec11 = CONST_C.npc.projectile_speed
        projectiles2.push({ by: count15.id, pos: [count15.pos[0], count15.pos[1], count15.pos[2] + 3], vel: [Math.cos(pointA2) * Math.cos(yawA2) * vec11, Math.cos(pointA2) * Math.sin(yawA2) * vec11, Math.sin(pointA2) * vec11], ttl: 2, dmg: CONST_C.npc.damage, kind: "npc" })
        noise(count15.pos, "npc_gun", -1)
      }
    } else if (Math.hypot(destination[0] - count15.pos[0], destination[1] - count15.pos[1]) < 2 || homeDist > CONST_C.npc.leash) {
      const lhs20 = bigR4() * Math.PI * 2, radiusVal = bigR4() * CONST_C.npc.leash * 0.8
      count15.goal = [homeSpot[0] + Math.cos(lhs20) * radiusVal, homeSpot[1] + Math.sin(lhs20) * radiusVal]
      destination = count15.goal
    }
    const gap16 = Math.hypot(destination[0] - count15.pos[0], destination[1] - count15.pos[1])
    if (gap16 > 6) {
      const nextX2 = count15.pos[0] + ((destination[0] - count15.pos[0]) / gap16) * CONST_C.npc.speed * STEP_DT * 2, nextY2 = count15.pos[1] + ((destination[1] - count15.pos[1]) / gap16) * CONST_C.npc.speed * STEP_DT * 2
      if (WORLD_MAP.surfAt(nextX2, nextY2) < count15.pos[2] + 1.2) count15.pos = [nextX2, nextY2, WORLD_MAP.surfAt(nextX2, nextY2)]
    }
  }
}

function obstructed(posX8: number, posY8: number, posZ4: number) {
  for (let key23 = 0; key23 < 8; key23++) {
    const lhs21 = (key23 / 8) * Math.PI * 2
    const startX = posX8 + Math.cos(lhs21) * CONST_C.mech.radius * 0.9, startY = posY8 + Math.sin(lhs21) * CONST_C.mech.radius * 0.9
    if (WORLD_MAP.cellOf(startX, startY) < 0 || WORLD_MAP.surfAt(startX, startY) > posZ4 + CONST_C.mech.step_up) return true
  }
  return false
}

function botAct(mechRef29: Mech, timeVal7: number, bigZ2: ReturnType<typeof zoneAtTime>, ourSide: Mech, intelligence: { t: number; pos: V3 }, bigR5: () => number) {
  const gap17 = Math.hypot(ourSide.pos[0] - mechRef29.pos[0], ourSide.pos[1] - mechRef29.pos[1])
  const canSee = ourSide !== mechRef29 && mechRef29.seen.get(ourSide.id) === timeVal7
  if (LANCE_MODE && !FREE_FOR_ALL) return lancerAct(mechRef29, timeVal7, bigZ2, ourSide, gap17, canSee, bigR5)
  const aware = timeVal7 - intelligence.t < 6
  mechRef29.fire = [false, false]
  mechRef29.boost = false
  mechRef29.jump = false
  // hunters fight on sight; the rest of the squad only answers if we hit them or come very close
  const angered = timeVal7 - mechRef29.hitByUsT < 8
  const engageRange = mechRef29.hunter ? 180 : angered ? 120 : 0
  if (canSee && ourSide.alive && gap17 < engageRange) {
    if (timeVal7 > (mechRef29 as any).flipT) ((mechRef29 as any).flipT = timeVal7 + 0.4 + bigR5() * 1.2), ((mechRef29 as any).side = bigR5() < 0.5 ? -1 : 1)
    const step5 = (mechRef29 as any).side ?? 1
    const rhs15 = Math.atan2(ourSide.pos[1] - mechRef29.pos[1], ourSide.pos[0] - mechRef29.pos[0])
    const outward = mechRef29.hunter ? (gap17 > 150 ? 0.8 : gap17 > 100 ? 0.35 : gap17 < 40 ? -0.5 : 0) : 0
    mechRef29.move = [Math.cos(rhs15) * outward - Math.sin(rhs15) * step5 * 0.6, Math.sin(rhs15) * outward + Math.cos(rhs15) * step5 * 0.6]
    if (obstructed(mechRef29.pos[0] + mechRef29.move[0] * 4, mechRef29.pos[1] + mechRef29.move[1] * 4, mechRef29.pos[2])) mechRef29.move = [Math.cos(rhs15) * outward, Math.sin(rhs15) * outward]
    const weightB = mechRef29.weapons[1]
    const beam = weightB?.kind === "laser" ? weightB : null
    const failure = REALISTIC ? REALISTIC_JITTER * (1.6 - mechRef29.skill) : (1 - mechRef29.skill) * 0.03 + (1 - STRENGTH) * 0.05
    if (beam && beam.cooldown <= 0.05) {
      // hitscan: aim at the body, charge fully, release when on target
      mechRef29.cmdAim = [rhs15 + normalNoise(bigR5) * failure, Math.atan2(ourSide.pos[2] + 4.5 - mechRef29.pos[2] - 7, gap17)]
      const lockedOn = Math.abs(angDiff(rhs15, mechRef29.aim[0])) < 0.02 + failure
      // slower trigger discipline for weaker squads: wait a little longer before releasing
      const stay = REALISTIC ? beam.stats.charge : beam.stats.charge * (1 + Math.max(0, -STRENGTH) * 0.8)
      mechRef29.fire = [false, !((beam.charge ?? 0) >= Math.min(stay, beam.stats.charge + beam.stats.hold_s - 0.2) && lockedOn)]
    } else {
      const timeLeft2 = gap17 / 252 + 0.05
      const pointX2 = ourSide.pos[0] + ourSide.vel[0] * timeLeft2 * (0.4 + 0.6 * mechRef29.skill), pointY2 = ourSide.pos[1] + ourSide.vel[1] * timeLeft2 * (0.4 + 0.6 * mechRef29.skill)
      mechRef29.cmdAim = [Math.atan2(pointY2 - mechRef29.pos[1], pointX2 - mechRef29.pos[0]) + normalNoise(bigR5) * failure, Math.atan2(ourSide.pos[2] + 4.5 - mechRef29.pos[2] - 7, gap17)]
      const lockedOn2 = Math.abs(angDiff(mechRef29.cmdAim[0], mechRef29.aim[0])) < 0.05
      const fireProb = REALISTIC ? 0.9 : Math.min(1, Math.max(0.2, 0.8 + 0.4 * STRENGTH))
      mechRef29.fire = [lockedOn2 && bigR5() < fireProb, weightB?.kind === "lightning" && gap17 < 40 && lockedOn2]
    }
    return
  }
  let destination2: V2
  if (mechRef29.hunter && ourSide.alive && aware) destination2 = [intelligence.pos[0], intelligence.pos[1]]
  else if (mechRef29.hunter && ourSide.alive) destination2 = [ourSide.pos[0] + Math.cos(mechRef29.id) * 60, ourSide.pos[1] + Math.sin(mechRef29.id) * 60] // roam toward our area
  else destination2 = [bigZ2.next.c[0] + Math.cos(mechRef29.id * 2.4) * bigZ2.next.r * 0.4, bigZ2.next.c[1] + Math.sin(mechRef29.id * 2.4) * bigZ2.next.r * 0.4]
  const radiusVal2 = mechRef29.nav!.steer(mechRef29.pos[0], mechRef29.pos[1], destination2[0], destination2[1], timeVal7, 2500)
  mechRef29.move = radiusVal2.arrived ? [0, 0] : radiusVal2.dir
  mechRef29.jump = radiusVal2.jump
  mechRef29.boost = mechRef29.hunter && aware && gap17 > 120 && mechRef29.energy > 50
  const gaze = aware && mechRef29.hunter ? Math.atan2(intelligence.pos[1] - mechRef29.pos[1], intelligence.pos[0] - mechRef29.pos[0]) : Math.atan2(mechRef29.move[1], mechRef29.move[0]) + Math.sin(timeVal7 + mechRef29.id) * 1.2
  mechRef29.cmdAim = [gaze || mechRef29.aim[0], 0]
}

function makeObs(mechRef30: Mech, timeVal8: number, bigZ3: ReturnType<typeof zoneAtTime>, alive3: Mech[], houseBots2: Npc[], lootBoxes2: Ent[], collectibles2: { id: number; kind: string; pos: V3 }[], energyCubes2: { id: number; pos: V3 }[], projectiles3: Shot[], dropItems2: any[], frameNo2: number): Observation {
  const memory = CONST_C.vision.memory_s
  const visible = (point9: V3, heightVal3 = 0.8) => {
    const gap18 = Math.hypot(point9[0] - mechRef30.pos[0], point9[1] - mechRef30.pos[1])
    if (gap18 > CONST_C.vision.range) return false
    if (gap18 > CONST_C.vision.near_range && Math.abs(angDiff(Math.atan2(point9[1] - mechRef30.pos[1], point9[0] - mechRef30.pos[0]), mechRef30.aim[0])) > (CONST_C.vision.fov_deg / 2) * DEG_TO_RAD) return false
    if (TREE_CANOPY) return WORLD_MAP.sees(mechRef30.pos[0], mechRef30.pos[1], mechRef30.pos[2] + 8, point9[0], point9[1], point9[2] + heightVal3, CONST_C.vision.near_range)
    return WORLD_MAP.los(mechRef30.pos[0], mechRef30.pos[1], mechRef30.pos[2] + 8, point9[0], point9[1], point9[2] + heightVal3)
  }
  for (const count16 of houseBots2) if (count16.alive && visible(count16.pos, 2)) mechRef30.seen.set(count16.id, timeVal8)
  const mechViews: any[] = alive3
    .filter((other13) => other13 !== mechRef30 && timeVal8 - (mechRef30.seen.get(other13.id) ?? -99) <= memory)
    .map((other14) => ({ id: other14.id, kind: "mech", pos: [...other14.pos], vel: [...other14.vel], aim: [...other14.aim], hp: other14.hp, max_hp: other14.maxHp, cubes: other14.cubes, weapons: other14.weapons.filter(Boolean).map((weight15) => weight15!.kind), charging: other14.weapons.some((weight16) => weight16?.kind === "laser" && (weight16.charge ?? 0) > 0) }))
  for (const count17 of houseBots2) if (count17.alive && timeVal8 - (mechRef30.seen.get(count17.id) ?? -99) <= memory) mechViews.push({ id: count17.id, kind: "npc", pos: [...count17.pos], vel: [0, 0, 0], aim: [0, 0], hp: count17.hp, max_hp: count17.maxHp })
  return {
    type: "obs", tick: frameNo2, t: timeVal8, alive: alive3.length,
    self: {
      id: mechRef30.id, pos: [...mechRef30.pos] as V3, vel: [...mechRef30.vel] as V3, aim: [...mechRef30.aim] as V2, hp: mechRef30.hp, max_hp: mechRef30.maxHp, energy: mechRef30.energy, max_energy: CONST_C.mech.energy, grounded: mechRef30.grounded, cubes: mechRef30.cubes, upgrades: [...mechRef30.upgrades],
      weapons: mechRef30.weapons.map((weight17) => (weight17 ? { ...weight17, stats: { ...weight17.stats } } : null)) as any, in_zone: Math.hypot(mechRef30.pos[0] - bigZ3.c[0], mechRef30.pos[1] - bigZ3.c[1]) <= bigZ3.r, stats: { ...mechRef30.stats },
    },
    mechs: mechViews,
    projectiles: projectiles3.filter((step6) => step6.by !== mechRef30.id && visible(step6.pos, 0)).map((step7, key24) => ({ id: key24, kind: step7.kind as any, pos: step7.pos, vel: step7.vel })),
    crates: lootBoxes2.filter((cell7) => cell7.alive && visible(cell7.pos, 1.2)).map((cell8) => ({ id: cell8.id, pos: cell8.pos, hp: cell8.hp })),
    cubes: energyCubes2.filter((cell9) => visible(cell9.pos, 0.5)).map((cell10) => ({ id: cell10.id, pos: cell10.pos })),
    pickups: collectibles2.filter((point10) => visible(point10.pos)) as any,
    sounds: mechRef30.sounds.splice(0), beams: [], events: mechRef30.events.splice(0),
    zone: { center: bigZ3.prev.c, radius: bigZ3.prev.r, next_center: bigZ3.next.c, next_radius: bigZ3.next.r, shrink_start: bigZ3.next.t0, shrink_end: bigZ3.next.t1, dps: bigZ3.dps },
    offer: mechRef30.offer,
    drops: dropItems2.filter((gap19) => !gap19.taken).map((gap20) => ({ id: gap20.id, pos: gap20.pos, radius: CONST_C.drops.radius, lands_in: Math.max(0, gap20.landT - timeVal8), capture_s: CONST_C.drops.capture_s, progress: gap20.progress, holder: gap20.holder })),
  } as Observation
}

function segmentCylinder(lhs22: V3, rhs16: V3, point11: V3, radiusVal3: number, heightVal4: number) {
  for (let key25 = 0; key25 <= 8; key25++) {
    const entry3 = key25 / 8
    const posX9 = lhs22[0] + (rhs16[0] - lhs22[0]) * entry3, posY9 = lhs22[1] + (rhs16[1] - lhs22[1]) * entry3, posZ5 = lhs22[2] + (rhs16[2] - lhs22[2]) * entry3
    if (Math.hypot(posX9 - point11[0], posY9 - point11[1]) <= radiusVal3 + 0.3 && posZ5 >= point11[2] && posZ5 <= point11[2] + heightVal4) return true
  }
  return false
}
function rayCylinder(other15: V3, gap21: V3, point12: V3, radiusVal4: number, heightVal5: number): number | null {
  const originX = other15[0] - point12[0], originY = other15[1] - point12[1]
  const lhs23 = gap21[0] * gap21[0] + gap21[1] * gap21[1]
  if (lhs23 < 1e-9) return null
  const rhs17 = 2 * (originX * gap21[0] + originY * gap21[1]), cell11 = originX * originX + originY * originY - radiusVal4 * radiusVal4
  const discriminant = rhs17 * rhs17 - 4 * lhs23 * cell11
  if (discriminant < 0) return null
  const timeVal9 = (-rhs17 - Math.sqrt(discriminant)) / (2 * lhs23)
  if (timeVal9 < 0) return null
  const posZ6 = other15[2] + gap21[2] * timeVal9
  return posZ6 >= point12[2] && posZ6 <= point12[2] + heightVal5 ? timeVal9 : null
}

const outcomes: any[] = []
for (let key26 = 0; key26 < MATCH_TOTAL; key26++) {
  const radiusVal5 = playMatch(BASE_SEED + key26)
  console.log(JSON.stringify(radiusVal5))
  outcomes.push(radiusVal5)
}
const average = (entry4: (r: any) => number) => (outcomes.reduce((step8, radiusVal6) => step8 + entry4(radiusVal6), 0) / outcomes.length).toFixed(2)
console.log(`mean score ${average((radiusVal7) => radiusVal7.score)}  survive ${average((radiusVal8) => radiusVal8.t)}s  kills ${average((radiusVal9) => radiusVal9.kills)}  dealt ${average((radiusVal10) => radiusVal10.dealt)}  taken ${average((radiusVal11) => radiusVal11.taken)} (hunter ${average((radiusVal12) => radiusVal12.byHunter)} other ${average((radiusVal13) => radiusVal13.byOther)} npc ${average((radiusVal14) => radiusVal14.byNpc)})  cubes ${average((radiusVal15) => radiusVal15.cubes)}  late ${average((radiusVal16) => radiusVal16.late)}  acc gun ${(outcomes.reduce((lhs24, radiusVal17) => lhs24 + radiusVal17.hits, 0) / Math.max(1, outcomes.reduce((lhs25, radiusVal18) => lhs25 + radiusVal18.shots, 0))).toFixed(2)} laser ${(outcomes.reduce((lhs26, radiusVal19) => lhs26 + radiusVal19.lzHits, 0) / Math.max(1, outcomes.reduce((lhs27, radiusVal20) => lhs27 + radiusVal20.lzShots, 0))).toFixed(2)} (${average((radiusVal21) => radiusVal21.lzShots)}/m) lightning ${(outcomes.reduce((lhs28, radiusVal22) => lhs28 + radiusVal22.ltHits, 0) / Math.max(1, outcomes.reduce((lhs29, radiusVal23) => lhs29 + radiusVal23.ltShots, 0))).toFixed(2)}`)


function lancerAct(mechRef31: Mech, timeVal10: number, bigZ4: ReturnType<typeof zoneAtTime>, ourSide2: Mech, gap22: number, canSee2: boolean, bigR6: () => number) {
  const level2 = (mechRef31 as any).tier as number
  mechRef31.fire = [false, false]
  mechRef31.boost = false
  mechRef31.jump = false
  const angered2 = timeVal10 - mechRef31.hitByUsT < 8
  if (ourSide2.alive && canSee2 && gap22 < LANCE_REACH[level2] && (mechRef31.hunter || angered2)) {
    if (timeVal10 > (mechRef31 as any).flipT) ((mechRef31 as any).flipT = timeVal10 + 1 + bigR6() * 2), ((mechRef31 as any).side = bigR6() < 0.5 ? -1 : 1)
    const step9 = gap22 < 90 ? ((mechRef31 as any).side ?? 1) : 0
    const rhs18 = Math.atan2(ourSide2.pos[1] - mechRef31.pos[1], ourSide2.pos[0] - mechRef31.pos[0])
    const outward2 = gap22 > LANCE_REACH[level2] * 0.8 ? 0.7 : gap22 < 30 ? -0.5 : 0
    mechRef31.move = [Math.cos(rhs18) * outward2 - Math.sin(rhs18) * step9 * 0.8, Math.sin(rhs18) * outward2 + Math.cos(rhs18) * step9 * 0.8]
    if (obstructed(mechRef31.pos[0] + mechRef31.move[0] * 4, mechRef31.pos[1] + mechRef31.move[1] * 4, mechRef31.pos[2])) mechRef31.move = [Math.cos(rhs18) * outward2, Math.sin(rhs18) * outward2]
    const leadShot = level2 >= 1 ? gap22 / 252 : 0
    const pointX3 = ourSide2.pos[0] + ourSide2.vel[0] * leadShot, pointY3 = ourSide2.pos[1] + ourSide2.vel[1] * leadShot
    const failure2 = (LANCE_JITTER[level2] * LANCE_FACTOR * Math.PI) / 180
    mechRef31.cmdAim = [Math.atan2(pointY3 - mechRef31.pos[1], pointX3 - mechRef31.pos[0]) + normalNoise(bigR6) * failure2, Math.atan2(ourSide2.pos[2] + 4.5 - mechRef31.pos[2] - 7, gap22)]
    const lockedOn3 = Math.abs(angDiff(mechRef31.cmdAim[0], mechRef31.aim[0])) < 0.05
    mechRef31.fire = [lockedOn3, false]
    const weightB2 = mechRef31.weapons[1]
    if (weightB2?.kind === "laser") mechRef31.fire[1] = !((weightB2.charge ?? 0) >= weightB2.stats.charge && lockedOn3)
    return
  }
  const destination3: V2 = ourSide2.alive && mechRef31.hunter && (SCHEDULED || gap22 < LANCE_PURSUIT[level2]) ? [ourSide2.pos[0], ourSide2.pos[1]] : [bigZ4.next.c[0] + Math.cos(mechRef31.id * 2.4) * bigZ4.next.r * 0.4, bigZ4.next.c[1] + Math.sin(mechRef31.id * 2.4) * bigZ4.next.r * 0.4]
  const radiusVal24 = mechRef31.nav!.steer(mechRef31.pos[0], mechRef31.pos[1], destination3[0], destination3[1], timeVal10, 2500)
  mechRef31.move = radiusVal24.arrived ? [0, 0] : radiusVal24.dir
  mechRef31.jump = radiusVal24.jump
  const gaze2 = ourSide2.alive && mechRef31.hunter ? Math.atan2(ourSide2.pos[1] - mechRef31.pos[1], ourSide2.pos[0] - mechRef31.pos[0]) : Math.atan2(mechRef31.move[1], mechRef31.move[0]) + Math.sin(timeVal10 + mechRef31.id) * 1.2
  mechRef31.cmdAim = [gaze2 || mechRef31.aim[0], 0]
}
