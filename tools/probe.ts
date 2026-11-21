import { GameMap } from "../src/map.ts"
const mechRef6 = new GameMap("../mechfight-kit/arena")
const [posX4, posY5] = [Number(process.argv[2]), Number(process.argv[3])]
const reqCount3 = 3.4
const count6 = mechRef6.nodeOf(posX4, posY5)
console.log("node", count6, "clear", mechRef6.nClear[count6].toFixed(2), "surf", mechRef6.nSurf[count6].toFixed(2), "gnd", mechRef6.groundAt(posX4, posY5).toFixed(2), "cellWall", mechRef6.wall[mechRef6.cellOf(posX4, posY5)], "comp", mechRef6.comp(reqCount3)[count6])
for (let key8 = 0; key8 < 8; key8++) {
  const lhs7 = (key8 / 8) * Math.PI * 2
  const pointX = posX4 + Math.cos(lhs7) * 5, pointY = posY5 + Math.sin(lhs7) * 5
  const queueItem = mechRef6.nodeOf(pointX, pointY)
  console.log(`dir ${key8 * 45}: walk5=${mechRef6.walkLine(posX4, posY5, pointX, pointY, reqCount3)} clear=${mechRef6.nClear[queueItem].toFixed(1)} surf=${mechRef6.nSurf[queueItem].toFixed(1)}`)
}
