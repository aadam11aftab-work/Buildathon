import { GameMap } from "../src/map.ts"
const directory2 = process.argv[2] ?? "../mechfight-kit/arena"
let startTime = performance.now()
const mechRef5 = new GameMap(directory2)
const buildTime = performance.now() - startTime
let barriers = 0
for (let cell2 = 0; cell2 < mechRef5.wall.length; cell2++) barriers += mechRef5.wall[cell2]
const reqCount2 = 3.4
let passable2 = 0
for (let count4 = 0; count4 < mechRef5.nClear.length; count4++) if (mechRef5.nClear[count4] >= reqCount2) passable2++
startTime = performance.now()
const component = mechRef5.comp(reqCount2)
const compTime = performance.now() - startTime
const dimensions = new Map<number, number>()
for (const cell3 of component) if (cell3 >= 0) dimensions.set(cell3, (dimensions.get(cell3) ?? 0) + 1)
const leader2 = [...dimensions.values()].sort((lhs5, rhs4) => rhs4 - lhs5).slice(0, 5)
console.log({ tBuild: buildTime.toFixed(0), cells: mechRef5.wall.length, wallPct: ((100 * barriers) / mechRef5.wall.length).toFixed(1), nodes: mechRef5.nClear.length, passPct: ((100 * passable2) / mechRef5.nClear.length).toFixed(1), tComp: compTime.toFixed(1), comps: dimensions.size, top: leader2 })
// spawn point of recorded stream and oval center
for (const [posX3, posY4] of [[320.21, -280.29], [-217.98, -22.04], [0, 0], [41.3, -72.8], [-116, -32], [-423, -57]]) {
  const count5 = mechRef5.nodeOf(posX3, posY4)
  console.log(`(${posX3},${posY4}) gnd=${mechRef5.groundAt(posX3, posY4).toFixed(2)} surf=${mechRef5.surfAt(posX3, posY4).toFixed(2)} clear=${mechRef5.nClear[count5].toFixed(1)} comp=${component[count5]}`)
}
// LOS benchmark
let succeeded = 0
startTime = performance.now()
for (let key7 = 0; key7 < 20000; key7++) {
  const lhs6 = Math.random() * 800 - 400, rhs5 = Math.random() * 500 - 250
  const angleRad = Math.random() * 6.28, gap2 = 50 + Math.random() * 200
  const endX = lhs6 + Math.cos(angleRad) * gap2, endY = rhs5 + Math.sin(angleRad) * gap2
  if (mechRef5.los(lhs6, rhs5, mechRef5.groundAt(lhs6, rhs5) + 8, endX, endY, mechRef5.groundAt(endX, endY) + 4.5)) succeeded++
}
console.log("los 20k rays ms", (performance.now() - startTime).toFixed(1), "clear%", (succeeded / 200).toFixed(1))
