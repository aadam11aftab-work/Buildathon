// Profile slow decisions: replay a jsonl stream through the Brain, time wrapped methods per tick.
import { readFileSync } from "node:fs"
import { Brain } from "../src/brain.ts"
import { draw } from "../mechfight.ts"
const fileA = process.argv[2]
const textLines2 = readFileSync(fileA, "utf8").trim().split("\n").map((lineRef2) => JSON.parse(lineRef2))
const helloMsg3 = textLines2[0]
helloMsg3.map.dir = "../mechfight-kit/arena"
const fileB = fileA
const rhs6: any = new Brain(helloMsg3)
const accumulator = new Map<string, number>()
const wrapAngle = (record: any, label0: string, caption: string) => {
  const entry = record[label0].bind(record)
  record[label0] = (...lhs8: any[]) => {
    const startTime2 = performance.now()
    try { return entry(...lhs8) } finally { accumulator.set(caption, (accumulator.get(caption) ?? 0) + performance.now() - startTime2) }
  }
}
wrapAngle(rhs6.nav, "steer", "nav.steer")
wrapAngle(rhs6.nav.pf, "run", "astar.run")
wrapAngle(rhs6.nav.pf, "begin", "astar.begin")
wrapAngle(rhs6.m, "snap", "snap")
wrapAngle(rhs6.m, "comp", "comp")
wrapAngle(rhs6.m, "walkLine", "walkLine")
for (const key9 of ["coverPoint", "roam", "dropGoal", "steerLocal", "fireControl", "dodge", "wantedPickup", "duelItem", "fightMove", "look"]) wrapAngle(rhs6.practice, key9, "P." + key9)
for (const key10 of ["zoneGoal", "needRotate", "lootGoal", "dropGoal", "replan", "fireControl", "lookDir", "mortarDodge", "findCover"]) wrapAngle(rhs6, key10, key10)
const locTimes: number[] = []
for (const other4 of textLines2.slice(1)) {
  if (other4.type !== "obs") continue
  accumulator.clear()
  const startTime3 = performance.now()
  rhs6.act(other4)
  const millis = performance.now() - startTime3
  locTimes.push(millis)
  draw.clear()
  if (millis > 4) console.log(other4.t, rhs6.state, millis.toFixed(1), [...accumulator.entries()].filter(([, vec6]) => vec6 > 0.5).map(([key11, vec7]) => `${key11}=${vec7.toFixed(1)}`).join(" "))
}
locTimes.sort((lhs9, rhs7) => lhs9 - rhs7)
console.log("p99", locTimes[Math.floor(locTimes.length * 0.99)].toFixed(2), "max", locTimes[locTimes.length - 1].toFixed(1))
