import { log, run, type Bot, type Hello, type Observation } from "./mechfight.ts"
import { Brain } from "./src/brain.ts"

/** Minimal fallback if the full brain cannot start (e.g. map missing): go to the zone, shoot what we see. */
function backupBot(): Bot {
  return {
    name: "steelwind",
    act(observation: Observation) {
      const [posX, posY, posZ] = observation.self.pos
      const goalPt = observation.zone.next_center
      const heading = Math.atan2(goalPt[1] - posY, goalPt[0] - posX)
      const enemy = observation.mechs.filter((mechRef) => mechRef.kind === "mech").sort((lhs, rhs) => Math.hypot(lhs.pos[0] - posX, lhs.pos[1] - posY) - Math.hypot(rhs.pos[0] - posX, rhs.pos[1] - posY))[0]
      if (!enemy) return { move: [Math.cos(heading), Math.sin(heading)], aim: [heading, 0], boost: !observation.self.in_zone, ...(observation.offer && { pick: 0 }) }
      const gap = Math.hypot(enemy.pos[0] - posX, enemy.pos[1] - posY)
      return { move: [Math.cos(heading), Math.sin(heading)], aim: [Math.atan2(enemy.pos[1] - posY, enemy.pos[0] - posX), Math.atan2(enemy.pos[2] + 4.5 - posZ - 7, gap)], fire: [true, false], ...(observation.offer && { pick: 0 }) }
    },
  }
}

run((helloMsg: Hello) => {
  for (const directory of [helloMsg.map?.dir, "/arena"]) {
    if (!directory) continue
    try {
      return new Brain({ ...helloMsg, map: { ...helloMsg.map, dir: directory } })
    } catch (enemy2) {
      log(`brain failed with map dir ${directory}:`, enemy2)
    }
  }
  return backupBot()
})
