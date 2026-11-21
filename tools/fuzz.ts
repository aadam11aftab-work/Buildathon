// Robustness: feed mutated real observations to the Brain and count exceptions.
import { readFileSync } from "node:fs"
import { gunzipSync } from "node:zlib"
import { Brain } from "../src/brain.ts"
import { draw } from "../mechfight.ts"
import { rng } from "../src/geom.ts"

const kitDir = "../mechfight-kit"
const textLines = gunzipSync(readFileSync(kitDir + "/check/stream.jsonl.gz")).toString("utf8").trim().split("\n").map((lineRef) => JSON.parse(lineRef))
const helloMsg2 = textLines[0]
helloMsg2.map.dir = kitDir + "/arena"
const observation2 = textLines.filter((mechRef3: any) => mechRef3.type === "obs")
const bigR2 = rng(42)
let failures = 0, count3 = 0
const spotted2 = new Map<string, number>()
for (let execute = 0; execute < 12; execute++) {
  const rhs3 = new Brain(JSON.parse(JSON.stringify(helloMsg2)))
  for (const origin0 of observation2) {
    const other3 = JSON.parse(JSON.stringify(origin0))
    // mutations
    for (const mechRef4 of other3.mechs) {
      if (bigR2() < 0.3) delete mechRef4.hp
      if (bigR2() < 0.3) delete mechRef4.max_hp
      if (bigR2() < 0.3) delete mechRef4.weapons
      if (bigR2() < 0.2) delete mechRef4.cubes
      if (bigR2() < 0.1) mechRef4.kind = "npc"
    }
    if (bigR2() < 0.1) other3.mechs.push({ id: 100 + Math.floor(bigR2() * 12), kind: "npc", pos: [other3.self.pos[0] + 20, other3.self.pos[1], other3.self.pos[2]], vel: [0, 0, 0], aim: [0, 0] })
    if (bigR2() < 0.1) other3.self.weapons = [other3.self.weapons[0], { kind: ["laser", "lightning", "mortar"][Math.floor(bigR2() * 3)], cooldown: bigR2(), charge: bigR2() * 3, stats: {} }]
    if (bigR2() < 0.05) other3.self.weapons = [null, null]
    if (bigR2() < 0.1) other3.offer = { id: 1, expires: other3.t + 5, choices: [{ id: "plating", name: "", desc: "" }].slice(0, Math.floor(bigR2() * 2)) }
    if (bigR2() < 0.1) other3.drops = [{ id: 20000, pos: [other3.self.pos[0] + 50, other3.self.pos[1], other3.self.pos[2]], radius: 8, lands_in: bigR2() * 20, capture_s: 4, progress: 0, holder: bigR2() < 0.5 ? null : 5 }]
    if (bigR2() < 0.1) other3.projectiles.push({ id: 1, kind: "mortar", pos: [other3.self.pos[0] + 30, other3.self.pos[1], other3.self.pos[2] + 40], vel: [-10, 0, 5] })
    if (bigR2() < 0.1) other3.beams.push({ by: 5, kind: "laser", path: [[0, 0, 0], [1, 1, 1]], age: 0.1 })
    if (bigR2() < 0.1) other3.events.push({ type: "damaged", amount: 50, weapon: bigR2() < 0.5 ? "zone" : "laser", bearing: bigR2() < 0.3 ? null : 1, by: bigR2() < 0.3 ? null : 5 })
    if (bigR2() < 0.05) other3.self.pos = [bigR2() * 1000 - 500, bigR2() * 700 - 330, 30]
    if (bigR2() < 0.05) other3.zone = { center: [0, 0], radius: 10, next_center: [5, 5], next_radius: 0, shrink_start: other3.t - 1, shrink_end: other3.t + 5, dps: 260 }
    count3++
    try {
      const lhs4 = rhs3.act(other3)
      if (!lhs4.move || !lhs4.aim || lhs4.aim.some((vec3: number) => !Number.isFinite(vec3)) || lhs4.move.some((vec4: number) => !Number.isFinite(vec4))) throw new Error("bad action " + JSON.stringify(lhs4).slice(0, 120))
    } catch (enemy3: any) {
      failures++
      const key5 = String(enemy3?.stack ?? enemy3).split("\n").slice(0, 2).join(" | ")
      spotted2.set(key5, (spotted2.get(key5) ?? 0) + 1)
    }
    draw.clear()
  }
}
console.log({ n: count3, errors: failures })
for (const [key6, vec5] of spotted2) console.log(vec5, key6)
