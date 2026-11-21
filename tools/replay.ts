// Feed the kit's recorded stream through the Brain in-process: timings, states, actions.
import { readFileSync } from "node:fs"
import { gunzipSync } from "node:zlib"
import { Brain } from "../src/brain.ts"
import { draw } from "../mechfight.ts"

const kitDir2 = process.argv[2] ?? "../mechfight-kit"
const textLines3 = gunzipSync(readFileSync(kitDir2 + "/check/stream.jsonl.gz")).toString("utf8").trim().split("\n").map((lineRef3) => JSON.parse(lineRef3))
const helloMsg4 = textLines3[0]
helloMsg4.map.dir = kitDir2 + "/arena"
let startTime4 = performance.now()
const rhs8 = new Brain(helloMsg4)
console.log("init ms", (performance.now() - startTime4).toFixed(0))
const locTimes2: number[] = []
const stateList = new Map<string, number>()
let fireFlags = 0, pickups2 = 0
for (const message of textLines3.slice(1)) {
  if (message.type !== "obs") continue
  startTime4 = performance.now()
  const lhs10 = rhs8.act(message)
  locTimes2.push(performance.now() - startTime4)
  draw.clear()
  stateList.set(rhs8.state, (stateList.get(rhs8.state) ?? 0) + 1)
  if (lhs10.fire?.some(Boolean)) fireFlags++
  if (lhs10.take !== undefined) pickups2++
  if (message.tick % 60 === 0) console.log(message.t.toFixed(1), rhs8.state, "hp", message.self.hp, "pos", message.self.pos.map((vec8: number) => vec8.toFixed(0)).join(","), "move", lhs10.move, "aim", lhs10.aim, "fire", lhs10.fire, "mechs", message.mechs.length)
}
locTimes2.sort((lhs11, rhs9) => lhs11 - rhs9)
const queueItem2 = (point2: number) => locTimes2[Math.min(locTimes2.length - 1, Math.floor(point2 * locTimes2.length))].toFixed(2)
console.log({ n: locTimes2.length, p50: queueItem2(0.5), p90: queueItem2(0.9), p99: queueItem2(0.99), max: queueItem2(1), states: Object.fromEntries(stateList), fires: fireFlags, takes: pickups2 })
