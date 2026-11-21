// How much do tree crowns hide a standing mech from far observers, on top of buildings and terrain?
import { GameMap } from "../src/map.ts"
const mechRef2 = new GameMap("../mechfight-kit/arena")
const reqCount = 3.4
const bigR = Number(process.argv[2] ?? 120)
let sumTotal = 0
const history = { los: new Array(17).fill(0), can: new Array(17).fill(0) }
for (let count2 = 0; count2 < mechRef2.NW * mechRef2.NH; count2 += 3) {
  if (!mechRef2.passable(count2, reqCount)) continue
  const posX2 = mechRef2.nodeX(count2), posY3 = mechRef2.nodeY(count2), posZ3 = mechRef2.surfAt(posX2, posY3)
  sumTotal++
  let histLen = 0, histCount = 0
  for (let key2 = 0; key2 < 16; key2++) {
    const lhs2 = (key2 / 16) * Math.PI * 2
    const enemyX = posX2 + Math.cos(lhs2) * bigR, enemyY = posY3 + Math.sin(lhs2) * bigR, enemyZ = mechRef2.surfAt(enemyX, enemyY) + 8
    const laserCount = mechRef2.los(enemyX, enemyY, enemyZ, posX2, posY3, posZ3 + 4.5), laserTime = mechRef2.los(enemyX, enemyY, enemyZ, posX2, posY3, posZ3 + 9)
    if (!laserCount && !laserTime) histLen++
    const cell = mechRef2.sees(enemyX, enemyY, enemyZ, posX2, posY3, posZ3 + 4.5), leader = mechRef2.sees(enemyX, enemyY, enemyZ, posX2, posY3, posZ3 + 9)
    if (!cell && !leader) histCount++
  }
  history.los[histLen]++
  history.can[histCount]++
}
const portion = (heightVal: number[], key3: number) => ((heightVal.slice(key3).reduce((lhs3, rhs2) => lhs3 + rhs2, 0) / sumTotal) * 100).toFixed(1) + "%"
console.log(`observers at ${bigR} m, eye 8 m; share of nav points hidden from at least k of 16 directions`)
for (const key4 of [4, 8, 12, 14, 16]) console.log(`k=${key4}: buildings/terrain only ${portion(history.los, key4)}   + tree crowns ${portion(history.can, key4)}`)
