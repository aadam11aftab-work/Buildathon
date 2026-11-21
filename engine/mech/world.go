package mech

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
)

type Slab struct{ Base, Top float32 }

type World struct {
	MinX, MinY float64
	NX, NY     int
	Ground     []float32
	Top        []float32
	Walk       []float32
	Obst       []bool
	Low        []bool
	HasSlab    []bool
	Slabs      map[int32][]Slab
	BX0, BX1   float64
	BY0, BY1   float64

	NS        float64
	NNX, NNY  int
	Blocked   []bool
	Surf      []float32
	Clearance []float32
	Comp      []int32
	MainComp  int32
	mainNear  []int32

	geo

	g      []float32
	came   []int32
	stamp  []uint32
	closed []uint32
	gen    uint32
	heap   []heapItem
}

type heapItem struct {
	f   float32
	idx int32
}

type interval struct {
	base, top float32
	bridge    bool
}

func LoadWorld(locDir string) (*World, error) {
	locRaw2, locErr7 := os.ReadFile(filepath.Join(locDir, "v2", "world.json"))
	if locErr7 != nil {
		return nil, locErr7
	}
	var locWj struct {
		Bounds struct {
			MinX, MaxX, MinY, MaxY float64
		} `json:"bounds"`
		Terrain struct {
			MinX float64 `json:"minX"`
			MinY float64 `json:"minY"`
			Step float64 `json:"step"`
			NX   int     `json:"nx"`
			NY   int     `json:"ny"`
		} `json:"terrain"`
		Prisms []struct {
			Kind  string      `json:"kind"`
			Base  float64     `json:"base"`
			Top   float64     `json:"top"`
			Ring  []float64   `json:"ring"`
			Holes [][]float64 `json:"holes"`
		} `json:"prisms"`
		Rules Rules `json:"rules"`
		Trees struct {
			Fields []string  `json:"fields"`
			Data   []float64 `json:"data"`
		} `json:"trees"`
	}
	if locErr3 := json.Unmarshal(locRaw2, &locWj); locErr3 != nil {
		return nil, locErr3
	}
	locTb, locErr7 := os.ReadFile(filepath.Join(locDir, "v2", "terrain.i16"))
	if locErr7 != nil {
		return nil, locErr7
	}
	locW23 := &World{MinX: locWj.Terrain.MinX, MinY: locWj.Terrain.MinY, NX: locWj.Terrain.NX, NY: locWj.Terrain.NY}
	locN := locW23.NX * locW23.NY
	if len(locTb) < locN*2 || locN == 0 {
		return nil, fmt.Errorf("terrain size mismatch")
	}
	locW23.BX0, locW23.BX1, locW23.BY0, locW23.BY1 = locWj.Bounds.MinX, locWj.Bounds.MaxX, locWj.Bounds.MinY, locWj.Bounds.MaxY
	locW23.Ground = make([]float32, locN)
	for locI31 := 0; locI31 < locN; locI31++ {
		locW23.Ground[locI31] = float32(int16(binary.LittleEndian.Uint16(locTb[locI31*2:]))) * 0.01
	}
	var locPrisms []Prism
	for _, locP18 := range locWj.Prisms {
		locQ4 := Prism{Kind: locP18.Kind, Base: locP18.Base, Top: locP18.Top, MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1)}
		for _, locR8 := range append([][]float64{locP18.Ring}, locP18.Holes...) {
			locM2 := len(locR8) / 2
			for locA8 := 0; locA8 < locM2; locA8++ {
				locB29 := (locA8 + 1) % locM2
				locQ4.Edges = append(locQ4.Edges, locR8[locA8*2], locR8[locA8*2+1], locR8[locB29*2], locR8[locB29*2+1])
				locQ4.MinX, locQ4.MaxX = math.Min(locQ4.MinX, locR8[locA8*2]), math.Max(locQ4.MaxX, locR8[locA8*2])
				locQ4.MinY, locQ4.MaxY = math.Min(locQ4.MinY, locR8[locA8*2+1]), math.Max(locQ4.MaxY, locR8[locA8*2+1])
			}
		}
		locPrisms = append(locPrisms, locQ4)
	}
	var locTrees2 []Tree
	if locNf := len(locWj.Trees.Fields); locNf > 0 {
		locCol := map[string]int{}
		for locI48, locF2 := range locWj.Trees.Fields {
			locCol[locF2] = locI48
		}
		locGet := func(locRow2 []float64, locName string, locDef float64) float64 {
			if locI37, locOk12 := locCol[locName]; locOk12 {
				return locRow2[locI37]
			}
			return locDef
		}
		for locK9 := 0; locK9+locNf <= len(locWj.Trees.Data); locK9 += locNf {
			locRow := locWj.Trees.Data[locK9 : locK9+locNf]
			locT24 := Tree{X: locGet(locRow, "x", 0), Y: locGet(locRow, "y", 0), Z: locGet(locRow, "z", 0), Height: locGet(locRow, "height", 0),
				CrownR: locGet(locRow, "crownR", 1), CrownBase: locGet(locRow, "crownBase", 0), TrunkR: locGet(locRow, "trunkR", 0)}
			locT24.CX, locT24.CY = locGet(locRow, "crownX", locT24.X), locGet(locRow, "crownY", locT24.Y)
			locCb, locTop3 := locT24.Z+locT24.CrownBase, locT24.Z+locT24.Height
			locT24.TrunkTop = locT24.Z + math.Max(locT24.CrownBase+0.5*(locT24.Height-locT24.CrownBase), 3)
			locT24.CZ = (locCb + locTop3) / 2
			locT24.RZ = math.Max((locTop3-locCb)/2, 0.5)
			locTrees2 = append(locTrees2, locT24)
		}
	}
	if locWj.Rules.MaxStep == 0 {
		locWj.Rules.MaxStep = 1.5
	}
	locW23.initGeo(locPrisms, locTrees2, locWj.Rules)

	locCells := make(map[int32][]interval)
	for _, locP10 := range locWj.Prisms {
		locRings := append([][]float64{locP10.Ring}, locP10.Holes...)
		locMinY, locMaxY := math.Inf(1), math.Inf(-1)
		for locK4 := 1; locK4 < len(locP10.Ring); locK4 += 2 {
			locMinY = math.Min(locMinY, locP10.Ring[locK4])
			locMaxY = math.Max(locMaxY, locP10.Ring[locK4])
		}
		locJ0 := int(math.Ceil(locMinY - locW23.MinY))
		locJ1 := int(math.Floor(locMaxY - locW23.MinY))
		locIv := interval{float32(locP10.Base), float32(locP10.Top), locP10.Kind == "bridge"}
		var locXs []float64
		for locJ8 := max(locJ0, 0); locJ8 <= locJ1 && locJ8 < locW23.NY; locJ8++ {
			locY51 := locW23.MinY + float64(locJ8) + 1e-4
			locXs = locXs[:0]
			for _, locR6 := range locRings {
				locM8 := len(locR6) / 2
				for locA5 := 0; locA5 < locM8; locA5++ {
					locB32 := (locA5 + 1) % locM8
					locX08, locY05, locX13, locY114 := locR6[locA5*2], locR6[locA5*2+1], locR6[locB32*2], locR6[locB32*2+1]
					if (locY05 <= locY51) != (locY114 <= locY51) {
						locXs = append(locXs, locX08+(locY51-locY05)/(locY114-locY05)*(locX13-locX08))
					}
				}
			}
			sort.Float64s(locXs)
			for locK24 := 0; locK24+1 < len(locXs); locK24 += 2 {
				locI0 := int(math.Ceil(locXs[locK24] - locW23.MinX))
				locI1 := int(math.Floor(locXs[locK24+1] - locW23.MinX))
				for locI46 := max(locI0, 0); locI46 <= locI1 && locI46 < locW23.NX; locI46++ {
					locIdx2 := int32(locJ8*locW23.NX + locI46)
					locCells[locIdx2] = append(locCells[locIdx2], locIv)
				}
			}
		}
	}
	locW23.Top = make([]float32, locN)
	locW23.Walk = make([]float32, locN)
	locW23.Obst = make([]bool, locN)
	locW23.Low = make([]bool, locN)
	locW23.HasSlab = make([]bool, locN)
	copy(locW23.Top, locW23.Ground)
	copy(locW23.Walk, locW23.Ground)
	locW23.Slabs = make(map[int32][]Slab)
	for locIdx3, locIvs := range locCells {
		sort.Slice(locIvs, func(locA22, locB8 int) bool { return locIvs[locA22].base < locIvs[locB8].base })
		locG9 := locW23.Ground[locIdx3]
		locTop := locG9
		locDeck3 := float32(-1e9)
		locLow := false
		for _, locIv2 := range locIvs {
			if locIv2.top <= locTop {
				continue
			}
			if locIv2.base <= locTop+1.5 {
				locTop = locIv2.top
				continue
			}
			locW23.Slabs[locIdx3] = append(locW23.Slabs[locIdx3], Slab{locIv2.base, locIv2.top})
			locW23.HasSlab[locIdx3] = true
			if locIv2.base-locTop < 10.5 {
				if locIv2.bridge && locIv2.top-locG9 < 6.5 {
					locDeck3 = locIv2.top
				} else {
					locLow = true
				}
			}
		}
		locW23.Top[locIdx3] = locTop
		locW23.Walk[locIdx3] = locTop
		if locDeck3 > locTop {
			locW23.Walk[locIdx3] = locDeck3
		}
		locW23.Low[locIdx3] = locLow
		locW23.Obst[locIdx3] = locTop-locG9 > 1.2 || locLow
	}
	locW23.buildNav()
	return locW23, nil
}

func (locW48 *World) vidx(locX25, locY35 float64) int {
	locI13 := int(math.Round(locX25 - locW48.MinX))
	locJ15 := int(math.Round(locY35 - locW48.MinY))
	if locI13 < 0 {
		locI13 = 0
	} else if locI13 >= locW48.NX {
		locI13 = locW48.NX - 1
	}
	if locJ15 < 0 {
		locJ15 = 0
	} else if locJ15 >= locW48.NY {
		locJ15 = locW48.NY - 1
	}
	return locJ15*locW48.NX + locI13
}

func (locW3 *World) InBounds(locX31, locY38 float64) bool {
	return locX31 > locW3.BX0 && locX31 < locW3.BX1 && locY38 > locW3.BY0 && locY38 < locW3.BY1
}

func (locW52 *World) GroundAt(locX6, locY43 float64) float64 {
	locFx := locX6 - locW52.MinX
	locFy := locY43 - locW52.MinY
	locFx = math.Max(0, math.Min(locFx, float64(locW52.NX-1)-1e-6))
	locFy = math.Max(0, math.Min(locFy, float64(locW52.NY-1)-1e-6))
	locI12, locJ2 := int(locFx), int(locFy)
	locTx2, locTy := locFx-float64(locI12), locFy-float64(locJ2)
	locA10 := float64(locW52.Ground[locJ2*locW52.NX+locI12])
	locB44 := float64(locW52.Ground[locJ2*locW52.NX+locI12+1])
	locC29 := float64(locW52.Ground[(locJ2+1)*locW52.NX+locI12])
	locD27 := float64(locW52.Ground[(locJ2+1)*locW52.NX+locI12+1])
	return (locA10*(1-locTx2)+locB44*locTx2)*(1-locTy) + (locC29*(1-locTx2)+locD27*locTx2)*locTy
}

func (locW31 *World) stack(locX39, locY8 float64, locDeck bool) float64 {
	locZ21, _ := locW31.stackKind(locX39, locY8, locDeck)
	return locZ21
}

func (locW26 *World) stackKind(locX11, locY30 float64, locDeck2 bool) (float64, int) {
	locG2 := locW26.GroundAt(locX11, locY30)
	locZ5 := locG2
	locId7 := -1
	locC16 := locW26.cellOf(locX11, locY30)
	if locC16 < 0 {
		return locZ5, locId7
	}
	locPs := locW26.geo.gridP[locC16]
	for locPass := 0; locPass < 8; locPass++ {
		locMoved := false
		for _, locPi2 := range locPs {
			locP := &locW26.geo.Prisms[locPi2]
			if locP.Top <= locZ5 || locX11 < locP.MinX || locX11 > locP.MaxX || locY30 < locP.MinY || locY30 > locP.MaxY {
				continue
			}
			if !(locP.Base <= locZ5+1.5 || locDeck2 && locP.Kind == "bridge" && locP.Top-locG2 < 6.5) {
				continue
			}
			if pointInPoly(locP.Edges, locX11, locY30) {
				locZ5, locId7 = locP.Top, int(locPi2)
				locMoved = true
			}
		}
		if !locMoved {
			break
		}
	}
	return locZ5, locId7
}

func (locW58 *World) WalkAt(locX8, locY33 float64) float64 { return locW58.stack(locX8, locY33, true) }

func (locW54 *World) TopAt(locX52, locY24 float64) float64 {
	return locW54.stack(locX52, locY24, false)
}

func (locW30 *World) Solid(locX35, locY32, locZ24 float64) bool {
	if locZ24 < locW30.GroundAt(locX35, locY32) {
		return true
	}
	locC52 := locW30.cellOf(locX35, locY32)
	if locC52 < 0 {
		return false
	}
	for _, locPi := range locW30.geo.gridP[locC52] {
		locP22 := &locW30.geo.Prisms[locPi]
		if locZ24 > locP22.Base && locZ24 < locP22.Top && locX35 >= locP22.MinX && locX35 <= locP22.MaxX && locY32 >= locP22.MinY && locY32 <= locP22.MaxY && pointInPoly(locP22.Edges, locX35, locY32) {
			return true
		}
	}
	return false
}

func (locW59 *World) solidRaster(locX51, locY21, locZ22 float64) bool {
	locIdx := locW59.vidx(locX51, locY21)
	if locZ22 < float64(locW59.Top[locIdx])-0.05 {
		return true
	}
	if locW59.HasSlab[locIdx] {
		for _, locS11 := range locW59.Slabs[int32(locIdx)] {
			if locZ22 > float64(locS11.Base) && locZ22 < float64(locS11.Top) {
				return true
			}
		}
	}
	return false
}

func (locW50 *World) Clear(locA11, locB26 Vec3) bool { return locW50.ShotClear(locA11, locB26) }

func (locW46 *World) clearRaster(locA, locB35 Vec3) bool {
	locDx16, locDy3, locDz := locB35[0]-locA[0], locB35[1]-locA[1], locB35[2]-locA[2]
	locD33 := math.Sqrt(locDx16*locDx16 + locDy3*locDy3 + locDz*locDz)
	locN15 := int(locD33/1.0) + 1
	locInv := 1 / float64(locN15)
	for locK19 := 1; locK19 < locN15; locK19++ {
		locT23 := float64(locK19) * locInv
		if locW46.solidRaster(locA[0]+locDx16*locT23, locA[1]+locDy3*locT23, locA[2]+locDz*locT23) {
			return false
		}
	}
	return true
}

var NavRadius = 2.6

func (locW27 *World) standable(locX17, locY52, locZ8, locR14 float64) bool {
	if locX17 < locW27.BX0+locR14 || locX17 > locW27.BX1-locR14 || locY52 < locW27.BY0+locR14 || locY52 > locW27.BY1-locR14 {
		return false
	}
	return !locW27.wallWithin(locX17, locY52, locZ8, locR14, 9)
}

func (locW56 *World) wallWithin(locX29, locY49, locZ26, locR29, locH17 float64) bool {
	locLo2, locHi5 := locZ26+StepUp, locZ26+locH17
	locR22 := locR29 * locR29
	locFound2 := false
	locW56.forCells(locX29-locR29, locY49-locR29, locX29+locR29, locY49+locR29, func(locC26 int) {
		if locFound2 {
			return
		}
		for _, locPi7 := range locW56.geo.gridP[locC26] {
			locP9 := &locW56.geo.Prisms[locPi7]
			if locP9.Top <= locLo2 || locP9.Base >= locHi5 || locX29+locR29 < locP9.MinX || locX29-locR29 > locP9.MaxX || locY49+locR29 < locP9.MinY || locY49-locR29 > locP9.MaxY {
				continue
			}
			locE6 := locP9.Edges
			for locI := 0; locI < len(locE6); locI += 4 {
				locX05, locY07 := locE6[locI], locE6[locI+1]
				locUx3, locUy7 := locE6[locI+2]-locX05, locE6[locI+3]-locY07
				locL23 := locUx3*locUx3 + locUy7*locUy7
				locT6 := 0.0
				if locL23 > 1e-9 {
					locT6 = math.Max(0, math.Min(1, ((locX29-locX05)*locUx3+(locY49-locY07)*locUy7)/locL23))
				}
				if locMx10, locMy2 := locX05+locUx3*locT6-locX29, locY07+locUy7*locT6-locY49; locMx10*locMx10+locMy2*locMy2 < locR22 {
					locFound2 = true
					return
				}
			}
			if pointInPoly(locE6, locX29, locY49) {
				locFound2 = true
				return
			}
		}
	})
	return locFound2
}

func (locW10 *World) buildNav() {
	locW10.NS = 2
	locW10.NNX = (locW10.NX + 1) / 2
	locW10.NNY = (locW10.NY + 1) / 2
	locN4 := locW10.NNX * locW10.NNY
	locW10.Blocked = make([]bool, locN4)
	locW10.Surf = make([]float32, locN4)
	for locC44 := 0; locC44 < locN4; locC44++ {
		locX16, locY16 := locW10.CellPos(locC44)
		locZ30, locId8 := locW10.stackKind(locX16, locY16, true)
		locW10.Surf[locC44] = float32(locZ30)

		locRoof := locId8 >= 0 && locW10.geo.Prisms[locId8].Kind != "bridge" && locZ30-locW10.GroundAt(locX16, locY16) > 1.5
		locW10.Blocked[locC44] = locRoof || !locW10.standable(locX16, locY16, locZ30, NavRadius)
	}

	locW10.Clearance = make([]float32, locN4)
	locQ := make([]int32, 0, locN4)
	for locC54 := 0; locC54 < locN4; locC54++ {
		if locW10.Blocked[locC54] {
			locW10.Clearance[locC54] = 0
			locQ = append(locQ, int32(locC54))
		} else {
			locW10.Clearance[locC54] = 1e9
		}
	}
	for locH14 := 0; locH14 < len(locQ); locH14++ {
		locC5 := int(locQ[locH14])
		locD25 := locW10.Clearance[locC5] + float32(locW10.NS)
		if locD25 > 20 {
			continue
		}
		locCi5, locCj3 := locC5%locW10.NNX, locC5/locW10.NNX
		for _, locO10 := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			locI27, locJ17 := locCi5+locO10[0], locCj3+locO10[1]
			if locI27 < 0 || locJ17 < 0 || locI27 >= locW10.NNX || locJ17 >= locW10.NNY {
				continue
			}
			locNb4 := locJ17*locW10.NNX + locI27
			if locW10.Clearance[locNb4] > locD25 {
				locW10.Clearance[locNb4] = locD25
				locQ = append(locQ, int32(locNb4))
			}
		}
	}
	for locC47 := range locW10.Clearance {
		if locW10.Clearance[locC47] > 20 {
			locW10.Clearance[locC47] = 20
		}
	}

	locW10.Comp = make([]int32, locN4)
	var locLabel, locBestN int32
	locBestSize := 0
	for locC0 := 0; locC0 < locN4; locC0++ {
		if locW10.Blocked[locC0] || locW10.Comp[locC0] != 0 {
			continue
		}
		locLabel++
		locW10.Comp[locC0] = locLabel
		locQ = append(locQ[:0], int32(locC0))
		for locH15 := 0; locH15 < len(locQ); locH15++ {
			locC6 := int(locQ[locH15])
			locCi3, locCj9 := locC6%locW10.NNX, locC6/locW10.NNX
			for _, locO := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				locI44, locJ13 := locCi3+locO[0], locCj9+locO[1]
				if locI44 < 0 || locJ13 < 0 || locI44 >= locW10.NNX || locJ13 >= locW10.NNY {
					continue
				}
				locNb5 := locJ13*locW10.NNX + locI44
				if locW10.Comp[locNb5] == 0 && locW10.canStep(locC6, locNb5, false) && locW10.canStep(locNb5, locC6, false) {
					locW10.Comp[locNb5] = locLabel
					locQ = append(locQ, int32(locNb5))
				}
			}
		}
		if len(locQ) > locBestSize {
			locBestSize, locBestN = len(locQ), locLabel
		}
	}
	locW10.MainComp = locBestN
	locW10.g = make([]float32, locN4)
	locW10.came = make([]int32, locN4)
	locW10.stamp = make([]uint32, locN4)
	locW10.closed = make([]uint32, locN4)
}

func (locW7 *World) Cell(locX20, locY46 float64) int {
	locCi := int(math.Round((locX20 - locW7.MinX) / locW7.NS))
	locCj2 := int(math.Round((locY46 - locW7.MinY) / locW7.NS))
	if locCi < 0 {
		locCi = 0
	} else if locCi >= locW7.NNX {
		locCi = locW7.NNX - 1
	}
	if locCj2 < 0 {
		locCj2 = 0
	} else if locCj2 >= locW7.NNY {
		locCj2 = locW7.NNY - 1
	}
	return locCj2*locW7.NNX + locCi
}

func (locW38 *World) CellPos(locC20 int) (float64, float64) {
	return locW38.MinX + float64(locC20%locW38.NNX)*locW38.NS, locW38.MinY + float64(locC20/locW38.NNX)*locW38.NS
}

func (locW5 *World) Free(locX4, locY15 float64) bool {
	return !locW5.Blocked[locW5.Cell(locX4, locY15)]
}

func (locW32 *World) NearestFree(locC22 int) int {
	if !locW32.Blocked[locC22] {
		return locC22
	}
	locCi2, locCj8 := locC22%locW32.NNX, locC22/locW32.NNX
	locBest3, locBestD2 := -1, 1<<30
	for locR26 := 1; locR26 <= 20; locR26++ {
		for locDj := -locR26; locDj <= locR26; locDj++ {
			for locDi := -locR26; locDi <= locR26; locDi++ {
				if max(abs(locDi), abs(locDj)) != locR26 {
					continue
				}
				locI3, locJ3 := locCi2+locDi, locCj8+locDj
				if locI3 < 0 || locJ3 < 0 || locI3 >= locW32.NNX || locJ3 >= locW32.NNY {
					continue
				}
				if !locW32.Blocked[locJ3*locW32.NNX+locI3] {
					if locD13 := locDi*locDi + locDj*locDj; locD13 < locBestD2 {
						locBest3, locBestD2 = locJ3*locW32.NNX+locI3, locD13
					}
				}
			}
		}
		if locBest3 >= 0 {
			return locBest3
		}
	}
	return -1
}

func abs(locA12 int) int {
	if locA12 < 0 {
		return -locA12
	}
	return locA12
}

var nbrs = [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}

func (locW13 *World) canStep(locA18, locB31 int, locDiag bool) bool {
	if locW13.Blocked[locB31] {
		return false
	}
	locRise := locW13.Surf[locB31] - locW13.Surf[locA18]
	locLim3 := float32(2.2)
	if locDiag {
		locLim3 = 3.1
	}
	return locRise <= locLim3
}

func (locW45 *World) push(locIt2 heapItem) {
	locW45.heap = append(locW45.heap, locIt2)
	locI17 := len(locW45.heap) - 1
	for locI17 > 0 {
		locP16 := (locI17 - 1) / 2
		if locW45.heap[locP16].f <= locW45.heap[locI17].f {
			break
		}
		locW45.heap[locP16], locW45.heap[locI17] = locW45.heap[locI17], locW45.heap[locP16]
		locI17 = locP16
	}
}

func (locW35 *World) pop() heapItem {
	locTop2 := locW35.heap[0]
	locLast := len(locW35.heap) - 1
	locW35.heap[0] = locW35.heap[locLast]
	locW35.heap = locW35.heap[:locLast]
	locI22 := 0
	for {
		locL3, locR11 := 2*locI22+1, 2*locI22+2
		locM := locI22
		if locL3 < locLast && locW35.heap[locL3].f < locW35.heap[locM].f {
			locM = locL3
		}
		if locR11 < locLast && locW35.heap[locR11].f < locW35.heap[locM].f {
			locM = locR11
		}
		if locM == locI22 {
			break
		}
		locW35.heap[locM], locW35.heap[locI22] = locW35.heap[locI22], locW35.heap[locM]
		locI22 = locM
	}
	return locTop2
}

func (locW12 *World) Path(locSx2, locSy3, locGx6, locGy7 float64, locMaxExpand int) (locPath []int32, locReached bool) {
	locS17 := locW12.NearestFree(locW12.Cell(locSx2, locSy3))
	locG7 := locW12.NearestFree(locW12.Cell(locGx6, locGy7))
	if locS17 < 0 || locG7 < 0 {
		return nil, false
	}
	if locS17 == locG7 {
		return []int32{int32(locS17)}, true
	}
	locW12.gen++
	locGen3 := locW12.gen
	locW12.heap = locW12.heap[:0]
	locGi, locGj := locG7%locW12.NNX, locG7/locW12.NNX
	locH := func(locC39 int) float32 {
		locDx19 := float64(abs(locC39%locW12.NNX - locGi))
		locDy13 := float64(abs(locC39/locW12.NNX - locGj))
		locMn, locMx4 := math.Min(locDx19, locDy13), math.Max(locDx19, locDy13)
		return float32((locMx4 + 0.4142*locMn) * locW12.NS * 1.15)
	}
	locW12.g[locS17] = 0
	locW12.came[locS17] = -1
	locW12.stamp[locS17] = locGen3
	locW12.push(heapItem{locH(locS17), int32(locS17)})
	locBest, locBestH := locS17, locH(locS17)
	locFound := false
	for locN13 := 0; len(locW12.heap) > 0 && locN13 < locMaxExpand; locN13++ {
		locIt3 := locW12.pop()
		locC50 := int(locIt3.idx)
		if locW12.closed[locC50] == locGen3 {
			continue
		}
		locW12.closed[locC50] = locGen3
		if locC50 == locG7 {
			locFound = true
			locBest = locC50
			break
		}
		if locHc := locH(locC50); locHc < locBestH {
			locBest, locBestH = locC50, locHc
		}
		locCi9, locCj5 := locC50%locW12.NNX, locC50/locW12.NNX
		for locK25, locO25 := range nbrs {
			locI45, locJ7 := locCi9+locO25[0], locCj5+locO25[1]
			if locI45 < 0 || locJ7 < 0 || locI45 >= locW12.NNX || locJ7 >= locW12.NNY {
				continue
			}
			locNb6 := locJ7*locW12.NNX + locI45
			locDiag2 := locK25 >= 4
			if locW12.closed[locNb6] == locGen3 || !locW12.canStep(locC50, locNb6, locDiag2) {
				continue
			}
			locStep2 := float32(locW12.NS)
			if locDiag2 {
				if locW12.Blocked[locCj5*locW12.NNX+locI45] || locW12.Blocked[locJ7*locW12.NNX+locCi9] {
					continue
				}
				locStep2 *= 1.4142
			}

			if locCl := locW12.Clearance[locNb6]; locCl < 4 {
				locStep2 *= 1 + (4-locCl)*0.15
			}
			locNg := locW12.g[locC50] + locStep2
			if locW12.stamp[locNb6] != locGen3 || locNg < locW12.g[locNb6] {
				locW12.g[locNb6] = locNg
				locW12.came[locNb6] = int32(locC50)
				locW12.stamp[locNb6] = locGen3
				locW12.push(heapItem{locNg + locH(locNb6), int32(locNb6)})
			}
		}
	}
	for locC11 := locBest; locC11 >= 0; locC11 = int(locW12.came[locC11]) {
		locPath = append(locPath, int32(locC11))
		if locC11 == locS17 {
			break
		}
	}
	for locI39, locJ5 := 0, len(locPath)-1; locI39 < locJ5; locI39, locJ5 = locI39+1, locJ5-1 {
		locPath[locI39], locPath[locJ5] = locPath[locJ5], locPath[locI39]
	}
	return locPath, locFound
}

func (locW57 *World) LineWalkable(locX02, locY06, locX14, locY1 float64) bool {
	locDx2, locDy := locX14-locX02, locY1-locY06
	locD39 := math.Hypot(locDx2, locDy)
	locN3 := int(locD39/1.0) + 1
	locPrev := locW57.Cell(locX02, locY06)
	for locK16 := 1; locK16 <= locN3; locK16++ {
		locT26 := float64(locK16) / float64(locN3)
		locC46 := locW57.Cell(locX02+locDx2*locT26, locY06+locDy*locT26)
		if locC46 == locPrev {
			continue
		}
		if locW57.Blocked[locC46] || locW57.Surf[locC46]-locW57.Surf[locPrev] > 3.1 {
			return false
		}
		locPrev = locC46
	}
	return true
}

func (locW4 *World) CanStand(locX22, locY50, locZ33, locR16 float64) bool {
	if locX22 < locW4.BX0+locR16 || locX22 > locW4.BX1-locR16 || locY50 < locW4.BY0+locR16 || locY50 > locW4.BY1-locR16 {
		return false
	}
	return !locW4.wallWithin(locX22, locY50, locZ33, locR16, 9)
}

func FlatWorld() *World {
	locW9 := &World{MinX: -513, MinY: -334, NX: 1070, NY: 724, BX0: -512, BX1: 555, BY0: -333, BY1: 388}
	locN6 := locW9.NX * locW9.NY
	locW9.Ground = make([]float32, locN6)
	locW9.Top = make([]float32, locN6)
	locW9.Walk = make([]float32, locN6)
	locW9.Obst = make([]bool, locN6)
	locW9.Low = make([]bool, locN6)
	locW9.HasSlab = make([]bool, locN6)
	locW9.Slabs = map[int32][]Slab{}
	locW9.initGeo(nil, nil, Rules{MaxStep: 1.5})
	locW9.buildNav()
	return locW9
}

func (locW39 *World) MainNear(locC4 int) int {
	if locW39.mainNear == nil {
		locN8 := locW39.NNX * locW39.NNY
		locNear2 := make([]int32, locN8)
		locDepth := make([]uint8, locN8)
		locQ5 := make([]int32, 0, locN8)
		for locI19 := range locNear2 {
			locNear2[locI19] = -1
			if !locW39.Blocked[locI19] && locW39.Comp[locI19] == locW39.MainComp {
				locNear2[locI19] = int32(locI19)
				locQ5 = append(locQ5, int32(locI19))
			}
		}
		for locH9 := 0; locH9 < len(locQ5); locH9++ {
			locC3 := int(locQ5[locH9])
			if locDepth[locC3] >= 20 {
				continue
			}
			locCi7, locCj4 := locC3%locW39.NNX, locC3/locW39.NNX
			for _, locO11 := range nbrs {
				locI2, locJ4 := locCi7+locO11[0], locCj4+locO11[1]
				if locI2 < 0 || locJ4 < 0 || locI2 >= locW39.NNX || locJ4 >= locW39.NNY {
					continue
				}
				locNb3 := locJ4*locW39.NNX + locI2
				if locNear2[locNb3] < 0 {
					locNear2[locNb3] = locNear2[locC3]
					locDepth[locNb3] = locDepth[locC3] + 1
					locQ5 = append(locQ5, int32(locNb3))
				}
			}
		}
		locW39.mainNear = locNear2
	}
	return int(locW39.mainNear[locC4])
}

func (locW33 *World) OffMain(locX42, locY44 float64) bool {
	locC13 := locW33.NearestFree(locW33.Cell(locX42, locY44))
	return locC13 < 0 || locW33.Comp[locC13] != locW33.MainComp
}

func (locW37 *World) EscapePlan(locX36, locY6 float64) (locLaunch, locLand [2]float64, locOk5 bool) {
	locS2 := locW37.NearestFree(locW37.Cell(locX36, locY6))
	if locS2 < 0 {
		return
	}
	locW37.gen++
	locGen := locW37.gen
	locQueue2 := []int32{int32(locS2)}
	locW37.stamp[locS2] = locGen
	locW37.g[locS2] = 0
	locBest4, locBestS2 := -1, math.Inf(1)
	for locH18 := 0; locH18 < len(locQueue2) && locH18 < 60000; locH18++ {
		locC36 := int(locQueue2[locH18])
		locCi8, locCj6 := locC36%locW37.NNX, locC36/locW37.NNX
		if locM3 := locW37.MainNear(locC36); locM3 >= 0 {
			locCx5, locCy2 := locW37.CellPos(locC36)
			locMx7, locMy6 := locW37.CellPos(locM3)
			if locSc4 := float64(locW37.g[locC36])*locW37.NS + 4*math.Hypot(locMx7-locCx5, locMy6-locCy2); locSc4 < locBestS2 {
				locBest4, locBestS2 = locC36, locSc4
			}
		}
		for locK8, locO7 := range nbrs {
			locI30, locJ16 := locCi8+locO7[0], locCj6+locO7[1]
			if locI30 < 0 || locJ16 < 0 || locI30 >= locW37.NNX || locJ16 >= locW37.NNY {
				continue
			}
			locNb2 := locJ16*locW37.NNX + locI30
			if locW37.stamp[locNb2] == locGen || !locW37.canStep(locC36, locNb2, locK8 >= 4) {
				continue
			}
			if locK8 >= 4 && (locW37.Blocked[locCj6*locW37.NNX+locI30] || locW37.Blocked[locJ16*locW37.NNX+locCi8]) {
				continue
			}
			locW37.stamp[locNb2] = locGen
			locW37.g[locNb2] = locW37.g[locC36] + 1
			locQueue2 = append(locQueue2, int32(locNb2))
		}
	}
	if locBest4 < 0 {
		return
	}
	locLx, locLy := locW37.CellPos(locBest4)
	locMx8, locMy4 := locW37.CellPos(locW37.MainNear(locBest4))

	if locD35 := math.Hypot(locMx8-locLx, locMy4-locLy); locD35 > 0.1 {
		locEx2, locEy5 := locMx8+(locMx8-locLx)/locD35*5, locMy4+(locMy4-locLy)/locD35*5
		if locW37.InBounds(locEx2, locEy5) && locW37.Reachable(locEx2, locEy5) {
			locMx8, locMy4 = locEx2, locEy5
		}
	}
	return [2]float64{locLx, locLy}, [2]float64{locMx8, locMy4}, true
}

func (locW44 *World) Reachable(locX45, locY31 float64) bool {
	locC24 := locW44.Cell(locX45, locY31)
	return !locW44.Blocked[locC24] && locW44.Comp[locC24] == locW44.MainComp
}

func (locW29 *World) Exposure(locX5, locY23 float64) float64 {
	locZ13 := locW29.WalkAt(locX5, locY23) + 7
	locTotal := 0.0
	for locK11 := 0; locK11 < 24; locK11++ {
		locA2 := float64(locK11) * math.Pi / 12
		locC30, locS19 := math.Cos(locA2), math.Sin(locA2)
		locD26 := 2.5
		for ; locD26 < 200; locD26 += 2.5 {
			locPx12, locPy10 := locX5+locC30*locD26, locY23+locS19*locD26
			if !locW29.InBounds(locPx12, locPy10) || locW29.solidRaster(locPx12, locPy10, locZ13) {
				break
			}
		}
		locTotal += locD26
	}
	return locTotal / (24 * 200)
}

const geoCell = 16.0

type Prism struct {
	Kind                   string
	Base, Top              float64
	Edges                  []float64
	MinX, MinY, MaxX, MaxY float64
}

type Tree struct {
	X, Y, Z, Height, CrownR, CrownBase, TrunkR float64
	CX, CY                                     float64
	TrunkTop, CZ, RZ                           float64
}

type Rules struct {
	MaxStep       float64 `json:"maxStep"`
	MaxSlopeDeg   float64 `json:"maxSlopeDeg"`
	CanopyDensity float64 `json:"canopyDensity"`
	CanopyBlockT  float64 `json:"canopyBlockT"`
}

type geo struct {
	Prisms      []Prism
	Trees       []Tree
	Rules       Rules
	maxSlopeTan float64
	gx0, gy0    float64
	gnx, gny    int
	gridP       [][]int32
	gridT       [][]int32
	bnx, bny    int
	blockMax    []float32
}

const StepUp = 1.2

func pointInPoly(locE5 []float64, locX40, locY28 float64) bool {
	locIn := false
	for locI34 := 0; locI34 < len(locE5); locI34 += 4 {
		locX03, locY08, locX111, locY113 := locE5[locI34], locE5[locI34+1], locE5[locI34+2], locE5[locI34+3]
		if (locY08 > locY28) != (locY113 > locY28) && locX40 < (locX111-locX03)*(locY28-locY08)/(locY113-locY08)+locX03 {
			locIn = !locIn
		}
	}
	return locIn
}

func closestOnEdges(locE2 []float64, locX, locY26 float64) (locCx, locCy6, locD36 float64) {
	locBest8 := math.Inf(1)
	for locI16 := 0; locI16 < len(locE2); locI16 += 4 {
		locX07, locY02 := locE2[locI16], locE2[locI16+1]
		locUx7, locUy2 := locE2[locI16+2]-locX07, locE2[locI16+3]-locY02
		locL22 := locUx7*locUx7 + locUy2*locUy2
		locP12 := 0.0
		if locL22 > 1e-9 {
			locP12 = ((locX-locX07)*locUx7 + (locY26-locY02)*locUy2) / locL22
		}
		locP12 = math.Max(0, math.Min(1, locP12))
		locMx9, locMy10 := locX07+locUx7*locP12, locY02+locUy2*locP12
		if locG4 := (locMx9-locX)*(locMx9-locX) + (locMy10-locY26)*(locMy10-locY26); locG4 < locBest8 {
			locBest8, locCx, locCy6 = locG4, locMx9, locMy10
		}
	}
	return locCx, locCy6, math.Sqrt(locBest8)
}

func (locW14 *World) initGeo(locPrisms2 []Prism, locTrees []Tree, locRules Rules) {
	locG3 := &locW14.geo
	locG3.Prisms, locG3.Trees, locG3.Rules = locPrisms2, locTrees, locRules
	if locG3.Rules.MaxSlopeDeg == 0 {
		locG3.Rules.MaxSlopeDeg = 50
	}
	if locG3.Rules.CanopyDensity == 0 {
		locG3.Rules.CanopyDensity, locG3.Rules.CanopyBlockT = 0.35, 0.25
	}
	locG3.maxSlopeTan = math.Tan(locG3.Rules.MaxSlopeDeg * math.Pi / 180)
	locG3.gx0, locG3.gy0 = locW14.BX0-geoCell, locW14.BY0-geoCell
	locG3.gnx = int(math.Ceil((locW14.BX1-locW14.BX0)/geoCell)) + 3
	locG3.gny = int(math.Ceil((locW14.BY1-locW14.BY0)/geoCell)) + 3
	locG3.gridP = make([][]int32, locG3.gnx*locG3.gny)
	locG3.gridT = make([][]int32, locG3.gnx*locG3.gny)
	for locI26 := range locG3.Prisms {
		locP15 := &locG3.Prisms[locI26]
		locW14.forCells(locP15.MinX, locP15.MinY, locP15.MaxX, locP15.MaxY, func(locC18 int) { locG3.gridP[locC18] = append(locG3.gridP[locC18], int32(locI26)) })
	}
	for locI49 := range locG3.Trees {
		locT4 := &locG3.Trees[locI49]
		locR28 := locT4.CrownR
		locW14.forCells(math.Min(locT4.CX-locR28, locT4.X-locT4.TrunkR), math.Min(locT4.CY-locR28, locT4.Y-locT4.TrunkR),
			math.Max(locT4.CX+locR28, locT4.X+locT4.TrunkR), math.Max(locT4.CY+locR28, locT4.Y+locT4.TrunkR),
			func(locC38 int) { locG3.gridT[locC38] = append(locG3.gridT[locC38], int32(locI49)) })
	}
	locG3.bnx, locG3.bny = (locW14.NX+7)/8, (locW14.NY+7)/8
	locBm := make([]float32, locG3.bnx*locG3.bny)
	for locI4 := range locBm {
		locBm[locI4] = float32(math.Inf(-1))
	}
	for locJ14 := 0; locJ14 < locW14.NY; locJ14++ {
		for locI28 := 0; locI28 < locW14.NX; locI28++ {
			locB42 := (locJ14/8)*locG3.bnx + locI28/8
			if locH2 := locW14.Ground[locJ14*locW14.NX+locI28]; locH2 > locBm[locB42] {
				locBm[locB42] = locH2
			}
		}
	}
	locG3.blockMax = make([]float32, len(locBm))
	for locJ9 := 0; locJ9 < locG3.bny; locJ9++ {
		for locI9 := 0; locI9 < locG3.bnx; locI9++ {
			locV4 := locBm[locJ9*locG3.bnx+locI9]
			if locI9+1 < locG3.bnx {
				locV4 = max(locV4, locBm[locJ9*locG3.bnx+locI9+1])
			}
			if locJ9+1 < locG3.bny {
				locV4 = max(locV4, locBm[(locJ9+1)*locG3.bnx+locI9])
			}
			if locI9+1 < locG3.bnx && locJ9+1 < locG3.bny {
				locV4 = max(locV4, locBm[(locJ9+1)*locG3.bnx+locI9+1])
			}
			locG3.blockMax[locJ9*locG3.bnx+locI9] = locV4
		}
	}
}

func (locW22 *World) cellOf(locX33, locY22 float64) int {
	locG11 := &locW22.geo
	locI29 := int(math.Floor((locX33 - locG11.gx0) / geoCell))
	locJ := int(math.Floor((locY22 - locG11.gy0) / geoCell))
	if locI29 < 0 || locJ < 0 || locI29 >= locG11.gnx || locJ >= locG11.gny {
		return -1
	}
	return locJ*locG11.gnx + locI29
}

func (locW40 *World) forCells(locX0, locY04, locX110, locY12 float64, locF func(int)) {
	locG5 := &locW40.geo
	locI02 := max(0, int(math.Floor((locX0-locG5.gx0)/geoCell)))
	locI15 := min(locG5.gnx-1, int(math.Floor((locX110-locG5.gx0)/geoCell)))
	locJ02 := max(0, int(math.Floor((locY04-locG5.gy0)/geoCell)))
	locJ12 := min(locG5.gny-1, int(math.Floor((locY12-locG5.gy0)/geoCell)))
	for locJ11 := locJ02; locJ11 <= locJ12; locJ11++ {
		for locI5 := locI02; locI5 <= locI15; locI5++ {
			locF(locJ11*locG5.gnx + locI5)
		}
	}
}

func (locW61 *World) walkCells(locX04, locY0, locX1, locY110 float64, locF3 func(int) float64) {
	locW61.walkCellsT(locX04, locY0, locX1, locY110, func(locC9 int, _, _ float64) float64 { return locF3(locC9) })
}

func (locW55 *World) walkCellsT(locX06, locY03, locX18, locY112 float64, locF6 func(locC8 int, locT02, locT12 float64) float64) {
	locG10 := &locW55.geo
	locDx15, locDy6 := locX18-locX06, locY112-locY03
	locCi4 := int(math.Floor((locX06 - locG10.gx0) / geoCell))
	locCj7 := int(math.Floor((locY03 - locG10.gy0) / geoCell))
	locEi := int(math.Floor((locX18 - locG10.gx0) / geoCell))
	locEj := int(math.Floor((locY112 - locG10.gy0) / geoCell))
	locSi, locSj := -1, -1
	if locDx15 > 0 {
		locSi = 1
	}
	if locDy6 > 0 {
		locSj = 1
	}
	locInf := math.Inf(1)
	locTdx, locTdy, locTmx, locTmy := locInf, locInf, locInf, locInf
	if math.Abs(locDx15) > 1e-9 {
		locTdx = geoCell / math.Abs(locDx15)
		if locDx15 > 0 {
			locTmx = (float64(locCi4+1)*geoCell + locG10.gx0 - locX06) / math.Abs(locDx15)
		} else {
			locTmx = (locX06 - (float64(locCi4)*geoCell + locG10.gx0)) / math.Abs(locDx15)
		}
	}
	if math.Abs(locDy6) > 1e-9 {
		locTdy = geoCell / math.Abs(locDy6)
		if locDy6 > 0 {
			locTmy = (float64(locCj7+1)*geoCell + locG10.gy0 - locY03) / math.Abs(locDy6)
		} else {
			locTmy = (locY03 - (float64(locCj7)*geoCell + locG10.gy0)) / math.Abs(locDy6)
		}
	}
	locBest12 := locInf
	locTin := 0.0
	for locK6 := 0; locK6 < 4096; locK6++ {
		if locCi4 >= 0 && locCj7 >= 0 && locCi4 < locG10.gnx && locCj7 < locG10.gny {
			locBest12 = locF6(locCj7*locG10.gnx+locCi4, locTin, math.Min(1, math.Min(locTmx, locTmy)))
		}
		if locCi4 == locEi && locCj7 == locEj {
			return
		}
		var locT53 float64
		if locTmx < locTmy {
			locT53 = locTmx
			locTmx += locTdx
			locCi4 += locSi
		} else {
			locT53 = locTmy
			locTmy += locTdy
			locCj7 += locSj
		}
		if locT53 > 1 || locBest12 < locT53 {
			return
		}
		locTin = locT53
	}
}

func (locW24 *World) GroundGradient(locX49, locY4 float64) (float64, float64) {
	return (locW24.GroundAt(locX49+1, locY4) - locW24.GroundAt(locX49-1, locY4)) / 2, (locW24.GroundAt(locX49, locY4+1) - locW24.GroundAt(locX49, locY4-1)) / 2
}

func (locW *World) SurfaceAt(locX44, locY41, locMaxZ float64) float64 {
	locZ32, _ := locW.SurfaceKind(locX44, locY41, locMaxZ)
	return locZ32
}

func (locW34 *World) SurfaceKind(locX9, locY48, locMaxZ2 float64) (float64, int) {
	locZ2 := locW34.GroundAt(locX9, locY48)
	locId12 := -1
	locC48 := locW34.cellOf(locX9, locY48)
	if locC48 < 0 {
		return locZ2, locId12
	}
	for _, locPi8 := range locW34.geo.gridP[locC48] {
		locP23 := &locW34.geo.Prisms[locPi8]
		if locP23.Top > locMaxZ2 || locP23.Top <= locZ2 || locX9 < locP23.MinX || locX9 > locP23.MaxX || locY48 < locP23.MinY || locY48 > locP23.MaxY || !pointInPoly(locP23.Edges, locX9, locY48) {
			continue
		}
		locZ2, locId12 = locP23.Top, int(locPi8)
	}
	return locZ2, locId12
}

func (locW49 *World) CeilingAbove(locX53, locY2, locZ4, locR23 float64) float64 {
	locBest13 := math.Inf(1)
	locW49.forCells(locX53-locR23, locY2-locR23, locX53+locR23, locY2+locR23, func(locC31 int) {
		for _, locPi9 := range locW49.geo.gridP[locC31] {
			locP4 := &locW49.geo.Prisms[locPi9]
			if locP4.Base < locZ4-0.01 || locP4.Base >= locBest13 || locX53 < locP4.MinX || locX53 > locP4.MaxX || locY2 < locP4.MinY || locY2 > locP4.MaxY {
				continue
			}
			if pointInPoly(locP4.Edges, locX53, locY2) {
				locBest13 = locP4.Base
			}
		}
	})
	return locBest13
}

var TrunkWalls = false

func (locW17 *World) Penetration(locX23, locY34, locZ19, locR13, locH8 float64) (locDepth2, locNx, locNy2 float64, locKind2 string, locId11 int) {
	locKind2, locId11 = "terrain", -1
	locLo, locHi2 := locZ19+StepUp, locZ19+locH8
	locPut := func(locD6, locAx, locAy float64, locK3 string, locI51 int) {
		if locD6 > locDepth2 {
			locDepth2, locNx, locNy2, locKind2, locId11 = locD6, locAx, locAy, locK3, locI51
		}
	}
	locW17.forCells(locX23-locR13, locY34-locR13, locX23+locR13, locY34+locR13, func(locC7 int) {
		for _, locPi5 := range locW17.geo.gridP[locC7] {
			locP17 := &locW17.geo.Prisms[locPi5]
			if locP17.Top <= locLo || locP17.Base >= locHi2 || locX23+locR13 < locP17.MinX || locX23-locR13 > locP17.MaxX || locY34+locR13 < locP17.MinY || locY34-locR13 > locP17.MaxY {
				continue
			}
			locCx3, locCy, locD24 := closestOnEdges(locP17.Edges, locX23, locY34)
			if pointInPoly(locP17.Edges, locX23, locY34) {
				locUx4, locUy5 := locCx3-locX23, locCy-locY34
				locL11 := math.Hypot(locUx4, locUy5)
				if locL11 == 0 {
					locL11 = 1e-9
				}
				locPut(locL11+locR13, locUx4/locL11, locUy5/locL11, locP17.Kind, int(locPi5))
			} else if locD24 < locR13 {
				locL12 := locD24
				if locL12 == 0 {
					locL12 = 1e-9
				}
				locPut(locR13-locD24, (locX23-locCx3)/locL12, (locY34-locCy)/locL12, locP17.Kind, int(locPi5))
			}
		}
		for _, locTi5 := range locW17.geo.gridT[locC7] {
			if !TrunkWalls {
				break
			}
			locT56 := &locW17.geo.Trees[locTi5]
			if locT56.TrunkTop <= locLo || locT56.Z-1 >= locHi2 {
				continue
			}
			locUx6, locUy3 := locX23-locT56.X, locY34-locT56.Y
			locD := math.Hypot(locUx6, locUy3)
			if locF5 := locR13 + locT56.TrunkR; locD < locF5 {
				if locD > 1e-9 {
					locPut(locF5-locD, locUx6/locD, locUy3/locD, "trunk", int(locTi5))
				} else {
					locPut(locF5-locD, 1, 0, "trunk", int(locTi5))
				}
			}
		}
	})
	if locW17.GroundAt(locX23, locY34) > locLo {
		locGx3, locGy := locW17.GroundGradient(locX23, locY34)
		if locN5 := math.Hypot(locGx3, locGy); locN5 > 0.001 {
			locUx, locUy6 := -locGx3/locN5, -locGy/locN5
			locL10 := 0.0
			for locL10 < 2*locR13 && locW17.GroundAt(locX23+locUx*locL10, locY34+locUy6*locL10) > locLo {
				locL10 += 0.25
			}
			locPut(math.Max(locL10, 0.05), locUx, locUy6, "terrain", -1)
		}
	}
	locPut(locW17.BX0+locR13-locX23, 1, 0, "terrain", -2)
	locPut(locX23+locR13-locW17.BX1, -1, 0, "terrain", -2)
	locPut(locW17.BY0+locR13-locY34, 0, 1, "terrain", -2)
	locPut(locY34+locR13-locW17.BY1, 0, -1, "terrain", -2)
	return
}

func (locW6 *World) IsWalkable(locX32, locY14, locR27, locMaxZ3, locH4 float64) bool {
	if locX32-locR27 < locW6.BX0 || locX32+locR27 > locW6.BX1 || locY14-locR27 < locW6.BY0 || locY14+locR27 > locW6.BY1 {
		return false
	}
	if math.IsNaN(locMaxZ3) {
		locMaxZ3 = locW6.GroundAt(locX32, locY14) + StepUp
	}
	locZ16 := locW6.SurfaceAt(locX32, locY14, locMaxZ3)
	if locZ16 <= locW6.GroundAt(locX32, locY14)+0.01 {
		if locGx7, locGy4 := locW6.GroundGradient(locX32, locY14); math.Hypot(locGx7, locGy4) > locW6.geo.maxSlopeTan {
			return false
		}
	}
	locD5, _, _, _, _ := locW6.Penetration(locX32, locY14, locZ16, locR27, locH4)
	return locD5 <= 0.001
}

type MoveResult struct {
	X, Y, Z  float64
	Grounded bool
	StoppedZ bool
	Walls    int
}

func (locW11 *World) MoveAndSlide(locX43, locY7, locZ36, locR15, locH5, locDx11, locDy10, locDz2 float64) MoveResult {
	locOx, locOy, locOz2 := locX43, locY7, locZ36
	locN2 := max(1, int(math.Ceil(math.Hypot(locDx11, locDy10)/(locR15*0.4))))
	locSx3, locSy := locDx11/float64(locN2), locDy10/float64(locN2)
	locMs := StepUp
	locWalls := 0
	var locSeen3 [8]int
	for locK17 := 0; locK17 < locN2; locK17++ {
		locX43 += locSx3
		locY7 += locSy
		for locIt := 0; locIt < 4; locIt++ {
			locD9, locNx3, locNy, _, locId3 := locW11.Penetration(locX43, locY7, locZ36, locR15, locH5)
			if locD9 <= 1e-4 {
				break
			}
			locX43 += locNx3 * (locD9 + 0.001)
			locY7 += locNy * (locD9 + 0.001)
			locDup := false
			for locI53 := 0; locI53 < locWalls && locI53 < len(locSeen3); locI53++ {
				if locSeen3[locI53] == locId3 {
					locDup = true
				}
			}
			if !locDup && locWalls < len(locSeen3) {
				locSeen3[locWalls] = locId3
				locWalls++
			}
		}
		if locS16 := locW11.SurfaceAt(locX43, locY7, locZ36+locMs); locS16 > locZ36 && locDz2 <= 0 {
			locZ36 = locS16
		}
	}
	locRes := MoveResult{X: locX43, Y: locY7, Walls: locWalls}
	locSurf := locW11.SurfaceAt(locX43, locY7, locZ36+locMs)
	locNz := locZ36 + locDz2
	if locDz2 > 0 {
		if locC51 := locW11.CeilingAbove(locX43, locY7, locZ36+locH5, locR15); locNz+locH5 > locC51 {
			locNz = locC51 - locH5
			locRes.StoppedZ = true
		}
	}
	if locNz <= locSurf+1e-4 {
		locNz = locSurf
		locRes.Grounded = true
		locRes.StoppedZ = locRes.StoppedZ || locDz2 < 0
	} else if locDz2 <= 0 && locNz-locSurf <= 0.75 && locOz2-locW11.SurfaceAt(locOx, locOy, locOz2+0.01) < 0.05 {
		locNz = locSurf
		locRes.Grounded = true
	}
	locRes.Z = locNz
	return locRes
}

type RayOpts struct {
	NoTrunks, NoTerrain, Canopy bool
}

type Hit struct {
	T          float64
	X, Y, Z    float64
	NX, NY, NZ float64
	Kind       string
	ID         int
}

func (locW43 *World) rayPrism(locPi10 int, locX10, locY47, locZ28, locDx6, locDy9, locDz8, locTmax2 float64) (Hit, bool) {
	locP6 := &locW43.geo.Prisms[locPi10]
	locT05, locT13 := 0.0, locTmax2
	locSlab := func(locO4, locD21, locLo3, locHi4 float64) bool {
		if math.Abs(locD21) < 1e-9 {
			return locO4 >= locLo3 && locO4 <= locHi4
		}
		locA16, locB56 := (locLo3-locO4)/locD21, (locHi4-locO4)/locD21
		if locA16 > locB56 {
			locA16, locB56 = locB56, locA16
		}
		locT05, locT13 = math.Max(locT05, locA16), math.Min(locT13, locB56)
		return locT05 <= locT13
	}
	if !locSlab(locX10, locDx6, locP6.MinX, locP6.MaxX) || !locSlab(locY47, locDy9, locP6.MinY, locP6.MaxY) || !locSlab(locZ28, locDz8, locP6.Base, locP6.Top) {
		return Hit{}, false
	}
	if locZ28 >= locP6.Base && locZ28 <= locP6.Top && pointInPoly(locP6.Edges, locX10, locY47) {
		locL2 := math.Sqrt(locDx6*locDx6+locDy9*locDy9+locDz8*locDz8) + 1e-300
		return Hit{T: 0, X: locX10, Y: locY47, Z: locZ28, NX: -locDx6 / locL2, NY: -locDy9 / locL2, NZ: -locDz8 / locL2, Kind: locP6.Kind, ID: locPi10}, true
	}
	var locH3 Hit
	locOk8 := false
	locBest7 := locTmax2
	locE := locP6.Edges
	for locI8 := 0; locI8 < len(locE); locI8 += 4 {
		locEx4, locEy7 := locE[locI8+2]-locE[locI8], locE[locI8+3]-locE[locI8+1]
		locDen := locDx6*locEy7 - locDy9*locEx4
		if math.Abs(locDen) < 1e-9 {
			continue
		}
		locPx13, locPy8 := locE[locI8]-locX10, locE[locI8+1]-locY47
		locT25 := (locPx13*locEy7 - locPy8*locEx4) / locDen
		locS4 := (locPx13*locDy9 - locPy8*locDx6) / locDen
		if locT25 < 0 || locT25 >= locBest7 || locS4 < 0 || locS4 > 1 {
			continue
		}
		locHz2 := locZ28 + locDz8*locT25
		if locHz2 < locP6.Base || locHz2 > locP6.Top {
			continue
		}
		locL5 := math.Hypot(locEx4, locEy7)
		locBest7 = locT25
		locH3 = Hit{T: locT25, X: locX10 + locDx6*locT25, Y: locY47 + locDy9*locT25, Z: locHz2, NX: locEy7 / locL5, NY: -locEx4 / locL5, Kind: locP6.Kind, ID: locPi10}
		locOk8 = true
	}
	if math.Abs(locDz8) > 1e-9 {
		for _, locCap := range [2]struct{ z, n float64 }{{locP6.Top, 1}, {locP6.Base, -1}} {
			locT30 := (locCap.z - locZ28) / locDz8
			if locT30 < 0 || locT30 >= locBest7 || (locCap.n > 0 && locDz8 > 0) || (locCap.n < 0 && locDz8 < 0) {
				continue
			}
			locHx, locHy := locX10+locDx6*locT30, locY47+locDy9*locT30
			if pointInPoly(locE, locHx, locHy) {
				locBest7 = locT30
				locH3 = Hit{T: locT30, X: locHx, Y: locHy, Z: locCap.z, NZ: locCap.n, Kind: locP6.Kind, ID: locPi10}
				locOk8 = true
			}
		}
	}
	return locH3, locOk8
}

func rayTrunk(locCx7, locCy4, locR5, locZ02, locZ1, locX26, locY18, locZ6, locDx8, locDy5, locDz6, locTmax3 float64) (float64, bool) {
	locPx3, locPy15 := locX26-locCx7, locY18-locCy4
	locA21 := locDx8*locDx8 + locDy5*locDy5
	if locA21 < 1e-9 {
		return 0, false
	}
	locB30 := 2 * (locPx3*locDx8 + locPy15*locDy5)
	locC34 := locPx3*locPx3 + locPy15*locPy15 - locR5*locR5
	locDisc2 := locB30*locB30 - 4*locA21*locC34
	if locDisc2 < 0 {
		return 0, false
	}
	locT38 := (-locB30 - math.Sqrt(locDisc2)) / (2 * locA21)
	if locT38 < 0 || locT38 >= locTmax3 {
		return 0, false
	}
	if locHz := locZ6 + locDz6*locT38; locHz < locZ02 || locHz > locZ1 {
		return 0, false
	}
	return locT38, true
}

func rayCrown(locTr3 *Tree, locX48, locY, locZ35, locDx20, locDy7, locDz7 float64) (float64, float64, bool) {
	locOx2, locOy2, locOz := (locX48-locTr3.CX)/locTr3.CrownR, (locY-locTr3.CY)/locTr3.CrownR, (locZ35-locTr3.CZ)/locTr3.RZ
	locUx2, locUy, locUz := locDx20/locTr3.CrownR, locDy7/locTr3.CrownR, locDz7/locTr3.RZ
	locA4 := locUx2*locUx2 + locUy*locUy + locUz*locUz
	if locA4 < 1e-9 {
		return 0, 0, false
	}
	locB45 := 2 * (locOx2*locUx2 + locOy2*locUy + locOz*locUz)
	locC28 := locOx2*locOx2 + locOy2*locOy2 + locOz*locOz - 1
	locDisc := locB45*locB45 - 4*locA4*locC28
	if locDisc <= 0 {
		return 0, 0, false
	}
	locS7 := math.Sqrt(locDisc)
	locT03, locT1 := (-locB45-locS7)/(2*locA4), (-locB45+locS7)/(2*locA4)
	if locT1 < 0 || locT03 > 1 {
		return 0, 0, false
	}
	return locT03, locT1, true
}

func (locW36 *World) rayTerrain(locX41, locY53, locZ3, locX12, locY111, locZ110, locTmax4 float64) (Hit, bool) {
	locDx3, locDy8, locDz9 := locX12-locX41, locY111-locY53, locZ110-locZ3
	locHl := math.Hypot(locDx3, locDy8)
	const blk = 8.0
	locAbove := func(locT40 float64) float64 {
		return locZ3 + locDz9*locT40 - locW36.GroundAt(locX41+locDx3*locT40, locY53+locDy8*locT40)
	}
	if locAbove(0) < 0 {
		return Hit{T: 0, X: locX41, Y: locY53, Z: locZ3, NZ: 1, Kind: "terrain", ID: -1}, true
	}
	locStep := 1.0
	if locHl > 1e-9 {
		locStep = 0.5 / locHl
	}
	locG := &locW36.geo
	locT49 := 0.0
	for locIt4 := 0; locT49 < locTmax4; locIt4++ {
		if locIt4 > 100000 {
			return Hit{}, false
		}
		locPx4, locPy12 := locX41+locDx3*locT49, locY53+locDy8*locT49
		locBi := int(math.Floor((locPx4 - locW36.MinX) / blk))
		locBj := int(math.Floor((locPy12 - locW36.MinY) / blk))
		locInb := locBi >= 0 && locBj >= 0 && locBi < locG.bnx && locBj < locG.bny
		locTb2 := locTmax4
		if locHl > 1e-9 {
			locEx3, locEy3 := locW36.MinX+float64(locBi)*blk, locW36.MinY+float64(locBj)*blk
			locNx2, locNy3 := math.Inf(1), math.Inf(1)
			if locDx3 > 1e-9 {
				locNx2 = (locEx3 + blk - locPx4) / locDx3
			} else if locDx3 < -1e-9 {
				locNx2 = (locEx3 - locPx4) / locDx3
			}
			if locDy8 > 1e-9 {
				locNy3 = (locEy3 + blk - locPy12) / locDy8
			} else if locDy8 < -1e-9 {
				locNy3 = (locEy3 - locPy12) / locDy8
			}
			locTb2 = math.Min(locT49+math.Max(0, math.Min(locNx2, locNy3))+1e-6, locTmax4)
		}
		if locInb && math.Min(locZ3+locDz9*locT49, locZ3+locDz9*locTb2) > float64(locG.blockMax[locBj*locG.bnx+locBi]) {
			locT49 = locTb2
			continue
		}
		locS15 := math.Min(locT49+locStep, locTmax4)
		if locAbove(locS15) < 0 {
			locLo5, locHi3 := locT49, locS15
			for locK13 := 0; locK13 < 20; locK13++ {
				locM5 := (locLo5 + locHi3) / 2
				if locAbove(locM5) < 0 {
					locHi3 = locM5
				} else {
					locLo5 = locM5
				}
			}
			locHx3, locHy4 := locX41+locDx3*locHi3, locY53+locDy8*locHi3
			locGx, locGy3 := locW36.GroundGradient(locHx3, locHy4)
			locL4 := math.Sqrt(locGx*locGx + locGy3*locGy3 + 1)
			return Hit{T: locHi3, X: locHx3, Y: locHy4, Z: locW36.GroundAt(locHx3, locHy4), NX: -locGx / locL4, NY: -locGy3 / locL4, NZ: 1 / locL4, Kind: "terrain", ID: -1}, true
		}
		if locS15 >= locTmax4 {
			break
		}
		locT49 = locS15
	}
	return Hit{}, false
}

func (locW15 *World) Raycast(locA27, locB36 Vec3, locO18 RayOpts) (Hit, bool) {
	locX2, locY11, locZ27 := locA27[0], locA27[1], locA27[2]
	locDx14, locDy19, locDz3 := locB36[0]-locX2, locB36[1]-locY11, locB36[2]-locZ27
	var locBest11 Hit
	locFound3 := false
	locTmax := func() float64 {
		if locFound3 {
			return locBest11.T
		}
		return 1
	}
	var locSeenP, locSeenT []int32
	var locBufP, locBufT [64]int32
	locSeenP, locSeenT = locBufP[:0], locBufT[:0]
	locHas := func(locS14 []int32, locV8 int32) bool {
		for _, locU3 := range locS14 {
			if locU3 == locV8 {
				return true
			}
		}
		return false
	}
	locW15.walkCells(locX2, locY11, locB36[0], locB36[1], func(locC49 int) float64 {
		for _, locPi3 := range locW15.geo.gridP[locC49] {
			if locHas(locSeenP, locPi3) {
				continue
			}
			locSeenP = append(locSeenP, locPi3)
			if locH10, locOk4 := locW15.rayPrism(int(locPi3), locX2, locY11, locZ27, locDx14, locDy19, locDz3, locTmax()); locOk4 {
				locBest11, locFound3 = locH10, true
			}
		}
		for _, locTi3 := range locW15.geo.gridT[locC49] {
			if locHas(locSeenT, locTi3) {
				continue
			}
			locSeenT = append(locSeenT, locTi3)
			locTr4 := &locW15.geo.Trees[locTi3]
			if !locO18.NoTrunks {
				if locT39, locOk7 := rayTrunk(locTr4.X, locTr4.Y, locTr4.TrunkR, locTr4.Z-1, locTr4.TrunkTop, locX2, locY11, locZ27, locDx14, locDy19, locDz3, locTmax()); locOk7 {
					locBest11 = Hit{T: locT39, X: locX2 + locDx14*locT39, Y: locY11 + locDy19*locT39, Z: locZ27 + locDz3*locT39, Kind: "trunk", ID: int(locTi3)}
					locFound3 = true
				}
			}
			if locO18.Canopy {
				if locT06, _, locOk13 := rayCrown(locTr4, locX2, locY11, locZ27, locDx14, locDy19, locDz3); locOk13 && locT06 < locTmax() {
					locT54 := math.Max(locT06, 0)
					locBest11 = Hit{T: locT54, X: locX2 + locDx14*locT54, Y: locY11 + locDy19*locT54, Z: locZ27 + locDz3*locT54, Kind: "canopy", ID: int(locTi3)}
					locFound3 = true
				}
			}
		}
		return locTmax()
	})
	if !locO18.NoTerrain {
		if locH11, locOk16 := locW15.rayTerrain(locX2, locY11, locZ27, locB36[0], locB36[1], locB36[2], locTmax()); locOk16 {
			locBest11, locFound3 = locH11, true
		}
	}
	return locBest11, locFound3
}

func (locW19 *World) CanopyTransmittance(locA13, locB25 Vec3) float64 {
	return math.Exp(-locW19.geo.Rules.CanopyDensity * locW19.crownMeters(locA13, locB25, math.Inf(1)))
}

func (locW18 *World) crownMeters(locA23, locB2 Vec3, locLimit float64) float64 {
	locX3, locY29, locZ20 := locA23[0], locA23[1], locA23[2]
	locDx, locDy11, locDz4 := locB2[0]-locX3, locB2[1]-locY29, locB2[2]-locZ20
	locL9 := math.Sqrt(locDx*locDx + locDy11*locDy11 + locDz4*locDz4)
	locSum := 0.0
	locW18.walkCellsT(locX3, locY29, locB2[0], locB2[1], func(locC21 int, locC02, locC1 float64) float64 {
		for _, locTi2 := range locW18.geo.gridT[locC21] {
			if locT04, locT110, locOk10 := rayCrown(&locW18.geo.Trees[locTi2], locX3, locY29, locZ20, locDx, locDy11, locDz4); locOk10 {
				if locT04, locT110 = math.Max(locT04, locC02), math.Min(locT110, locC1); locT110 > locT04 {
					locSum += (locT110 - locT04) * locL9
				}
			}
		}
		if locSum > locLimit {
			return -1
		}
		return 1
	})
	return locSum
}

func (locW20 *World) LineOfSight(locA25, locB50 Vec3) bool {
	if locW20.anyHit(locA25, locB50, true) {
		return false
	}

	locLim4 := -math.Log(locW20.geo.Rules.CanopyBlockT) / locW20.geo.Rules.CanopyDensity
	return locW20.crownMeters(locA25, locB50, locLim4) <= locLim4
}

func (locW42 *World) ShotClear(locA20, locB27 Vec3) bool {
	return !locW42.anyHit(locA20, locB27, false)
}

func (locW47 *World) anyHit(locA15, locB51 Vec3, locTrunks bool) bool {
	locX55, locY40, locZ23 := locA15[0], locA15[1], locA15[2]
	locDx13, locDy15, locDz5 := locB51[0]-locX55, locB51[1]-locY40, locB51[2]-locZ23
	var locBufP2, locBufT2 [64]int32
	locSeenP2, locSeenT2 := locBufP2[:0], locBufT2[:0]
	locHit := false
	locW47.walkCells(locX55, locY40, locB51[0], locB51[1], func(locC15 int) float64 {
	nextP:
		for _, locPi4 := range locW47.geo.gridP[locC15] {
			for _, locU4 := range locSeenP2 {
				if locU4 == locPi4 {
					continue nextP
				}
			}
			if len(locSeenP2) < cap(locSeenP2) {
				locSeenP2 = append(locSeenP2, locPi4)
			}
			if _, locOk := locW47.rayPrism(int(locPi4), locX55, locY40, locZ23, locDx13, locDy15, locDz5, 1); locOk {
				locHit = true
				return -1
			}
		}
		if locTrunks {
		nextT:
			for _, locTi4 := range locW47.geo.gridT[locC15] {
				for _, locU6 := range locSeenT2 {
					if locU6 == locTi4 {
						continue nextT
					}
				}
				if len(locSeenT2) < cap(locSeenT2) {
					locSeenT2 = append(locSeenT2, locTi4)
				}
				locTr2 := &locW47.geo.Trees[locTi4]
				if _, locOk3 := rayTrunk(locTr2.X, locTr2.Y, locTr2.TrunkR, locTr2.Z-1, locTr2.TrunkTop, locX55, locY40, locZ23, locDx13, locDy15, locDz5, 1); locOk3 {
					locHit = true
					return -1
				}
			}
		}
		return 1
	})
	if locHit {
		return true
	}
	_, locOk2 := locW47.rayTerrain(locX55, locY40, locZ23, locB51[0], locB51[1], locB51[2], 1)
	return locOk2
}

func (locW51 *World) InCanopy(locX24, locY13, locZ7 float64) int {
	locC37 := locW51.cellOf(locX24, locY13)
	if locC37 < 0 {
		return -1
	}
	for _, locTi := range locW51.geo.gridT[locC37] {
		locT43 := &locW51.geo.Trees[locTi]
		locA26, locB9, locD8 := (locX24-locT43.CX)/locT43.CrownR, (locY13-locT43.CY)/locT43.CrownR, (locZ7-locT43.CZ)/locT43.RZ
		if locA26*locA26+locB9*locB9+locD8*locD8 <= 1 {
			return int(locTi)
		}
	}
	return -1
}

func (locW8 *World) SupportAt(locX21, locY17, locFeetZ, locR4 float64) float64 {
	locZ0 := locW8.SurfaceAt(locX21, locY17, locFeetZ+StepUp)
	locZ14 := locZ0
	locLim5 := locFeetZ + 0.05
	locRe := locR4 + 0.02
	locW8.forCells(locX21-locRe, locY17-locRe, locX21+locRe, locY17+locRe, func(locC12 int) {
		for _, locPi6 := range locW8.geo.gridP[locC12] {
			locP21 := &locW8.geo.Prisms[locPi6]

			if locP21.Top <= locZ14 || locP21.Top > locLim5 || locP21.Top <= locZ0+StepUp || locX21+locRe < locP21.MinX || locX21-locRe > locP21.MaxX || locY17+locRe < locP21.MinY || locY17-locRe > locP21.MaxY {
				continue
			}
			if _, _, locD11 := closestOnEdges(locP21.Edges, locX21, locY17); locD11 <= locRe || pointInPoly(locP21.Edges, locX21, locY17) {
				locZ14 = locP21.Top
			}
		}
	})
	return locZ14
}
