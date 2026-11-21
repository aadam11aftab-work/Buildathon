package mech

import (
	"math"
	"math/rand"
	"sort"
)

const lat = 0.075

type Track struct {
	MechSeen
	Seen    float64
	HitMe   float64
	Dist    float64
	LOS     bool
	Closing float64
	Engage  bool
}

type known struct {
	ID   int
	Kind string
	Pos  Vec3
	HP   float64
	Seen float64
}

type circle struct{ X, Y, R float64 }

type Brain struct {
	W    *World
	Cfg  Config
	Slot int
	rng  *rand.Rand
	P    Params

	tracks  map[int]*Track
	npcs    map[int]*Track
	crates  map[int]*known
	cubes   map[int]*known
	pickups map[int]*known
	bad     map[int]float64

	now float64

	path     []int32
	pathGoal [2]float64
	pathAt   float64
	pathI    int

	strafe      float64
	strafeUntil float64
	boostLow    bool // tweakBoostHyst: energy fell under 30, no extra boosts until it is back over 60

	retreat   bool
	coverAt   float64
	coverGoal [2]float64
	coverOK   bool

	lastHurt    float64
	hurtBearing float64
	// tweakHear: last unexplained enemy sound (time, bearing) and the side-step against an unseen laser charge
	heardAt, heardBearing                  float64
	hearDodgeUntil, hearDodgeX, hearDodgeY float64
	hurtSeenBy                             bool

	lootID    int
	lootSince float64

	hold      circle
	holdStart float64

	homeXY       [2]float64
	homeAt       float64
	homeSet      bool
	explore      [2]float64
	exploreUntil float64

	stuckSince   float64
	unstuckUntil float64
	unstuckDir   [2]float64
	lastThreatAt float64
	lastAim      [2]float64
	laserHeld    float64
	Mode         string
	Panics       int

	escLaunch, escLand [2]float64
	escAt              float64
	escOK, escFlying   bool
	oddsFleeUntil      float64
	base               Params
	baseSet, pracOn    bool
	openOn             bool
	npcHurt            float64
	npcBearing         float64
	notPractice        bool
	otherSince         float64 // tweakFFAUntil: time the current run of deaths we did not cause was first seen
	startAlive, kills  int
	otherDead          int
	stickID            int
	stickAt, loseAt    float64
	prevT              float64
}

type Params struct {
	RetreatHP  float64
	RecoverHP  float64
	GunRange   float64
	LightRange float64
	LaserRange float64
	EngageDist float64
	FarmNPC    bool
	Explore    bool
	Ghost      bool
	HomeExpo   float64
	LootRadius float64
	CrateUntil float64
	UseDrops   bool
	Dodge      bool
	NoMortar   bool
	Peek       bool
	LaserFirst bool
	CrowdHide  bool
	Vulture    float64
	Ambush     float64
	LaserSeek  float64
	Lethal     bool
	Alert      float64
	Shy        float64
	EarlyHide  float64
	Evade      bool
	Odds       float64
	OddsFlee   float64
	Practice   bool
	Stick      float64
	HPw        float64
	LowBonus   float64
	MaxTarget  float64
	LoseRule   bool
	FightLoot  float64
	FarmHP     float64
	AllFoes    bool
	CubeGoal   float64
	Econ       bool
	LaserUp    bool
	EconSafe   float64
	Opening    bool
	NanoUp     bool
	Finish     float64
	FinishHP   float64
	FinishRush bool
	Pursue     float64
}

func DefaultParams() Params {
	locP8 := Params{RetreatHP: 0.38, RecoverHP: 0.8, GunRange: 85, LightRange: 28, LaserRange: 85, EngageDist: 60, FarmNPC: true, Explore: false,
		Ghost: true, HomeExpo: 120, LootRadius: 90, CrateUntil: 150, UseDrops: false, Dodge: true, NoMortar: true,
		Peek: true, LaserFirst: false, CrowdHide: true, Practice: true}

	locP8.LaserFirst = true

	return locP8
}

func NewBrain(locH13 *Hello, locW62 *World) *Brain {
	locSeed := int64(locH13.Seed)
	if locSeed == 0 {
		locSeed = int64(locH13.Slot) + 1
	}
	locSeed += seedSalt * 7919
	return &Brain{
		W: locW62, Cfg: locH13.Config, Slot: locH13.Slot, rng: rand.New(rand.NewSource(locSeed)), P: DefaultParams(),
		tracks: map[int]*Track{}, npcs: map[int]*Track{}, crates: map[int]*known{}, cubes: map[int]*known{},
		pickups: map[int]*known{}, bad: map[int]float64{}, strafe: 1, lootID: -1, lastHurt: -100, lastThreatAt: -100,
		holdStart: -1,
	}
}

func hyp(locX47, locY37 float64) float64 { return math.Hypot(locX47, locY37) }

func angDiff(locA19, locB18 float64) float64 {
	locD16 := math.Mod(locA19-locB18, 2*math.Pi)
	if locD16 > math.Pi {
		locD16 -= 2 * math.Pi
	} else if locD16 < -math.Pi {
		locD16 += 2 * math.Pi
	}
	return locD16
}

func clamp(locV2, locLo4, locHi float64) float64 { return math.Max(locLo4, math.Min(locHi, locV2)) }

func (locB46 *Brain) Act(locO16 *Obs) (locAct2 Action) {
	locAct2.Tick = locO16.Tick
	defer func() {
		if locR10 := recover(); locR10 != nil {
			locB46.Panics++
			locA24 := locB46.lastAim
			locAct2 = Action{Tick: locO16.Tick, Aim: &locA24}
		}
	}()
	locB46.step(locO16, &locAct2)
	if locAct2.Aim != nil {
		locA3 := locAct2.Aim
		if math.IsNaN(locA3[0]) || math.IsInf(locA3[0], 0) || math.IsNaN(locA3[1]) || math.IsInf(locA3[1], 0) {
			locAct2.Aim = nil
		} else {
			locB46.lastAim = *locA3
		}
	}
	if math.IsNaN(locAct2.Move[0]) || math.IsNaN(locAct2.Move[1]) || math.IsInf(locAct2.Move[0], 0) || math.IsInf(locAct2.Move[1], 0) {
		locAct2.Move = [2]float64{}
	}
	return
}

func (locB6 *Brain) scaleOf(locCubes int) float64 {
	return 1 + locB6.Cfg.Cubes.SizePerCube*float64(locCubes)
}

func (locB21 *Brain) center(locT7 *Track) Vec3 {
	locH6 := locB21.Cfg.Mech.Height * locB21.scaleOf(locT7.Cubes)
	if locT7.Kind == "npc" {
		locH6 = locB21.Cfg.Npc.Height
	}
	return Vec3{locT7.Pos[0], locT7.Pos[1], locT7.Pos[2] + locH6*0.5}
}

func (locB20 *Brain) eye(locT17 *Track) Vec3 {
	return Vec3{locT17.Pos[0], locT17.Pos[1], locT17.Pos[2] + locB20.Cfg.Mech.EyeHeight*locB20.scaleOf(locT17.Cubes)}
}

func (locB48 *Brain) observe(locO13 *Obs) {
	locMe16 := &locO13.Self
	locNow := locO13.T
	if locMe16.Stats.Radius == 0 {
		locMe16.Stats = locB48.Cfg.Mech
	}
	locEye := Vec3{locMe16.Pos[0], locMe16.Pos[1], locMe16.Pos[2] + locMe16.Stats.EyeHeight}
	locMuzzle2 := Vec3{locMe16.Pos[0], locMe16.Pos[1], locMe16.Pos[2] + locMe16.Stats.MuzzleHeight}
	for locI25 := range locO13.Mechs {
		locM6 := &locO13.Mechs[locI25]
		locTab := locB48.tracks
		if locM6.Kind == "npc" {
			locTab = locB48.npcs
		}
		locT52 := locTab[locM6.ID]
		if locT52 == nil {
			locT52 = &Track{HitMe: -100}
			locTab[locM6.ID] = locT52
		}
		locT52.MechSeen = *locM6
		locT52.Seen = locNow
		locDx7, locDy14 := locM6.Pos[0]-locMe16.Pos[0], locM6.Pos[1]-locMe16.Pos[1]
		locT52.Dist = hyp(locDx7, locDy14)
		locC10 := locB48.center(locT52)
		locT52.LOS = locB48.W.Clear(locMuzzle2, locC10)
		if !locT52.LOS && locM6.Kind == "mech" {
			locH12 := locB48.Cfg.Mech.Height * locB48.scaleOf(locM6.Cubes)
			locT52.LOS = locB48.W.Clear(locMuzzle2, Vec3{locM6.Pos[0], locM6.Pos[1], locM6.Pos[2] + locH12*0.85})
		}
		if locT52.Dist > 1 {
			locT52.Closing = -((locM6.Vel[0]-locMe16.Vel[0])*locDx7 + (locM6.Vel[1]-locMe16.Vel[1])*locDy14) / locT52.Dist
		}
	}
	for locId4, locT21 := range locB48.tracks {
		if locNow-locT21.Seen > 20 {
			delete(locB48.tracks, locId4)
		}
	}
	for locId10, locT51 := range locB48.npcs {
		if locNow-locT51.Seen > 120 {
			delete(locB48.npcs, locId10)
		}
		// a drone listed last time but not now, and nearly dead, was killed (a living one stays listed for memory_s)
		if tweakNpcGoneHP > 0 && locT51.Seen != locNow && locT51.Seen == locB48.prevT && locT51.HP <= tweakNpcGoneHP*math.Max(1, locT51.MaxHP) {
			delete(locB48.npcs, locId10)
		}
	}
	locB48.prevT = locNow
	locUpd := func(locTab2 map[int]*known, locItems []Item) {
		for locI40 := range locItems {
			locIt5 := &locItems[locI40]
			locK22 := locTab2[locIt5.ID]
			if locK22 == nil {
				locK22 = &known{ID: locIt5.ID}
				locTab2[locIt5.ID] = locK22
			}
			locK22.Kind, locK22.Pos, locK22.HP, locK22.Seen = locIt5.Kind, locIt5.Pos, locIt5.HP, locNow
		}
		for locId2, locK15 := range locTab2 {
			if locK15.Seen == locNow {
				continue
			}
			locD30 := hyp(locK15.Pos[0]-locMe16.Pos[0], locK15.Pos[1]-locMe16.Pos[1])
			if locD30 < 40 && locNow-locK15.Seen > 0.35 && locB48.W.Clear(locEye, Vec3{locK15.Pos[0], locK15.Pos[1], locK15.Pos[2] + 1.5}) {
				delete(locTab2, locId2)
			}
		}
	}
	locUpd(locB48.crates, locO13.Crates)
	locUpd(locB48.cubes, locO13.Cubes)
	locUpd(locB48.pickups, locO13.Pickups)
	for locI7 := range locO13.Events {
		locE7 := &locO13.Events[locI7]
		switch locE7.Type {
		case "damaged":
			if locE7.Weapon == "npc" && locE7.Bearing != nil {
				locB48.npcHurt, locB48.npcBearing = locNow, *locE7.Bearing
			}
			if locE7.By != nil {
				locB48.lastHurt = locNow
				if locE7.Bearing != nil {
					locB48.hurtBearing = *locE7.Bearing
				}
				if locT3 := locB48.tracks[*locE7.By]; locT3 != nil {
					locT3.HitMe = locNow
				}
				if locN11 := locB48.npcs[*locE7.By]; locN11 != nil {
					locN11.HitMe = locNow
				}
			}
		case "kill":
			locB48.kills++
			delete(locB48.tracks, locE7.Target)
		}
	}

	if !locB48.baseSet {
		locB48.base, locB48.baseSet = locB48.P, true
		locB48.startAlive = locO13.Alive
	}
	if locB48.startAlive-locO13.Alive-locB48.kills > 0 {
		if locB48.otherDead == 0 {
			locB48.otherSince = locNow
		}
		locB48.otherDead++
	} else {
		locB48.otherDead = 0
	}
	if locB48.otherDead >= 5 && (tweakFFAUntil <= 0 || locB48.otherSince < tweakFFAUntil) {
		locB48.notPractice = true
	}
	if locOn := locB48.base.Practice && !locB48.notPractice && locNow > tw.squadT; locOn != locB48.pracOn {
		locB48.pracOn = locOn
		locB48.P = locB48.base
		if locOn {
			locB48.P = squadParams(locB48.base)
		}
	}
	if locOpen := locB48.base.Opening && !locB48.notPractice && !locB48.pracOn && locB48.startAlive-locO13.Alive-locB48.kills <= 0; locOpen != locB48.openOn {
		locB48.openOn = locOpen
		locB48.P = locB48.base
		if locOpen {
			locB48.P.LaserFirst, locB48.P.LaserSeek, locB48.P.LootRadius = true, 240, 150
		} else if locB48.pracOn {
			locB48.P = squadParams(locB48.base)
		}
	}

	if tweakHear && locB48.pracOn {
		locB48.hear(locO13)
	}

	locZ10 := &locO13.Zone
	if locB48.holdStart != locZ10.ShrinkStart {
		locB48.holdStart = locZ10.ShrinkStart
		locB48.hold = circle{locZ10.Center[0], locZ10.Center[1], locZ10.Radius}
	} else if locNow <= locZ10.ShrinkStart {
		locB48.hold = circle{locZ10.Center[0], locZ10.Center[1], locZ10.Radius}
	}
}

func (locB38 *Brain) zoneAt(locZ25 *Zone, locT32 float64) circle {
	if locT32 <= locZ25.ShrinkStart || locZ25.ShrinkEnd <= locZ25.ShrinkStart {
		return locB38.hold
	}
	locF7 := clamp((locT32-locZ25.ShrinkStart)/(locZ25.ShrinkEnd-locZ25.ShrinkStart), 0, 1)
	return circle{locB38.hold.X + (locZ25.NextCenter[0]-locB38.hold.X)*locF7, locB38.hold.Y + (locZ25.NextCenter[1]-locB38.hold.Y)*locF7, locB38.hold.R + (locZ25.NextRadius-locB38.hold.R)*locF7}
}

func (locB15 *Brain) curZone(locZ18 *Zone) circle {
	locLive := circle{locZ18.Center[0], locZ18.Center[1], locZ18.Radius}
	if locB15.now <= locZ18.ShrinkStart {
		return locLive
	}
	if math.Abs(locLive.R-locB15.hold.R) > 0.01 || math.Abs(locLive.X-locB15.hold.X) > 0.01 {
		return locLive
	}
	return locB15.zoneAt(locZ18, locB15.now)
}

func (locB19 *Brain) zoneDeadline(locZ *Zone, locX46, locY5, locPad2 float64) float64 {
	if hyp(locX46-locZ.NextCenter[0], locY5-locZ.NextCenter[1]) <= locZ.NextRadius-locPad2 {
		return math.Inf(1)
	}
	for locK21 := 0; locK21 <= 24; locK21++ {
		locT44 := locZ.ShrinkStart + (locZ.ShrinkEnd-locZ.ShrinkStart)*float64(locK21)/24
		locC14 := locB19.zoneAt(locZ, locT44)
		if hyp(locX46-locC14.X, locY5-locC14.Y) > locC14.R-locPad2 {
			return locT44
		}
	}
	return locZ.ShrinkEnd
}

func (locB3 *Brain) zonePenalty(locZ37 *Zone, locX19, locY27 float64) float64 {
	locC41 := locB3.curZone(locZ37)
	locP7 := 0.0
	if locD38 := hyp(locX19-locC41.X, locY27-locC41.Y) - locC41.R; locD38 > -5 {
		locP7 += 150 + 5*(locD38+5)
	}
	if locDn := hyp(locX19-locZ37.NextCenter[0], locY27-locZ37.NextCenter[1]) - locZ37.NextRadius; locDn > -5 {
		locSoon := clamp(1-(locZ37.ShrinkStart-locB3.now)/25, 0.1, 1)
		locP7 += locSoon * (25 + 0.8*(locDn+5))
	}
	return locP7
}

func (locB28 *Brain) zoneGoal(locZ17 *Zone, locX7, locY20 float64) (float64, float64) {
	locMargin := clamp(0.3*locZ17.NextRadius, 3, 30)
	if locZ17.NextRadius <= 30 {
		return locZ17.NextCenter[0], locZ17.NextCenter[1]
	}
	locDx17, locDy18 := locX7-locZ17.NextCenter[0], locY20-locZ17.NextCenter[1]
	locD7 := hyp(locDx17, locDy18)
	if locD7 < 1 {
		return locX7, locY20
	}
	locR12 := math.Max(0, locZ17.NextRadius-locMargin-5)
	if locD7 <= locR12 {
		return locX7, locY20
	}
	return locZ17.NextCenter[0] + locDx17/locD7*locR12, locZ17.NextCenter[1] + locDy18/locD7*locR12
}

func hasWeapon(locWs []*WeaponState, locKind string) (int, *WeaponState) {
	for locI11, locW2 := range locWs {
		if locW2 != nil && locW2.Kind == locKind {
			return locI11, locW2
		}
	}
	return -1, nil
}

func holds(locList []string, locKind6 string) bool {
	for _, locK10 := range locList {
		if locK10 == locKind6 {
			return true
		}
	}
	return false
}

var upgradeOrder = []string{
	"plating", "armor", "gun_damage", "nanites", "gun_rate", "lightning_efficiency", "lightning_range", "laser_damage",
	"servos", "laser_charge", "gun_mag", "gun_velocity", "gyros", "lightning_chain", "reactor", "capacitor",
	"mortar_reload", "mortar_splash", "mortar_velocity", "sensors", "jets", "laser_pierce",
}

var upgradeOrderLaser = []string{
	"plating", "armor", "laser_damage", "laser_charge", "nanites", "gun_damage", "gun_rate", "lightning_efficiency", "lightning_range",
	"servos", "gun_mag", "gun_velocity", "gyros", "lightning_chain", "reactor", "capacitor",
	"mortar_reload", "mortar_splash", "mortar_velocity", "sensors", "jets", "laser_pierce",
}

var upgradeOrderNano = []string{
	"plating", "armor", "nanites", "laser_damage", "laser_charge", "gun_damage", "gun_rate", "lightning_efficiency", "lightning_range",
	"servos", "gun_mag", "gun_velocity", "gyros", "lightning_chain", "reactor", "capacitor",
	"mortar_reload", "mortar_splash", "mortar_velocity", "sensors", "jets", "laser_pierce",
}

// upgradeOrderSquad: the laser order with nanites second (tweakNano): hunters come one after another, so the
// regen between fights is worth more than another armor stack.
// upgradeOrderMob (tweakMobUp): plating and armor, then speed and boost energy (servos, jets, reactor), then the laser order.
var upgradeOrderMob = func() []string {
	locOut2 := []string{"plating", "armor", "servos", "jets", "reactor"}
	for _, locU2 := range upgradeOrderLaser {
		if locU2 != "plating" && locU2 != "armor" && locU2 != "servos" && locU2 != "jets" && locU2 != "reactor" {
			locOut2 = append(locOut2, locU2)
		}
	}
	return locOut2
}()

var upgradeOrderSquad = []string{
	"plating", "nanites", "armor", "laser_damage", "laser_charge", "gun_damage", "gun_rate", "lightning_efficiency", "lightning_range",
	"servos", "gun_mag", "gun_velocity", "gyros", "lightning_chain", "reactor", "capacitor",
	"mortar_reload", "mortar_splash", "mortar_velocity", "sensors", "jets", "laser_pierce",
}

func pickUpgrade(locOf *Offer, locLaserUp, locNanoUp bool) int {
	locOrder := upgradeOrder
	if locLaserUp {
		locOrder = upgradeOrderLaser
	}
	if locNanoUp {
		locOrder = upgradeOrderNano
	}
	return pickFrom(locOf, locOrder)
}

func pickFrom(locOf2 *Offer, locOrder2 []string) int {
	locBest5, locBestRank := 0, 1<<30
	for locI36, locC32 := range locOf2.Choices {
		locRank := len(locOrder2)
		for locR25, locId6 := range locOrder2 {
			if locId6 == locC32.ID {
				locRank = locR25
				break
			}
		}
		if locRank < locBestRank {
			locBest5, locBestRank = locI36, locRank
		}
	}
	return locBest5
}

var weaponRank = map[string]int{"lightning": 3, "laser": 2, "mortar": 1}

func (locB7 *Brain) step(locO19 *Obs, locAct *Action) {
	locMe5 := &locO19.Self
	locB7.now = locO19.T
	locNow3 := locO19.T
	locB7.observe(locO19)
	locSt := &locMe5.Stats
	locX50, locY3, locZf2 := locMe5.Pos[0], locMe5.Pos[1], locMe5.Pos[2]
	locZ31 := &locO19.Zone
	locHpFrac3 := locMe5.HP / math.Max(1, locMe5.MaxHP)

	if locO19.Offer != nil && len(locO19.Offer.Choices) > 0 {
		locP14 := pickUpgrade(locO19.Offer, locB7.P.LaserUp, locB7.P.NanoUp)
		if tweakNano && locB7.pracOn && locB7.P.LaserUp && !locB7.P.NanoUp {
			locP14 = pickFrom(locO19.Offer, upgradeOrderSquad)
		}
		if tweakMobUp && locB7.pracOn && locB7.P.LaserUp && !locB7.P.NanoUp {
			locP14 = pickFrom(locO19.Offer, upgradeOrderMob)
		}
		locAct.Pick = &locP14
	}

	_, locGun := hasWeapon(locMe5.Weapons, "gun")
	_, locLight2 := hasWeapon(locMe5.Weapons, "lightning")
	_, locLaser := hasWeapon(locMe5.Weapons, "laser")
	locLate := locZ31.NextRadius <= 70 || locO19.Alive <= 3

	var locSeen []*Track
	var locTarget, locHidden2, locShy *Track
	locBestScore := math.Inf(1)
	locMaxT, locHpw := 215.0, 60.0/1400
	if locB7.P.MaxTarget > 0 {
		locMaxT = locB7.P.MaxTarget
	}
	if locB7.P.HPw > 0 {
		locHpw = locB7.P.HPw
	}
	for _, locT45 := range locB7.tracks {
		if locNow3-locT45.Seen > 2.5 {
			continue
		}
		locT45.Engage = false
		if locT45.Seen == locNow3 {
			locSeen = append(locSeen, locT45)
		} else {
			locSeen = append(locSeen, locT45)
			locT45.LOS = false
			continue
		}
		locToMe2 := math.Atan2(locY3-locT45.Pos[1], locX50-locT45.Pos[0])
		locAimed := math.Abs(angDiff(locT45.Aim[0], locToMe2)) < 0.22
		switch {
		case locB7.P.Ghost:

			locT45.Engage = locT45.Dist < locB7.P.EngageDist || locNow3-locT45.HitMe < 4 || (locAimed && locT45.Charging && locT45.Dist < 120) ||
				(locT45.HP < 350 && locT45.Dist < 120) || locO19.Alive <= 2
			locOff := math.Abs(angDiff(locT45.Aim[0], locToMe2))

			if locB7.P.Vulture > 0 && locHpFrac3 > 0.75 && locT45.HP < locB7.P.Vulture*locT45.MaxHP && locOff > 0.6 && locT45.Dist < 165 {
				locT45.Engage = true
			}
			if locB7.P.Alert > 0 && locT45.LOS && locT45.Dist < locB7.P.Alert && (locT45.Closing > 3 || locAimed) {
				locT45.Engage = true
			}
			if locB7.P.Shy > 0 && locT45.LOS && locT45.Dist < locB7.P.Shy && locT45.Dist >= locB7.P.EngageDist && (locT45.Closing > 3 || locAimed) && !locT45.Engage &&
				(locShy == nil || locT45.Dist < locShy.Dist) {
				locShy = locT45
			}

			if locB7.P.Ambush > 0 && locHpFrac3 > 0.85 && locOff > 1.75 && locT45.Dist < locB7.P.Ambush {
				locT45.Engage = true
			}
			if locB7.P.Odds > 0 && !locT45.Engage && locT45.LOS && locHpFrac3 > 0.5 && locT45.Dist < locB7.reach(locMe5) && locB7.odds(locMe5, locT45, locNow3, true) >= locB7.P.Odds {
				locT45.Engage = true
			}
		case locB7.P.AllFoes && locT45.Dist < locMaxT:
			locT45.Engage = true
		case locT45.Dist < locB7.P.EngageDist, locNow3-locT45.HitMe < 6, locLate:
			locT45.Engage = true
		case locT45.Closing > 3 && locT45.Dist < 180:
			locT45.Engage = true
		case locT45.HP < 0.4*locT45.MaxHP && locT45.Dist < 160:
			locT45.Engage = true
		case locAimed && locT45.Dist < 190:
			locT45.Engage = true
		}
		if locT45.Engage && !locT45.LOS && locT45.Dist < locMaxT && (locHidden2 == nil || locT45.Dist < locHidden2.Dist) {
			locHidden2 = locT45
		}
		if !locT45.Engage || !locT45.LOS || locT45.Dist > locMaxT {
			continue
		}
		locS3 := locT45.Dist + locHpw*locT45.HP
		if locB7.P.LowBonus > 0 && locT45.HP <= 0.3*locT45.MaxHP {
			locS3 -= locB7.P.LowBonus
		}
		if locB7.P.Stick > 0 && locT45.ID == locB7.stickID && locNow3-locB7.stickAt < 2 {
			locS3 -= locB7.P.Stick
		}
		if locNow3-locT45.HitMe < 4 {
			locS3 -= 40
		}
		if locT45.Closing > 3 {
			locS3 -= 15
		}
		if tweakRear > 0 && locB7.pracOn && locB7.backTo(locT45, locX50, locY3) {
			locS3 -= tweakRear
		}
		if locS3 < locBestScore {
			locBestScore, locTarget = locS3, locT45
		}
	}
	if locTarget != nil {
		locB7.stickID, locB7.stickAt = locTarget.ID, locNow3
	}
	locFoe := locTarget
	if locFoe == nil && locB7.P.Peek {
		locFoe = locHidden2
	}
	if locFoe == nil && locB7.P.Pursue > 0 && locHidden2 != nil && locHidden2.HP < locB7.P.Pursue*locHidden2.MaxHP {
		locFoe = locHidden2
	}
	locDanger2 := false
	locCrowd := 0
	for _, locT35 := range locSeen {
		if locT35.Engage || locNow3-locT35.HitMe < 4 {
			locDanger2 = true
		}
		if locT35.Seen == locNow3 && (locT35.Engage || (locT35.LOS && locT35.Dist < 150)) {
			locCrowd++
		}
	}
	if locDanger2 || locNow3-locB7.lastHurt < 3 {
		locB7.lastThreatAt = locNow3
	}

	locFinishing := locTarget != nil && locTarget.Dist < 120 && (locTarget.HP < 200 || locTarget.HP < locMe5.HP*0.5)
	_, locMyLaser := hasWeapon(locMe5.Weapons, "laser")
	locOutgunned := locTarget != nil && holds(locTarget.Weapons, "laser") && locMyLaser == nil && locTarget.Dist > 55 && locHpFrac3 < 0.8
	locSwamped := locB7.P.CrowdHide && (locCrowd >= 3 || (locCrowd == 2 && locHpFrac3 < 0.65) || locOutgunned)
	if locB7.P.OddsFlee > 0 && locTarget != nil && !locFinishing && locB7.odds(locMe5, locTarget, locNow3, false) < locB7.P.OddsFlee {
		locB7.oddsFleeUntil = locNow3 + 5
	}
	if locNow3 < locB7.oddsFleeUntil && !locFinishing {
		locSwamped = true
	}
	if !locB7.retreat && ((locHpFrac3 < locB7.P.RetreatHP && locNow3-locB7.lastThreatAt < 2) || locSwamped) {
		locB7.retreat = true
		locB7.coverAt = -1
	}
	if locB7.retreat && !locSwamped && (locHpFrac3 > locB7.P.RecoverHP || (locNow3-locB7.lastThreatAt > 7 && locHpFrac3 > 0.55)) {
		locB7.retreat = false
	}
	if locB7.P.LoseRule {
		locNear3 := 0
		for _, locT16 := range locSeen {
			if locT16.Seen == locNow3 && locT16.Dist < 160 {
				locNear3++
			}
		}
		locLosing := (locNear3 >= 2 && locHpFrac3 < 0.55*tw.loseK) || (locNear3 >= 3 && locHpFrac3 < math.Min(0.95, 0.8*tw.loseK))
		if locTarget != nil {
			locTf := locTarget.HP / math.Max(1, locTarget.MaxHP)
			if locHpFrac3 < 0.35*tw.loseK && locTf > locHpFrac3+0.1 {
				locLosing = true
			}
			if locTf < 0.25 && locHpFrac3 > 0.2 && locNear3 <= 2 {
				locLosing = false
			}
		}
		if locLosing {
			locB7.loseAt = locNow3
		}
		locB7.retreat = locNow3-locB7.loseAt < 1.5 && locB7.loseAt > 0
	}

	locExposed := false
	for _, locT31 := range locSeen {
		if locT31.Seen == locNow3 && locT31.LOS && locT31.Dist < 230 {
			locExposed = true
		}
	}
	for _, locT5 := range locSeen {
		if locT5.Seen == locNow3 && locT5.Dist < 45 {
			locExposed = false
		}
	}

	var locMx3, locMy8 float64
	locBoost := false
	locMode := "idle"
	var locFarm *Track
	var locFarmPos *Vec3

	locZgx, locZgy := locB7.zoneGoal(locZ31, locX50, locY3)
	locZdist := hyp(locZgx-locX50, locZgy-locY3)
	locTNeed := locZdist*1.45/locSt.WalkSpeed + 4
	locDeadline := locB7.zoneDeadline(locZ31, locX50, locY3, 8)
	locZoneMust := !locMe5.InZone || locNow3+locTNeed+3 >= locDeadline
	locZoneShould := locNow3+locTNeed+14 >= locDeadline

	locEscJump := false
	switch {
	case locB7.escape(locMe5, &locMx3, &locMy8, &locBoost, &locEscJump):
		locMode = "escape"
	case locNow3 < locB7.unstuckUntil:
		locMx3, locMy8 = locB7.unstuckDir[0], locB7.unstuckDir[1]
		locMode = "unstuck"
	case locB7.dodgeShell(locO19, &locMx3, &locMy8):
		locBoost = locMe5.Energy > 15
		locMode = "dodge"
	case locZoneMust && locZdist > 2:
		locMx3, locMy8 = locB7.travel(locMe5, locZgx, locZgy)
		locBoost = (!locMe5.InZone || locNow3+locTNeed >= locDeadline) && locMe5.Energy > 5
		locMode = "zone"
	case locB7.retreat && !locFinishing && !locLate:
		if locB7.coverMove(locO19, locSeen, &locMx3, &locMy8) {
			locBoost = locMe5.Energy > 10 && (locMx3 != 0 || locMy8 != 0)
			locMode = "cover"
		} else if locTarget != nil {
			locMx3, locMy8 = locB7.fightMove(locO19, locTarget, locSeen, 1)
			locMode = "cornered"
		} else {
			locMx3, locMy8 = locB7.travel(locMe5, locZgx, locZgy)
			if locB7.npcHurt > 0 && locNow3-locB7.npcHurt < 3 && locMx3 == 0 && locMy8 == 0 {

				locSide := 1.0
				if math.Mod(locNow3, 3) > 1.5 {
					locSide = -1
				}
				locMx3, locMy8 = -math.Sin(locB7.npcBearing)*locSide, math.Cos(locB7.npcBearing)*locSide
			}
			locMode = "flee"
		}
	case locTarget == nil && locNow3-locB7.lastHurt < 2.5 && !locLate && locB7.P.Ghost && locB7.hideFromBearing(locO19, locSeen, &locMx3, &locMy8):
		locBoost = locMe5.Energy > 30 && (locMx3 != 0 || locMy8 != 0)
		locMode = "cover"
	case locNow3 < locB7.P.EarlyHide && locExposed && !locFinishing && locB7.coverMove(locO19, locSeen, &locMx3, &locMy8):
		locBoost = locMe5.Energy > 20 && (locMx3 != 0 || locMy8 != 0)
		locMode = "earlyhide"
	case locFoe == nil && locShy != nil && !locLate && locB7.coverMove(locO19, locSeen, &locMx3, &locMy8):
		locBoost = locMe5.Energy > 40 && (locMx3 != 0 || locMy8 != 0)
		locMode = "shy"
	case locB7.P.Evade && locFoe != nil && !locLate && !locFinishing && locFoe.Dist > locB7.P.EngageDist*1.2 && locFoe.HP > 0.45*locFoe.MaxHP &&
		locB7.coverMove(locO19, locSeen, &locMx3, &locMy8):
		locBoost = locMe5.Energy > 30 && (locMx3 != 0 || locMy8 != 0)
		locMode = "evade"
	case locFoe != nil:

		locReady := locGun != nil && locGun.Ammo > 0 && locGun.Reload <= 0.25 && locFoe.Dist <= locGun.Stats.Range-10
		locHot := locLaser != nil && locLaser.Charge >= locLaser.Stats.Charge-0.3 && locFoe.Dist <= locLaser.Stats.Range-5
		if locHot || (locLight2 != nil && locFoe.Dist <= locLight2.Stats.Range+6 && locMe5.Energy >= locLight2.Stats.Energy*3) {
			locReady = true
		}
		if !locB7.P.Peek && (locLight2 != nil || locLaser != nil) {
			locReady = true
		}
		locWantLos := -1.0
		if locReady {
			locWantLos = 1
		}
		if locB7.P.Peek && !locHot {
			for _, locT15 := range locSeen {
				if locT15.Seen == locNow3 && locT15.Charging && math.Abs(angDiff(locT15.Aim[0], math.Atan2(locY3-locT15.Pos[1], locX50-locT15.Pos[0]))) < 0.3 {
					locWantLos = -1.6
				}
			}
		}
		locFinHP := 0.38
		if locB7.P.FinishHP > 0 {
			locFinHP = locB7.P.FinishHP
		}
		if locKeep := locB7.P; locB7.P.Finish > 0 && locFoe.HP < locFinHP*locFoe.MaxHP && locHpFrac3 > 0.42 && locCrowd <= 2 {
			locB7.P.GunRange, locB7.P.LaserRange, locB7.P.LightRange = locB7.P.Finish, locB7.P.Finish, locB7.P.Finish
			locMx3, locMy8 = locB7.fightMove(locO19, locFoe, locSeen, locWantLos)
			locB7.P = locKeep
			if locB7.P.FinishRush && locFoe.Dist > 2*locB7.P.Finish && locMe5.Energy > 30 && locMe5.Grounded {
				locBoost = true
			}
		} else {
			locMx3, locMy8 = locB7.fightMove(locO19, locFoe, locSeen, locWantLos)
		}
		if locLight2 != nil && locFoe.Dist > 50 && locMe5.Energy > 55 && locWantLos > 0 {
			locBoost = true
		}
		locMode = "fight"
		if locB7.P.FightLoot > 0 {
			locR24 := locB7.P.FightLoot
			if locHpFrac3 <= 0.5 {
				locR24 *= 0.57
			}
			var locBc *Vec3
			for _, locC33 := range locB7.cubes {
				if tweakCubeLevel && locC33.Pos[2]-locB7.W.GroundAt(locC33.Pos[0], locC33.Pos[1]) > 3.2 && math.Abs(locC33.Pos[2]-locZf2) > 2 {
					continue
				}
				if locD23 := hyp(locC33.Pos[0]-locX50, locC33.Pos[1]-locY3); locD23 < locR24 {
					locR24 = locD23
					locCp3 := locC33.Pos
					locBc = &locCp3
				}
			}
			if locBc != nil {
				locMx3, locMy8 = locB7.travel(locMe5, locBc[0], locBc[1])
				locMode = "fight:cube"
			}
		}
		if tweakFightDropR > 0 && locB7.pracOn && !(locFinishing && locHpFrac3 > 0.5) {
			// a supply drop refills HP mid-fight: walk into its ring and keep shooting from inside it
			if locD15 := locB7.fightDrop(locO19, locHpFrac3); locD15 != nil {
				locR7 := locB7.dropRadius(locD15)
				locDd3 := hyp(locD15.Pos[0]-locX50, locD15.Pos[1]-locY3)
				if locDd3 > locR7-3 || hyp(locX50+locMx3*2-locD15.Pos[0], locY3+locMy8*2-locD15.Pos[1]) > locR7-2.5 {
					locMx3, locMy8 = locB7.travel(locMe5, locD15.Pos[0], locD15.Pos[1])
				}
				locBoost = locDd3 > locR7+4 && locMe5.Energy > 30
				locMode = "fight:drop"
			}
		}
	case locZoneShould && locZdist > 2:
		locMx3, locMy8 = locB7.travel(locMe5, locZgx, locZgy)
		locMode = "zone"
	default:
		locLc := locB7.chooseLoot(locO19, locHpFrac3, locDanger2)
		if locLc != nil {
			locMode = "loot:" + locLc.kind
			switch locLc.kind {
			case "npc":
				locT34 := locB7.npcs[locLc.id]
				if locT34 != nil && locNow3-locT34.Seen < 1 && locT34.LOS && locT34.Dist < 75 {
					locFarm = locT34
					locMx3, locMy8 = locB7.fightMove(locO19, locT34, locSeen, 1)
				} else {
					locMx3, locMy8 = locB7.travel(locMe5, locLc.pos[0], locLc.pos[1])
				}
			case "crate":
				locD2 := hyp(locLc.pos[0]-locX50, locLc.pos[1]-locY3)
				locCp := Vec3{locLc.pos[0], locLc.pos[1], locLc.pos[2] + locB7.Cfg.Crates.Height*0.5}
				if locD2 < 50 && locB7.W.Clear(Vec3{locX50, locY3, locZf2 + locSt.MuzzleHeight}, locCp) {
					locFarmPos = &locCp
					if locD2 < 12 {
						locMx3, locMy8 = (locX50-locLc.pos[0])/math.Max(locD2, 0.1), (locY3-locLc.pos[1])/math.Max(locD2, 0.1)
					}
				} else {
					locMx3, locMy8 = locB7.travel(locMe5, locLc.pos[0], locLc.pos[1])
				}
			default:
				locMx3, locMy8 = locB7.travel(locMe5, locLc.pos[0], locLc.pos[1])
				if locLc.kind == "drop" && hyp(locLc.pos[0]-locX50, locLc.pos[1]-locY3) < 2.5 {
					locMx3, locMy8 = 0, 0
				}
			}
		} else if locZdist > 3 && (!locB7.P.Ghost || locZ31.NextRadius <= 30) {
			locMx3, locMy8 = locB7.travel(locMe5, locZgx, locZgy)
			locMode = "zone-walk"
		} else if locB7.P.Ghost {

			if locHx2, locHy3, locOk14 := locB7.home(locO19); locOk14 && hyp(locHx2-locX50, locHy3-locY3) > 3 {
				locMx3, locMy8 = locB7.travel(locMe5, locHx2, locHy3)
				if !locDanger2 && locNow3-locB7.lastHurt > 6 && !locB7.openOn {
					locQ2 := math.Min(1, 4.6/locSt.WalkSpeed)
					locMx3, locMy8 = locMx3*locQ2, locMy8*locQ2
				}
				locMode = "home"
			}
		} else if locB7.P.Explore && !locLate && locMe5.Cubes < locB7.cubeCap() && locHpFrac3 > 0.7 && !locDanger2 {
			locMx3, locMy8 = locB7.exploreMove(locO19)
			locMode = "explore"
		}
	}

	if locEscJump {
		locAct.Jump = true
	}
	if tweakBoostHyst && locB7.pracOn {
		if locMe5.Energy < 30 {
			locB7.boostLow = true
		} else if locMe5.Energy > 60 {
			locB7.boostLow = false
		}
	}
	locExtraOK := !(tweakBoostHyst && locB7.boostLow)
	if locB7.P.Dodge && locMode != "unstuck" && locMode != "escape" {
		if locDx18, locDy17, locLz := locB7.dodge(locO19); locDx18 != 0 || locDy17 != 0 {
			locMx3, locMy8 = locMx3*0.4+locDx18*0.9, locMy8*0.4+locDy17*0.9
			if locL8 := hyp(locMx3, locMy8); locL8 > 1 {
				locMx3, locMy8 = locMx3/locL8, locMy8/locL8
			}
			if locLz && locMe5.Energy > 25 {
				locBoost = true
			}
			if tweakDodgeBoost && locB7.pracOn && locMe5.Energy > 25 && locMe5.Grounded && locExtraOK {
				locBoost = true
			}
		}
	}
	if locB7.pracOn && locMe5.Grounded && (locMx3 != 0 || locMy8 != 0) && locMode != "unstuck" && locMode != "escape" {
		locCharging := locLaser != nil && locLaser.Charge > 0
		locLightOK := !tweakBoostHyst || locLight2 == nil || locMe5.Energy > 60 // keep energy for lightning shots
		if tweakJinkBoost && locExtraOK && locLightOK && (locMode == "fight" || locMode == "cornered") && locTarget != nil && locMe5.Energy > 40 && !locCharging {
			// strafe at boost speed in gunfights: harder to lead
			locBoost = true
		}
		if tweakThreatBoost && locExtraOK && locLightOK && locMe5.Energy > 30 && !locCharging && (locTarget != nil || locNow3-locB7.lastHurt < 2) {
			locBoost = true
		}
		if tweakChargeBoost && locExtraOK && locCharging && locNow3-locB7.lastHurt < 2 && locMe5.Energy > 30 {
			// under fire while charging: half speed, so boost to keep moving
			locBoost = true
		}
	}

	if locMode == "escape" {
		locB7.stuckSince = -1
	} else if (locMx3 != 0 || locMy8 != 0) && hyp(locMe5.Vel[0], locMe5.Vel[1]) < 1.2 && locMe5.Grounded {
		if locB7.stuckSince < 0 {
			locB7.stuckSince = locNow3
		} else if locNow3-locB7.stuckSince > 0.7 {
			locA14 := locB7.rng.Float64() * 2 * math.Pi
			locB7.unstuckDir = [2]float64{math.Cos(locA14), math.Sin(locA14)}
			locB7.unstuckUntil = locNow3 + 0.5
			locB7.stuckSince = -1
			locB7.path = nil
			locAct.Jump = true
		}
	} else {
		locB7.stuckSince = -1
	}

	locMuzzle := Vec3{locX50 + locMe5.Vel[0]*lat, locY3 + locMe5.Vel[1]*lat, locZf2 + locSt.MuzzleHeight}
	locAim := [2]float64{locMe5.Aim[0], 0}
	locHaveAim := false
	var locTp, locTv Vec3
	var locTr, locTh float64
	locShootable := false
	locIsMech := false
	locDefend := false
	if locTarget == nil && locB7.npcHurt > 0 && locNow3-locB7.npcHurt < 3 {

		var locWorst *Track
		locBestOff := 0.7
		for _, locN16 := range locB7.npcs {
			if locNow3-locN16.Seen > 0.5 || !locN16.LOS || locN16.Dist > 150 {
				continue
			}
			locOff2 := math.Abs(angDiff(math.Atan2(locN16.Pos[1]-locY3, locN16.Pos[0]-locX50), locB7.npcBearing))
			if locNow3-locN16.HitMe < 3 {
				locOff2 = 0
			}
			locTie := tweakNpcTie && locWorst != nil && locOff2 == locBestOff && (locN16.Dist < locWorst.Dist || locN16.Dist == locWorst.Dist && locN16.ID < locWorst.ID)
			if locOff2 < locBestOff || locTie {
				locBestOff, locWorst = locOff2, locN16
			}
		}
		if locWorst != nil {
			locFarm, locFarmPos, locDefend = locWorst, nil, true
		}
	}
	if locB7.P.Econ && locTarget == nil && locFarm == nil && locFarmPos == nil && locGun != nil && !locB7.retreat {

		locReach := 110.0
		for _, locT22 := range locB7.tracks {
			if locNow3-locT22.Seen < 4 {
				locReach = 50
			}
		}
		for _, locC17 := range locB7.crates {
			if locUntil, locOk9 := locB7.bad[locC17.ID]; locOk9 && locUntil > locNow3 {
				continue
			}
			locD28 := hyp(locC17.Pos[0]-locX50, locC17.Pos[1]-locY3)
			locCp2 := Vec3{locC17.Pos[0], locC17.Pos[1], locC17.Pos[2] + locB7.Cfg.Crates.Height*0.5}
			if locD28 < locReach && locD28 > 6 && locB7.W.Clear(locMuzzle, locCp2) {
				locReach = locD28
				locFarmPos = &locCp2
			}
		}
	}
	switch {
	case locTarget != nil:
		locSc3 := locB7.scaleOf(locTarget.Cubes)
		locTr, locTh = locB7.Cfg.Mech.Radius*locSc3, locB7.Cfg.Mech.Height*locSc3
		locTp, locTv = locTarget.Pos, locTarget.Vel
		locTp[2] += locTh * 0.5
		if !locB7.W.Clear(locMuzzle, locTp) {
			locTp[2] = locTarget.Pos[2] + locTh*0.85
		}
		locShootable, locIsMech = true, true
	case locFarm != nil:
		locTr, locTh = locB7.Cfg.Npc.Radius, locB7.Cfg.Npc.Height
		locTp, locTv = locFarm.Pos, locFarm.Vel
		locTp[2] += locTh * 0.5
		locShootable = true
	case locFarmPos != nil:
		locTr, locTh = locB7.Cfg.Crates.Radius, locB7.Cfg.Crates.Height
		locTp = *locFarmPos
		locShootable = true
	}
	if !locIsMech && locTarget == nil && (locB7.retreat || locHpFrac3 < 0.5) && !locB7.P.Econ && !locDefend {
		locShootable = false
	}

	if locShootable {
		locDist2 := hyp(locTp[0]-locX50, locTp[1]-locY3)
		locGrounded := math.Abs(locTv[2]) < 0.5

		var locGy8, locGp float64
		locGunOK := false
		if locGun != nil {
			locGy8, locGp = locB7.lead(locMuzzle, locTp, locTv, locGun.Stats.Speed, locGrounded)
			locGunOK = locDist2 <= locGun.Stats.Range*0.96
		}

		locDp := Vec3{locTp[0] + locTv[0]*(lat+0.05), locTp[1] + locTv[1]*(lat+0.05), locTp[2]}
		locHy2 := math.Atan2(locDp[1]-locMuzzle[1], locDp[0]-locMuzzle[0])
		locHp := math.Atan2(locDp[2]-locMuzzle[2], hyp(locDp[0]-locMuzzle[0], locDp[1]-locMuzzle[1]))
		locAim = [2]float64{locGy8, locGp}
		if locGun == nil {
			locAim = [2]float64{locHy2, locHp}
		}
		locHaveAim = true
		locTolY := math.Atan2(locTr*0.75, math.Max(locDist2, 1))
		locTolP := math.Atan2(locTh*0.4, math.Max(locDist2, 1))
		locSlew := func(locWantY, locWantP float64) bool {
			locEy4 := math.Max(0, math.Abs(angDiff(locWantY, locMe5.Aim[0]))-locSt.TurnRate*0.05)
			locEp2 := math.Max(0, math.Abs(locWantP-locMe5.Aim[1])-locSt.PitchRate*0.05)
			return locEy4 < locTolY && locEp2 < locTolP
		}
		for locI10, locW16 := range locMe5.Weapons {
			if locW16 == nil || locI10 > 1 {
				continue
			}
			switch locW16.Kind {
			case "gun":
				if locGunOK && locW16.Ammo > 0 && locW16.Reload <= 0 && locSlew(locGy8, locGp) {
					locAct.Fire[locI10] = true
				}
			case "lightning":
				locCone := locW16.Stats.ConeDeg * math.Pi / 180
				if locDist2 < locW16.Stats.Range-1.5 && math.Abs(angDiff(locHy2, locMe5.Aim[0])) < locCone*0.5-0.05 && locMe5.Energy >= locW16.Stats.Energy {
					locAct.Fire[locI10] = true
				}
			case "laser":
				if !locIsMech || locDist2 > locW16.Stats.Range*0.97 {
					break
				}
				locFull := locW16.Stats.Charge
				if locB7.P.Lethal && locW16.Charge >= locW16.Stats.MinCharge && locW16.Charge < locFull-0.051 {
					locDmg := locW16.Stats.Damage * (locW16.Stats.MinFrac + (1-locW16.Stats.MinFrac)*locW16.Charge/locFull) *
						(1 + locB7.Cfg.Cubes.DamagePerCube*float64(min(locMe5.Cubes, locB7.Cfg.Cubes.StatCap)))
					if holds(locTarget.Weapons, "mortar") {
						locDmg *= 1 + locW16.Stats.Cookoff
					}
					if tweakRear > 0 && locB7.pracOn && locB7.Cfg.Mech.RearMult > 1 && locB7.backTo(locTarget, locX50, locY3) {
						locDmg *= locB7.Cfg.Mech.RearMult
					}
					locLk := 0.8
					if tweakLethalK > 0 && locB7.pracOn {
						locLk = tweakLethalK
					}
					if locDmg*locLk >= locTarget.HP && math.Abs(angDiff(locHy2, locMe5.Aim[0])) < locTolY*0.9 && math.Abs(locHp-locMe5.Aim[1]) < locTolP {
						locAim = [2]float64{locHy2, locHp}
						locAct.Fire[locI10] = false
						break
					}
				}
				if locW16.Charge < locFull-0.051 {
					locAct.Fire[locI10] = true
					if locW16.Charge > locFull*0.45 {
						locAim = [2]float64{locHy2, locHp}
					}
				} else {
					locAim = [2]float64{locHy2, locHp}
					locEy2 := math.Abs(angDiff(locHy2, locMe5.Aim[0]))
					locEp3 := math.Abs(locHp - locMe5.Aim[1])
					if locEy2 < locTolY*0.9 && locEp3 < locTolP {
						locAct.Fire[locI10] = false
					} else {
						locAct.Fire[locI10] = true
					}
				}
			case "mortar":
				if !locIsMech || locW16.Cooldown > 0.25 || locDist2 < 16 || locDist2 > 200 {
					break
				}
				locTube2 := Vec3{locMuzzle[0], locMuzzle[1], locZf2 + locSt.Height + 0.5}
				locFoot := Vec3{locTp[0], locTp[1], locTp[2] - locTh*0.5}
				if locMy22, locMp, locOk15 := locB7.mortar(locTube2, locFoot, locTv, locW16.Stats.Speed, locW16.Stats.Gravity, locSt); locOk15 {

					if locGun == nil || locGun.Reload > 0 || locGun.Ammo == 0 || !locGunOK {
						locAim = [2]float64{locMy22, locMp}
					}
					locEy6 := math.Max(0, math.Abs(angDiff(locMy22, locMe5.Aim[0]))-locSt.TurnRate*0.05)
					locEp := math.Max(0, math.Abs(locMp-locMe5.Aim[1])-locSt.PitchRate*0.05)
					if locEy6 < 0.04 && locEp < 0.03 {
						locAct.Fire[locI10] = true
					}
				}
			}
		}
	} else {

	}

	if locTarget == nil && locFoe != nil {

		for locI24, locW53 := range locMe5.Weapons {
			if locW53 != nil && locI24 < 2 && locW53.Kind == "laser" && locW53.Cooldown <= 0.05 && locFoe.Dist < locW53.Stats.Range && locW53.Charge < locW53.Stats.Charge+locW53.Stats.HoldS-0.35 {
				locAct.Fire[locI24] = true
			}
		}
	}

	if !locHaveAim {

		var locWatch *Track
		for _, locT8 := range locSeen {
			if locT8.Engage || locNow3-locT8.HitMe < 6 {
				if locWatch == nil || locT8.Dist < locWatch.Dist {
					locWatch = locT8
				}
			}
		}
		switch {
		case locWatch != nil:
			locAim[0] = math.Atan2(locWatch.Pos[1]-locY3, locWatch.Pos[0]-locX50)
		case locB7.npcHurt > 0 && locNow3-locB7.npcHurt < 2.5:
			locAim[0] = locB7.npcBearing
		case locNow3-locB7.lastHurt < 2.5:
			locAim[0] = locB7.hurtBearing
		case tweakHear && locB7.pracOn && locB7.heardAt > 0 && locNow3-locB7.heardAt < 2:
			locAim[0] = locB7.heardBearing
		case locMx3 != 0 || locMy8 != 0:
			locAim[0] = math.Atan2(locMy8, locMx3) + 1.15*math.Sin(locNow3*1.3)
		default:
			locAim[0] = locMe5.Aim[0] + 0.35
		}
		locAim[1] = 0
	}
	locAim[1] = clamp(locAim[1], locSt.MinPitch, locSt.MaxPitch)
	locAct.Aim = &locAim

	if locGun != nil && locGun.Reload <= 0 && locGun.Ammo < locGun.Stats.Mag {
		if locTarget == nil && locFarm == nil && locFarmPos == nil && (locGun.Ammo <= locGun.Stats.Mag*0.6 || locNow3-locB7.lastThreatAt > 3) {
			locAct.Reload = true
		}
	}

	locSlot1 := ""
	if len(locMe5.Weapons) > 1 && locMe5.Weapons[1] != nil {
		locSlot1 = locMe5.Weapons[1].Kind
	}
	for locI6 := range locO19.Pickups {
		locP3 := &locO19.Pickups[locI6]
		if hyp(locP3.Pos[0]-locX50, locP3.Pos[1]-locY3) > locB7.Cfg.Loot.PickupRadius-0.3 || math.Abs(locP3.Pos[2]-locZf2) > 3.6 {
			continue
		}
		if !locB7.wantPickup(locMe5, locP3.Kind) {
			continue
		}
		locId5 := locP3.ID
		locAct.Take = &locId5
		if locSlot1 != "" && len(locMe5.Weapons) > 0 && locMe5.Weapons[0] != nil {
			if locSlot1 == "laser" && locMe5.Weapons[1].Charge > 0 {
				locAct.Take = nil
				break
			}
			locOne := 1
			locAct.Drop = &locOne
		}
		break
	}

	if tweakHear && locB7.pracOn && locNow3 < locB7.hearDodgeUntil && locMode != "unstuck" && locMode != "escape" {
		locMx3, locMy8 = locMx3*0.4+locB7.hearDodgeX*0.9, locMy8*0.4+locB7.hearDodgeY*0.9
		if locL := hyp(locMx3, locMy8); locL > 1 {
			locMx3, locMy8 = locMx3/locL, locMy8/locL
		}
		if locMe5.Energy > 25 && locMe5.Grounded {
			locBoost = true
		}
	}
	if tweakNoAirBoost && !locMe5.Grounded && locMode != "escape" && locMode != "dodge" {
		locBoost = false
	}
	if tw.hopT > 0 && locNow3 < tw.hopT && (locMx3 != 0 || locMy8 != 0) {
		locMx3, locMy8 = locMx3*0.5-locMy8*0.85, locMy8*0.5+locMx3*0.85
		locAct.Jump = true
		locBoost = true
	}
	locAct.Move = [2]float64{locMx3, locMy8}
	locAct.Boost = locBoost && (locMx3 != 0 || locMy8 != 0)
	locB7.Mode = locMode
}

func (locB57 *Brain) escape(locMe9 *Self, locMx, locMy9 *float64, locBoost2, locJump *bool) bool {
	locX15, locY42, locZf := locMe9.Pos[0], locMe9.Pos[1], locMe9.Pos[2]
	locW21 := locB57.W
	if locMe9.Grounded {
		if locZf-locW21.GroundAt(locX15, locY42) > 3 && locW21.Obst[locW21.vidx(locX15, locY42)] {
			locB57.escOK, locB57.escFlying = false, false
			return false
		}
		if !locW21.OffMain(locX15, locY42) {
			locB57.escOK, locB57.escFlying = false, false
			return false
		}
		locB57.escFlying = false
		if !locB57.escOK || locB57.now-locB57.escAt > 6 {
			locB57.escLaunch, locB57.escLand, locB57.escOK = locW21.EscapePlan(locX15, locY42)
			locB57.escAt = locB57.now
			if !locB57.escOK {
				return false
			}
		}
		if hyp(locB57.escLaunch[0]-locX15, locB57.escLaunch[1]-locY42) > 2.5 {
			*locMx, *locMy9 = locB57.travel(locMe9, locB57.escLaunch[0], locB57.escLaunch[1])
			return true
		}
		if locMe9.Energy < math.Min(70, locMe9.MaxEnergy*0.7) {
			*locMx, *locMy9 = 0, 0
			return true
		}
		locB57.escFlying = true
		*locJump = true
	} else if !locB57.escFlying {
		return false
	}
	locDx12, locDy4 := locB57.escLand[0]-locX15, locB57.escLand[1]-locY42
	locD19 := hyp(locDx12, locDy4)
	if locD19 < 1.5 {
		return true
	}
	*locMx, *locMy9 = locDx12/locD19, locDy4/locD19
	locNeed := -1e9
	for locT48 := 0.0; locT48 <= math.Min(locD19, 30); locT48 += 1 {
		locNeed = math.Max(locNeed, locW21.TopAt(locX15+*locMx*locT48, locY42+*locMy9*locT48))
	}
	*locBoost2 = locZf < locNeed+2.5 && locMe9.Energy > 1
	return true
}

func (locB59 *Brain) wantPickup(locMe15 *Self, locKind5 string) bool {
	if locKind5 == "gun" {
		return len(locMe15.Weapons) > 0 && locMe15.Weapons[0] == nil
	}
	if locI18, _ := hasWeapon(locMe15.Weapons, locKind5); locI18 >= 0 {
		return false
	}
	if locKind5 == "mortar" && locB59.P.NoMortar {
		return false
	}
	locRank2 := func(locK12 string) int {
		if locB59.P.LaserFirst && locK12 == "laser" {
			return 4
		}
		return weaponRank[locK12]
	}
	locCur := 0
	if len(locMe15.Weapons) > 1 && locMe15.Weapons[1] != nil {
		locCur = locRank2(locMe15.Weapons[1].Kind)
	}
	return locRank2(locKind5) > locCur
}

func (locB54 *Brain) lead(locMuzzle3, locTp2, locTv3 Vec3, locSpeed float64, locGrounded2 bool) (float64, float64) {
	if locSpeed <= 0 {
		locSpeed = 252
	}
	locT36 := 0.0
	var locPx8, locPy11, locPz float64
	for locK := 0; locK < 4; locK++ {
		locTt2 := lat + locT36
		locPx8, locPy11, locPz = locTp2[0]+locTv3[0]*locTt2, locTp2[1]+locTv3[1]*locTt2, locTp2[2]
		if !locGrounded2 {
			locPz += locTv3[2] * locTt2 * 0.5
		}
		locD37 := math.Sqrt((locPx8-locMuzzle3[0])*(locPx8-locMuzzle3[0]) + (locPy11-locMuzzle3[1])*(locPy11-locMuzzle3[1]) + (locPz-locMuzzle3[2])*(locPz-locMuzzle3[2]))
		locT36 = math.Max(0, locD37-3) / locSpeed
	}
	return math.Atan2(locPy11-locMuzzle3[1], locPx8-locMuzzle3[0]), math.Atan2(locPz-locMuzzle3[2], hyp(locPx8-locMuzzle3[0], locPy11-locMuzzle3[1]))
}

func (locB11 *Brain) mortar(locTube, locFoot2, locTv2 Vec3, locV5, locG6 float64, locSt2 *MechStats) (locYaw, locPitch float64, locOk6 bool) {
	if locV5 <= 0 || locG6 <= 0 {
		return 0, 0, false
	}
	locT2 := 0.0
	for locK18 := 0; locK18 < 3; locK18++ {
		locPx2, locPy7 := locFoot2[0]+locTv2[0]*(lat+locT2), locFoot2[1]+locTv2[1]*(lat+locT2)
		locDx4 := hyp(locPx2-locTube[0], locPy7-locTube[1])
		locDz10 := locFoot2[2] - locTube[2]
		locDisc3 := locV5*locV5*locV5*locV5 - locG6*(locG6*locDx4*locDx4+2*locDz10*locV5*locV5)
		if locDisc3 < 0 || locDx4 < 1 {
			return 0, 0, false
		}
		locPitch = math.Atan((locV5*locV5 - math.Sqrt(locDisc3)) / (locG6 * locDx4))
		locYaw = math.Atan2(locPy7-locTube[1], locPx2-locTube[0])
		locT2 = locDx4 / (locV5 * math.Cos(locPitch))
	}
	if locPitch < locSt2.MinPitch || locPitch > locSt2.MaxPitch {
		return 0, 0, false
	}

	locC53, locS5 := math.Cos(locPitch), math.Sin(locPitch)
	for locTt := 0.1; locTt < locT2*0.9; locTt += 0.1 {
		locD18 := locV5 * locC53 * locTt
		locPx15, locPy := locTube[0]+math.Cos(locYaw)*locD18, locTube[1]+math.Sin(locYaw)*locD18
		locPz2 := locTube[2] + locV5*locS5*locTt - 0.5*locG6*locTt*locTt
		if locB11.W.Solid(locPx15, locPy, locPz2) {
			return 0, 0, false
		}
	}
	return locYaw, locPitch, true
}

func (locB24 *Brain) dodgeShell(locO5 *Obs, locMx2, locMy *float64) bool {
	locMe13 := &locO5.Self
	locG12 := locB24.Cfg.Weapons["mortar"].Gravity
	if locG12 <= 0 {
		locG12 = 22
	}
	locRad2 := locB24.Cfg.Weapons["mortar"].SplashRadius + locMe13.Stats.Radius + 2
	for locI41 := range locO5.Projectiles {
		locP5 := &locO5.Projectiles[locI41]
		if locP5.Kind != "mortar" {
			continue
		}
		locPx7, locPy3, locPz3 := locP5.Pos[0], locP5.Pos[1], locP5.Pos[2]
		locVz := locP5.Vel[2]
		for locT19 := 0.0; locT19 < 4; locT19 += 0.05 {
			locPx7 += locP5.Vel[0] * 0.05
			locPy3 += locP5.Vel[1] * 0.05
			locPz3 += locVz * 0.05
			locVz -= locG12 * 0.05
			if locB24.W.Solid(locPx7, locPy3, locPz3) {
				locD10 := hyp(locPx7-locMe13.Pos[0], locPy3-locMe13.Pos[1])
				if locD10 < locRad2 && locT19 < 2.5 {
					locDx5, locDy12 := locMe13.Pos[0]-locPx7, locMe13.Pos[1]-locPy3
					if locD10 < 0.5 {
						locDx5, locDy12 = -locP5.Vel[1], locP5.Vel[0]
						locD10 = hyp(locDx5, locDy12)
					}
					locDx5, locDy12 = locDx5/locD10, locDy12/locD10

					for _, locRot := range []float64{0, 0.7, -0.7, 1.4, -1.4} {
						locC, locS := math.Cos(locRot), math.Sin(locRot)
						locEx, locEy := locDx5*locC-locDy12*locS, locDx5*locS+locDy12*locC
						if locB24.W.LineWalkable(locMe13.Pos[0], locMe13.Pos[1], locMe13.Pos[0]+locEx*6, locMe13.Pos[1]+locEy*6) {
							*locMx2, *locMy = locEx, locEy
							return true
						}
					}
				}
				break
			}
		}
	}
	return false
}

func (locB43 *Brain) travel(locMe10 *Self, locGx4, locGy6 float64) (float64, float64) {
	locX28, locY10 := locMe10.Pos[0], locMe10.Pos[1]
	locD17 := hyp(locGx4-locX28, locGy6-locY10)
	if locD17 < 0.8 {
		return 0, 0
	}
	locUnit := func(locTx, locTy2 float64) (float64, float64) {
		locL7 := hyp(locTx-locX28, locTy2-locY10)
		if locL7 < 1e-6 {
			return 0, 0
		}
		return (locTx - locX28) / locL7, (locTy2 - locY10) / locL7
	}

	if locMe10.Pos[2]-locB43.W.GroundAt(locX28, locY10) > 3 && locB43.W.Obst[locB43.W.vidx(locX28, locY10)] {
		return locUnit(locGx4, locGy6)
	}
	if locD17 < 45 && locB43.W.LineWalkable(locX28, locY10, locGx4, locGy6) {
		return locUnit(locGx4, locGy6)
	}
	if locB43.path == nil || hyp(locGx4-locB43.pathGoal[0], locGy6-locB43.pathGoal[1]) > 5 || locB43.now-locB43.pathAt > 2.5 {
		locB43.path, _ = locB43.W.Path(locX28, locY10, locGx4, locGy6, 22000)
		locB43.pathGoal = [2]float64{locGx4, locGy6}
		locB43.pathAt = locB43.now
		locB43.pathI = 0
	}
	if len(locB43.path) == 0 {
		return locUnit(locGx4, locGy6)
	}

	locBi2, locBd2 := locB43.pathI, math.Inf(1)
	for locI38 := locB43.pathI; locI38 < len(locB43.path) && locI38 < locB43.pathI+10; locI38++ {
		locPx9, locPy13 := locB43.W.CellPos(int(locB43.path[locI38]))
		if locDd2 := hyp(locPx9-locX28, locPy13-locY10); locDd2 < locBd2 {
			locBi2, locBd2 = locI38, locDd2
		}
	}
	locB43.pathI = locBi2
	if locBd2 > 12 {
		locB43.path = nil
		return locUnit(locGx4, locGy6)
	}
	for locI32 := min(locBi2+14, len(locB43.path)-1); locI32 > locBi2; locI32-- {
		locPx6, locPy4 := locB43.W.CellPos(int(locB43.path[locI32]))
		if locB43.W.LineWalkable(locX28, locY10, locPx6, locPy4) {
			if locI32 == len(locB43.path)-1 && hyp(locPx6-locGx4, locPy4-locGy6) < 4 && locB43.W.LineWalkable(locX28, locY10, locGx4, locGy6) {
				return locUnit(locGx4, locGy6)
			}
			return locUnit(locPx6, locPy4)
		}
	}
	if locBi2+1 < len(locB43.path) {
		locPx11, locPy6 := locB43.W.CellPos(int(locB43.path[locBi2+1]))
		return locUnit(locPx11, locPy6)
	}
	return locUnit(locGx4, locGy6)
}

func (locB13 *Brain) prefRange(locMe11 *Self, locT9 *Track) float64 {
	_, locLight := hasWeapon(locMe11.Weapons, "lightning")
	_, locLaser2 := hasWeapon(locMe11.Weapons, "laser")
	locR17 := locB13.P.GunRange
	switch {
	case locLight != nil && locMe11.Energy > 12:
		locR17 = locB13.P.LightRange
	case locLaser2 != nil:
		locR17 = locB13.P.LaserRange
	}
	if locT9.Kind == "npc" {
		return 58
	}
	if locLight == nil {
		if holds(locT9.Weapons, "lightning") {
			locR17 = math.Max(locR17, 64)
		}
		if holds(locT9.Weapons, "mortar") {
			locR17 = math.Max(locR17, 80)
		}
	}
	return locR17
}

func (locB5 *Brain) fightMove(locO14 *Obs, locTg *Track, locSeen4 []*Track, locWantLos2 float64) (float64, float64) {
	locMe6 := &locO14.Self
	locX37, locY9 := locMe6.Pos[0], locMe6.Pos[1]
	locRp := locB5.prefRange(locMe6, locTg)
	locBx, locBy := locTg.Pos[0]-locX37, locTg.Pos[1]-locY9
	locBd := hyp(locBx, locBy) + 1e-9
	locUx5, locUy4 := locBx/locBd, locBy/locBd
	if locB5.now >= locB5.strafeUntil {
		if tweakFlipFast && locB5.pracOn {
			// reverse the strafe about every 0.5 s
			if locB5.rng.Float64() < 0.9 {
				locB5.strafe = -locB5.strafe
			}
			locB5.strafeUntil = locB5.now + 0.35 + locB5.rng.Float64()*0.3
		} else {
			if locB5.rng.Float64() < 0.6 {
				locB5.strafe = -locB5.strafe
			}
			locB5.strafeUntil = locB5.now + 0.3 + locB5.rng.Float64()*0.7
		}
	}
	locSx, locSy2 := -locUy4*locB5.strafe, locUx5*locB5.strafe
	locTc2 := locB5.center(locTg)
	locMh := locMe6.Stats.MuzzleHeight
	var locCube *known
	for _, locC27 := range locB5.cubes {
		if locD32 := hyp(locC27.Pos[0]-locX37, locC27.Pos[1]-locY9); locD32 < 40 && (locCube == nil || locD32 < hyp(locCube.Pos[0]-locX37, locCube.Pos[1]-locY9)) && math.Abs(locC27.Pos[2]-locMe6.Pos[2]) < 4 {
			locCube = locC27
		}
	}
	locBest6 := math.Inf(-1)
	var locMx6, locMy7 float64
	locBlockedSide := false
	locRings2 := 1
	if locB5.P.Peek {
		locRings2 = 2
	}
	for locK20 := -1; locK20 < 16*locRings2; locK20++ {
		locCx8, locCy3, locDx10, locDy16, locR := locX37, locY9, 0.0, 0.0, 0.0
		if locK20 >= 0 {
			locA6 := float64(locK20%16) * math.Pi / 8
			locR = 6 * float64(1+locK20/16)
			locDx10, locDy16 = math.Cos(locA6), math.Sin(locA6)
			locCx8, locCy3 = locX37+locDx10*locR, locY9+locDy16*locR
			if !locB5.W.LineWalkable(locX37, locY9, locCx8, locCy3) || !locB5.W.Free(locCx8, locCy3) {
				if locK20 < 16 && locDx10*locSx+locDy16*locSy2 > 0.9 {
					locBlockedSide = true
				}
				continue
			}
		}
		locCz := locB5.W.WalkAt(locCx8, locCy3)
		locD29 := hyp(locTg.Pos[0]-locCx8, locTg.Pos[1]-locCy3)
		locS12 := -1.1*math.Abs(locD29-locRp) - locR*0.4
		locLos := locB5.W.Clear(Vec3{locCx8, locCy3, locCz + locMh}, locTc2)
		if locLos {
			locS12 += 45 * locWantLos2
		}
		if locB5.P.Peek && locK20 >= 0 && locK20 < 16 && (locLos == (locWantLos2 > 0)) {

			for locQ3 := 0; locQ3 < 16; locQ3 += 2 {
				locQx, locQy := locCx8+math.Cos(float64(locQ3)*math.Pi/8)*7, locCy3+math.Sin(float64(locQ3)*math.Pi/8)*7
				if locB5.W.Free(locQx, locQy) && locB5.W.Clear(Vec3{locQx, locQy, locB5.W.WalkAt(locQx, locQy) + locMh}, locTc2) != locLos {
					locS12 += 8
					break
				}
			}
		}
		locBody := Vec3{locCx8, locCy3, locCz + locMe6.Stats.Height*0.6}
		for _, locE4 := range locSeen4 {
			if locE4 == locTg || hyp(locE4.Pos[0]-locCx8, locE4.Pos[1]-locCy3) > 170 {
				continue
			}
			if locB5.W.Clear(locB5.eye(locE4), locBody) {
				locS12 -= 30
			}
		}
		locS12 -= locB5.zonePenalty(&locO14.Zone, locCx8, locCy3)
		if locCube != nil {
			locS12 += 1.5 * (hyp(locCube.Pos[0]-locX37, locCube.Pos[1]-locY9) - hyp(locCube.Pos[0]-locCx8, locCube.Pos[1]-locCy3))
		}
		if locCl2 := float64(locB5.W.Clearance[locB5.W.Cell(locCx8, locCy3)]); locCl2 < 4 {
			locS12 -= (4 - locCl2) * 2.5
		}
		if locWantLos2 > 0 {
			locS12 += 9 * (locDx10*locSx + locDy16*locSy2)
		}
		if locK20 < 0 {
			locS12 -= 8
			if locWantLos2 < 0 && !locLos {
				locS12 += 14
			}
		}
		if locS12 > locBest6 {
			locBest6, locMx6, locMy7 = locS12, locDx10, locDy16
		}
	}
	if locBlockedSide {
		locB5.strafe = -locB5.strafe
		locB5.strafeUntil = locB5.now + 0.5
	}
	return locMx6, locMy7
}

func (locB10 *Brain) coverMove(locO6 *Obs, locSeen5 []*Track, locMx5, locMy5 *float64) bool {
	locMe19 := &locO6.Self
	locX30, locY36 := locMe19.Pos[0], locMe19.Pos[1]
	var locThreats []*Track
	for _, locT14 := range locSeen5 {
		if hyp(locT14.Pos[0]-locX30, locT14.Pos[1]-locY36) < 230 {
			locThreats = append(locThreats, locT14)
		}
	}
	if len(locThreats) == 0 {
		return false
	}
	if len(locThreats) > 4 {
		sort.Slice(locThreats, func(locI43, locJ6 int) bool {
			return hyp(locThreats[locI43].Pos[0]-locX30, locThreats[locI43].Pos[1]-locY36) < hyp(locThreats[locJ6].Pos[0]-locX30, locThreats[locJ6].Pos[1]-locY36)
		})
		locThreats = locThreats[:4]
	}
	locHidden := func(locCx2, locCy8 float64) bool {
		locCz2 := locB10.W.WalkAt(locCx2, locCy8)
		for _, locT33 := range locThreats {
			locE3 := locB10.eye(locT33)
			if locB10.W.Clear(locE3, Vec3{locCx2, locCy8, locCz2 + locMe19.Stats.Height}) || locB10.W.Clear(locE3, Vec3{locCx2, locCy8, locCz2 + locMe19.Stats.Height*0.5}) {
				return false
			}
		}
		return true
	}
	if locB10.coverOK && locB10.now-locB10.coverAt < 0.4 {
		*locMx5, *locMy5 = locB10.travelCover(locMe19)
		return true
	}
	locB10.coverAt = locB10.now
	if locHidden(locX30, locY36) && locB10.zonePenalty(&locO6.Zone, locX30, locY36) == 0 {
		locB10.coverOK = true
		locB10.coverGoal = [2]float64{locX30, locY36}
		*locMx5, *locMy5 = 0, 0
		return true
	}

	locW60 := locB10.W
	locS18 := locW60.NearestFree(locW60.Cell(locX30, locY36))
	if locS18 < 0 {
		return false
	}
	locW60.gen++
	locGen2 := locW60.gen
	locQueue := []int32{int32(locS18)}
	locW60.stamp[locS18] = locGen2
	locW60.g[locS18] = 0
	locBestC, locBestS := -1, math.Inf(1)
	locChecked := 0
	for locH7 := 0; locH7 < len(locQueue) && locChecked < 350; locH7++ {
		locC43 := int(locQueue[locH7])
		locCi6, locCj := locC43%locW60.NNX, locC43/locW60.NNX
		locSteps := float64(locW60.g[locC43])
		if locSteps > 50 || (locBestC >= 0 && locSteps*2 > locBestS+30) {
			break
		}
		if (locCi6+locCj)%2 == 0 && locSteps > 0 {
			locCx6, locCy7 := locW60.CellPos(locC43)
			locChecked++
			if locHidden(locCx6, locCy7) {
				locSc2 := locSteps*2 + locB10.zonePenalty(&locO6.Zone, locCx6, locCy7)
				locNear := math.Inf(1)
				for _, locT10 := range locThreats {
					locNear = math.Min(locNear, hyp(locT10.Pos[0]-locCx6, locT10.Pos[1]-locCy7))
				}
				locSc2 -= math.Min(locNear, 80) * 0.25
				if locSc2 < locBestS {
					locBestC, locBestS = locC43, locSc2
				}
			}
		}
		for locK5, locO22 := range nbrs {
			locI33, locJ10 := locCi6+locO22[0], locCj+locO22[1]
			if locI33 < 0 || locJ10 < 0 || locI33 >= locW60.NNX || locJ10 >= locW60.NNY {
				continue
			}
			locNb := locJ10*locW60.NNX + locI33
			if locW60.stamp[locNb] == locGen2 || !locW60.canStep(locC43, locNb, locK5 >= 4) {
				continue
			}
			if locK5 >= 4 && (locW60.Blocked[locCj*locW60.NNX+locI33] || locW60.Blocked[locJ10*locW60.NNX+locCi6]) {
				continue
			}
			locW60.stamp[locNb] = locGen2
			locW60.g[locNb] = locW60.g[locC43] + 1
			locQueue = append(locQueue, int32(locNb))
		}
	}
	if locBestC < 0 {
		locB10.coverOK = false
		return false
	}
	locGx5, locGy2 := locW60.CellPos(locBestC)
	locB10.coverOK = true
	locB10.coverGoal = [2]float64{locGx5, locGy2}
	*locMx5, *locMy5 = locB10.travelCover(locMe19)
	return true
}

func (locB *Brain) travelCover(locMe20 *Self) (float64, float64) {
	if hyp(locB.coverGoal[0]-locMe20.Pos[0], locB.coverGoal[1]-locMe20.Pos[1]) < 1.5 {
		return 0, 0
	}
	return locB.travel(locMe20, locB.coverGoal[0], locB.coverGoal[1])
}

type lootChoice struct {
	kind  string
	id    int
	pos   Vec3
	value float64
}

func (locB47 *Brain) chooseLoot(locO2 *Obs, locHpFrac float64, locDanger bool) *lootChoice {
	locMe4 := &locO2.Self
	locX27, locY39 := locMe4.Pos[0], locMe4.Pos[1]
	locZ9 := &locO2.Zone
	locNow4 := locB47.now
	locCur5 := locB47.curZone(locZ9)
	var locBest2 *lootChoice
	locBestU := 0.0
	locEcon := locB47.P.Econ
	locSquadDrops := tweakDropV > 0 && locB47.pracOn
	locConsider := func(locKind4 string, locId9 int, locPos2 Vec3, locValue float64) {
		if locUntil2, locOk11 := locB47.bad[locId9]; locOk11 && locUntil2 > locNow4 {
			return
		}
		locD31 := hyp(locPos2[0]-locX27, locPos2[1]-locY39)
		if locD31 > 260 {
			return
		}
		if hyp(locPos2[0]-locCur5.X, locPos2[1]-locCur5.Y) > locCur5.R-6 {
			return
		}

		if locDl := locB47.zoneDeadline(locZ9, locPos2[0], locPos2[1], 6); locNow4+locD31*1.4/locMe4.Stats.WalkSpeed+5 > locDl {
			return
		}

		if locPos2[2]-locB47.W.GroundAt(locPos2[0], locPos2[1]) > 3.2 {
			return
		}
		if locNf2 := locB47.W.NearestFree(locB47.W.Cell(locPos2[0], locPos2[1])); locNf2 < 0 || locB47.W.Comp[locNf2] != locB47.W.MainComp {
			return
		}
		locU5 := locValue / (locD31 + 18)
		if locId9 == locB47.lootID {
			locU5 *= 1.35
		}
		if locEcon {
			locU5 = locValue - 0.6*locD31
			if locId9 == locB47.lootID {
				locU5 += 30
			}
		}
		if locU5 > locBestU {
			locBestU = locU5
			locBest2 = &lootChoice{locKind4, locId9, locPos2, locValue}
		}
	}
	if locEcon {
		locMechNear := false
		for _, locT42 := range locB47.tracks {
			if locT42.Seen == locNow4 {
				locMechNear = true
			}
		}
		locHurt := locHpFrac <= 0.45
		locNearOnly := false
		for _, locT46 := range locB47.tracks {
			if locB47.P.EconSafe > 0 && locNow4-locT46.Seen < locB47.P.EconSafe {
				locNearOnly = true
			}
		}
		locInner := locConsider
		locConsider = func(locKind3 string, locId int, locPos Vec3, locValue2 float64) {
			if locNearOnly && hyp(locPos[0]-locX27, locPos[1]-locY39) > 60 {
				return
			}
			locInner(locKind3, locId, locPos, locValue2)
		}
		for _, locP19 := range locB47.pickups {
			if !locB47.wantPickup(locMe4, locP19.Kind) || locHurt {
				continue
			}
			locV3 := 150.0
			if len(locMe4.Weapons) > 1 && locMe4.Weapons[1] == nil {
				locV3 = 260
			}
			if locP19.Kind == "laser" {
				locV3 = math.Max(locV3, 270) + 20
			}
			locConsider("pickup", locP19.ID, locP19.Pos, locV3)
		}
		for _, locC25 := range locB47.cubes {
			if !locHurt || hyp(locC25.Pos[0]-locX27, locC25.Pos[1]-locY39) < 35 {
				locConsider("cube", locC25.ID, locC25.Pos, 110)
			}
		}
		for _, locC40 := range locB47.crates {
			if !locHurt || hyp(locC40.Pos[0]-locX27, locC40.Pos[1]-locY39) < 35 {
				locConsider("crate", locC40.ID, locC40.Pos, 105)
			}
		}
		if locB47.P.FarmNPC && locHpFrac > 0.6 && !locMechNear {
			for _, locN17 := range locB47.npcs {
				locV6 := 170.0
				if locNow4-locN17.Seen > 12 {
					locV6 = 155
				}
				locConsider("npc", locN17.ID, locN17.Pos, locV6)
			}
		}
		for locI20 := range locO2.Drops {
			locD34 := &locO2.Drops[locI20]
			if locB47.P.UseDrops && !locHurt && hyp(locD34.Pos[0]-locX27, locD34.Pos[1]-locY39)/locMe4.Stats.WalkSpeed < locD34.LandsIn+25 {
				locConsider("drop", locD34.ID, locD34.Pos, 170)
			}
		}
		if locBest2 == nil {
			locB47.lootID = -1
			return nil
		}
		if locBest2.id != locB47.lootID {
			locB47.lootID = locBest2.id
			locB47.lootSince = locNow4
		} else if locLim2 := 30.0; locNow4-locB47.lootSince > locLim2 || (locBest2.kind == "crate" && locNow4-locB47.lootSince > locLim2-6 && hyp(locBest2.pos[0]-locX27, locBest2.pos[1]-locY39) < 50) {
			locB47.bad[locBest2.id] = locNow4 + 60
			locB47.lootID = -1
			return nil
		}
		return locBest2
	}
	for _, locP2 := range locB47.pickups {
		locRad := locB47.P.LootRadius
		locSeek := locP2.Kind == "laser" && locB47.P.LaserSeek > locRad
		if locSeek {
			locRad = locB47.P.LaserSeek
		}
		if locB47.wantPickup(locMe4, locP2.Kind) && hyp(locP2.Pos[0]-locX27, locP2.Pos[1]-locY39) < locRad {
			locV7 := 60.0
			if len(locMe4.Weapons) > 1 && locMe4.Weapons[1] == nil {
				locV7 = 130
			}
			if locSeek {
				locV7 = math.Max(locV7, 150)
			}
			locConsider("pickup", locP2.ID, locP2.Pos, locV7)
		}
	}
	locCubeV := 75.0
	if locMe4.Cubes >= locB47.cubeCap() {
		locCubeV = 25
	}
	for _, locC19 := range locB47.cubes {
		if hyp(locC19.Pos[0]-locX27, locC19.Pos[1]-locY39) < math.Max(60, locB47.P.LootRadius*0.7) {
			locConsider("cube", locC19.ID, locC19.Pos, locCubeV)
		}
	}
	locFarmHP := 0.7
	if locB47.P.FarmHP > 0 {
		locFarmHP = locB47.P.FarmHP
	}
	if locHpFrac > locFarmHP && !locDanger && locNow4 < locB47.P.CrateUntil {
		for _, locC23 := range locB47.crates {
			if hyp(locC23.Pos[0]-locX27, locC23.Pos[1]-locY39) < locB47.P.LootRadius {
				locConsider("crate", locC23.ID, locC23.Pos, locCubeV*0.6)
			}
		}
		if locB47.P.FarmNPC && locMe4.Cubes < locB47.cubeCap() {
			for _, locN14 := range locB47.npcs {
				if locNow4-locN14.Seen < 12 && hyp(locN14.Pos[0]-locX27, locN14.Pos[1]-locY39) < locB47.P.LootRadius {
					locConsider("npc", locN14.ID, locN14.Pos, 85)
				}
			}
		}
	}
	for locI35 := range locO2.Drops {
		locD22 := &locO2.Drops[locI35]
		locDist := hyp(locD22.Pos[0]-locX27, locD22.Pos[1]-locY39)
		if locB47.P.UseDrops && locDist/locMe4.Stats.WalkSpeed < locD22.LandsIn+25 && !locDanger {
			locConsider("drop", locD22.ID, locD22.Pos, 260)
		}
		if locSquadDrops && !locB47.P.UseDrops && !locDanger {
			// go when we would arrive at most 3 s before it lands; skip it if another mech holds or stands at it
			locEta := math.Max(0, locDist-locB47.dropRadius(locD22)*0.5) / math.Max(1, locMe4.Stats.WalkSpeed)
			if locD22.LandsIn <= locEta+3 && (locDist < locB47.dropRadius(locD22) || locB47.dropFree(locD22, locMe4.ID, 6)) {
				locConsider("drop", locD22.ID, locD22.Pos, tweakDropV*locB47.dropWorth(locMe4, locHpFrac))
			}
		}
	}
	if locBest2 == nil {
		locB47.lootID = -1
		return nil
	}
	locLim := 30.0
	if locSquadDrops && locBest2.kind == "drop" {
		locLim = 60
	}
	if locBest2.id != locB47.lootID {
		locB47.lootID = locBest2.id
		locB47.lootSince = locNow4
	} else if locNow4-locB47.lootSince > locLim {
		locB47.bad[locBest2.id] = locNow4 + 60
		locB47.lootID = -1
		return nil
	}
	return locBest2
}

func (locB14 *Brain) dropRadius(locD3 *Drop) float64 {
	switch {
	case locD3.Radius > 0:
		return locD3.Radius
	case locB14.Cfg.Drops.Radius > 0:
		return locB14.Cfg.Drops.Radius
	}
	return 8
}

// dropFree: no other mech holds the capture clock or was seen within the ring (plus pad) in the last 3 s.
func (locB58 *Brain) dropFree(locD20 *Drop, locMyID int, locPad float64) bool {
	if locD20.Holder != nil && *locD20.Holder != locMyID {
		return false
	}
	locR9 := locB58.dropRadius(locD20) + locPad
	for _, locT11 := range locB58.tracks {
		if locB58.now-locT11.Seen < 3 && hyp(locT11.Pos[0]-locD20.Pos[0], locT11.Pos[1]-locD20.Pos[1]) < locR9 {
			return false
		}
	}
	return true
}

// dropWorth: a drop's cubes (valued like loose cubes, 75 below the stat cap, 25 above) plus the HP it refills.
func (locB4 *Brain) dropWorth(locMe12 *Self, locHpFrac4 float64) float64 {
	locN12 := locB4.Cfg.Drops.Cubes
	if locN12 <= 0 {
		locN12 = 3
	}
	locUnder := min(max(locB4.Cfg.Cubes.StatCap-locMe12.Cubes, 0), locN12)
	return 75*float64(locUnder) + 25*float64(locN12-locUnder) + 400*clamp(1-locHpFrac4, 0, 1)
}

// fightDrop: the nearest supply drop within tweakFightDropR that is on the ground by the time we get there,
// not held or stood on by another mech (unless we are already in its ring), and safe from the zone until captured.
func (locB12 *Brain) fightDrop(locO24 *Obs, locHpFrac2 float64) *Drop {
	locMe := &locO24.Self
	if locHpFrac2 >= 0.9 && locMe.Cubes >= locB12.Cfg.Cubes.StatCap {
		return nil
	}
	locX34, locY45 := locMe.Pos[0], locMe.Pos[1]
	locZ29 := &locO24.Zone
	locCur2 := locB12.curZone(locZ29)
	locWalk := math.Max(1, locMe.Stats.WalkSpeed)
	var locBest9 *Drop
	locBestD := tweakFightDropR
	for locI23 := range locO24.Drops {
		locD4 := &locO24.Drops[locI23]
		locDd := hyp(locD4.Pos[0]-locX34, locD4.Pos[1]-locY45)
		if locDd > locBestD {
			continue
		}
		locR18 := locB12.dropRadius(locD4)
		locEta2 := math.Max(0, locDd-locR18*0.5) / locWalk
		if locD4.LandsIn > locEta2+0.5 {
			continue
		}
		if locDd >= locR18 && !locB12.dropFree(locD4, locMe.ID, 4) {
			continue
		}
		if hyp(locD4.Pos[0]-locCur2.X, locD4.Pos[1]-locCur2.Y) > locCur2.R-6 {
			continue
		}
		locCapS := locD4.CaptureS
		if locCapS <= 0 {
			locCapS = locB12.Cfg.Drops.CaptureS
		}
		if locD4.Holder != nil && *locD4.Holder == locMe.ID {
			locCapS -= locD4.Progress
		}
		if locB12.zoneDeadline(locZ29, locD4.Pos[0], locD4.Pos[1], 6) < locB12.now+locEta2+locCapS+2 {
			continue
		}
		if locNf3 := locB12.W.NearestFree(locB12.W.Cell(locD4.Pos[0], locD4.Pos[1])); locNf3 < 0 || locB12.W.Comp[locNf3] != locB12.W.MainComp {
			continue
		}
		locBest9, locBestD = locD4, locDd
	}
	return locBest9
}

func (locB39 *Brain) exploreMove(locO3 *Obs) (float64, float64) {
	locMe3 := &locO3.Self
	locX38, locY25 := locMe3.Pos[0], locMe3.Pos[1]
	locZ15 := &locO3.Zone
	if locB39.now > locB39.exploreUntil || hyp(locB39.explore[0]-locX38, locB39.explore[1]-locY25) < 6 {
		for locTry := 0; locTry < 20; locTry++ {
			locA17 := locB39.rng.Float64() * 2 * math.Pi
			locR3 := math.Sqrt(locB39.rng.Float64()) * locZ15.NextRadius * 0.8
			locPx5, locPy2 := locZ15.NextCenter[0]+math.Cos(locA17)*locR3, locZ15.NextCenter[1]+math.Sin(locA17)*locR3
			if !locB39.W.InBounds(locPx5, locPy2) || !locB39.W.Free(locPx5, locPy2) {
				continue
			}
			if locD12 := hyp(locPx5-locX38, locPy2-locY25); locD12 < 50 || locD12 > 220 {
				continue
			}
			locB39.explore = [2]float64{locPx5, locPy2}
			locB39.exploreUntil = locB39.now + 30
			break
		}
	}
	if locB39.now > locB39.exploreUntil {
		return 0, 0
	}
	return locB39.travel(locMe3, locB39.explore[0], locB39.explore[1])
}

func (locB16 *Brain) homeCost(locO20 *Obs, locPx, locPy5 float64) float64 {
	locZ34 := &locO20.Zone
	locCrowd2 := 0.0
	for _, locT27 := range locB16.tracks {
		if locB16.now-locT27.Seen < 20 {
			locCrowd2 += math.Max(0, 80-hyp(locT27.Pos[0]-locPx, locT27.Pos[1]-locPy5)) / 80
		}
	}
	for locI50 := range locO20.Drops {
		locCrowd2 += math.Max(0, 60-hyp(locO20.Drops[locI50].Pos[0]-locPx, locO20.Drops[locI50].Pos[1]-locPy5)) / 60
	}
	locEdge := hyp(locPx-locZ34.NextCenter[0], locPy5-locZ34.NextCenter[1]) / math.Max(30, locZ34.NextRadius)
	return locB16.P.HomeExpo*locB16.W.Exposure(locPx, locPy5) + 0.25*hyp(locPx-locO20.Self.Pos[0], locPy5-locO20.Self.Pos[1]) + 40*locCrowd2 + 10*locEdge
}

func (locB49 *Brain) home(locO17 *Obs) (float64, float64, bool) {
	locZ11 := &locO17.Zone
	locOut := !locB49.homeSet || hyp(locB49.homeXY[0]-locZ11.NextCenter[0], locB49.homeXY[1]-locZ11.NextCenter[1]) > locZ11.NextRadius*0.8
	if locOut || locB49.now-locB49.homeAt > 12 {
		locB49.homeAt = locB49.now
		locBest10, locBestC2 := [2]float64{}, math.Inf(1)
		for locK23 := 0; locK23 < 40; locK23++ {
			locA7 := locB49.rng.Float64() * 2 * math.Pi
			locR2 := math.Sqrt(locB49.rng.Float64()) * locZ11.NextRadius * 0.7
			locPx14, locPy14 := locZ11.NextCenter[0]+math.Cos(locA7)*locR2, locZ11.NextCenter[1]+math.Sin(locA7)*locR2
			if !locB49.W.InBounds(locPx14, locPy14) || !locB49.W.Reachable(locPx14, locPy14) {
				continue
			}
			if locC45 := locB49.homeCost(locO17, locPx14, locPy14); locC45 < locBestC2 {
				locBest10, locBestC2 = [2]float64{locPx14, locPy14}, locC45
			}
		}
		if !math.IsInf(locBestC2, 1) && (locOut || locBestC2 < locB49.homeCost(locO17, locB49.homeXY[0], locB49.homeXY[1])-15) {
			locB49.homeXY, locB49.homeSet = locBest10, true
		}
	}
	return locB49.homeXY[0], locB49.homeXY[1], locB49.homeSet
}

// backTo: the track's torso faces away from (x, y) by a margin, so our hits land on its rear armor.
func (locB17 *Brain) backTo(locT28 *Track, locX54, locY19 float64) bool {
	locArc := locB17.Cfg.Mech.FrontArcDeg
	if locArc <= 0 {
		locArc = 180
	}
	locToMe := math.Atan2(locY19-locT28.Pos[1], locX54-locT28.Pos[0])
	return math.Abs(angDiff(locT28.Aim[0], locToMe)) > locArc*math.Pi/360+0.3
}

// mechSounds: sounds only mechs make (weapons, steps, boosts, jets, reloads).
var mechSounds = map[string]bool{"gun": true, "mortar": true, "laser_charge": true, "laser": true, "lightning": true, "step": true, "boost": true, "jet": true, "reload": true}

// hear remembers the latest enemy sound that no visible mech explains (within 160 m), and side-steps an unseen laser charge.
func (locB34 *Brain) hear(locO23 *Obs) {
	locMe14 := &locO23.Self
	locNow2 := locO23.T
	for _, locS6 := range locO23.Sounds {
		if !mechSounds[locS6.Kind] || locS6.Dist == "far" {
			continue
		}
		locExplained := false
		for _, locT := range locB34.tracks {
			if locT.Seen == locNow2 && math.Abs(angDiff(math.Atan2(locT.Pos[1]-locMe14.Pos[1], locT.Pos[0]-locMe14.Pos[0]), locS6.Bearing)) < 0.35 {
				locExplained = true
				break
			}
		}
		if locExplained {
			continue
		}
		if locAt := locNow2 - locS6.Age; locAt > locB34.heardAt {
			locB34.heardAt, locB34.heardBearing = locAt, locS6.Bearing
		}
		if locS6.Kind == "laser_charge" && locNow2-locS6.Age > locB34.hearDodgeUntil-0.8 {
			locSide2 := 1.0
			if locB34.rng.Float64() < 0.5 {
				locSide2 = -1
			}
			locB34.hearDodgeX, locB34.hearDodgeY = -math.Sin(locS6.Bearing)*locSide2, math.Cos(locS6.Bearing)*locSide2
			locB34.hearDodgeUntil = locNow2 + 0.8
		}
	}
}

func (locB37 *Brain) hideFromBearing(locO12 *Obs, locSeen2 []*Track, locMx11, locMy3 *float64) bool {
	locMe17 := &locO12.Self
	locGhost := &Track{}
	locGhost.Pos = Vec3{locMe17.Pos[0] + math.Cos(locB37.hurtBearing)*80, locMe17.Pos[1] + math.Sin(locB37.hurtBearing)*80, locMe17.Pos[2]}
	return locB37.coverMove(locO12, append([]*Track{locGhost}, locSeen2...), locMx11, locMy3)
}

func (locB22 *Brain) dodge(locO8 *Obs) (locDx9, locDy2 float64, locLaser3 bool) {
	locMe2 := &locO8.Self
	locCx4, locCy5, locCz3 := locMe2.Pos[0], locMe2.Pos[1], locMe2.Pos[2]+locMe2.Stats.Height*0.56
	locReach2 := locMe2.Stats.Radius + 1.1
	for locI42 := range locO8.Projectiles {
		locP13 := &locO8.Projectiles[locI42]
		if locP13.Kind == "mortar" {
			continue
		}
		locV22 := locP13.Vel[0]*locP13.Vel[0] + locP13.Vel[1]*locP13.Vel[1] + locP13.Vel[2]*locP13.Vel[2]
		if locV22 < 1 {
			continue
		}
		locTc := ((locCx4-locP13.Pos[0])*locP13.Vel[0] + (locCy5-locP13.Pos[1])*locP13.Vel[1] + (locCz3-locP13.Pos[2])*locP13.Vel[2]) / locV22
		if locTc <= 0 || locTc > 0.7 {
			continue
		}
		locMxx, locMyy := locP13.Pos[0]+locP13.Vel[0]*locTc-locCx4, locP13.Pos[1]+locP13.Vel[1]*locTc-locCy5
		if hyp(locMxx, locMyy) < locReach2 {
			locVh := hyp(locP13.Vel[0], locP13.Vel[1])
			if locVh < 1 {
				continue
			}
			locPx10, locPy9 := -locP13.Vel[1]/locVh, locP13.Vel[0]/locVh
			if locPx10*locMxx+locPy9*locMyy > 0 {
				locPx10, locPy9 = -locPx10, -locPy9
			}
			locDx9 += locPx10
			locDy2 += locPy9
		}
	}
	for locI52 := range locO8.Mechs {
		locM4 := &locO8.Mechs[locI52]
		if locM4.Kind != "mech" || !locM4.Charging {
			continue
		}
		locToMe4 := math.Atan2(locCy5-locM4.Pos[1], locCx4-locM4.Pos[0])
		if math.Abs(angDiff(locM4.Aim[0], locToMe4)) > 0.15 {
			continue
		}
		locDx9 += math.Cos(locToMe4+math.Pi/2*locB22.strafe) * 1.5
		locDy2 += math.Sin(locToMe4+math.Pi/2*locB22.strafe) * 1.5
		locLaser3 = true
	}
	if locL6 := hyp(locDx9, locDy2); locL6 > 0.01 {
		return locDx9 / locL6, locDy2 / locL6, locLaser3
	}
	return 0, 0, false
}

func (locB33 *Brain) reach(locMe7 *Self) float64 {
	locR19 := 0.0
	for _, locW28 := range locMe7.Weapons {
		if locW28 == nil {
			continue
		}
		switch locW28.Kind {
		case "gun":
			locR19 = math.Max(locR19, math.Min(locW28.Stats.Range-10, 130))
		case "laser":
			locR19 = math.Max(locR19, locW28.Stats.Range-5)
		case "lightning":
			locR19 = math.Max(locR19, locW28.Stats.Range)
		}
	}
	return locR19
}

func (locB53 *Brain) odds(locMe18 *Self, locT41 *Track, locNow5 float64, locOpening bool) float64 {
	locMyW, locThW := 0, len(locT41.Weapons)
	for _, locW41 := range locMe18.Weapons {
		if locW41 != nil {
			locMyW++
		}
	}
	if locThW == 0 {
		locThW = 1
	}
	locCapc := func(locC35 int) float64 { return math.Min(float64(locC35), 10) }
	locBusyMe, locBusyThem := 0.0, 0.0
	for _, locU := range locB53.tracks {
		if locU.ID == locT41.ID || locNow5-locU.Seen > 2.5 {
			continue
		}
		if locNow5-locU.HitMe < 6 {
			locBusyMe = 1
		}
		locBr := math.Atan2(locU.Pos[1]-locT41.Pos[1], locU.Pos[0]-locT41.Pos[0])
		if math.Abs(angDiff(locT41.Aim[0], locBr)) < 0.2 && hyp(locU.Pos[0]-locT41.Pos[0], locU.Pos[1]-locT41.Pos[1]) < 160 {
			locBusyThem = 1
		}
	}
	locZ12 := 1.5093*math.Log(math.Max(locMe18.HP, 1)/math.Max(locT41.HP, 1)) + 0.7049*(locCapc(locMe18.Cubes)-locCapc(locT41.Cubes))/10 +
		0.9583*float64(locMyW-locThW) - 0.5592*(locBusyMe-locBusyThem)
	if locOpening {
		locZ12 += 0.0562 + 0.7475*math.Min(locT41.Dist, 200)/100
		locToMe3 := math.Atan2(locMe18.Pos[1]-locT41.Pos[1], locMe18.Pos[0]-locT41.Pos[0])
		if math.Abs(angDiff(locT41.Aim[0], locToMe3)) > 1.75 {
			locZ12 += 0.8235
		}
	}
	return 1 / (1 + math.Exp(-locZ12))
}

func squadParams(locP11 Params) Params {
	locP11.Ghost = false
	locP11.Peek = false
	locP11.LaserFirst = true
	locP11.LaserRange = 120
	locP11.LootRadius = 150
	locP11.CrateUntil = 1e9
	locP11.Stick = 120
	locP11.HPw = 0.2
	locP11.LowBonus = 100
	locP11.MaxTarget = 240
	locP11.LoseRule = true
	locP11.FightLoot = 70
	locP11.FarmHP = 0.5

	locP11.LaserSeek = 240
	locP11.LaserUp = true
	locP11.Lethal = true
	locP11.Finish = 14
	locP11.Pursue = 1.01

	locP11.Finish, locP11.FinishHP, locP11.FinishRush, locP11.Pursue, locP11.Stick = tw.finish, tw.finishHP, tw.finishRush, tw.pursue, tw.stick
	if tweakHPw > 0 {
		locP11.HPw = tweakHPw
	}
	if tweakLowBonus > 0 {
		locP11.LowBonus = tweakLowBonus
	}
	return locP11
}

func (locB23 *Brain) cubeCap() int {
	locC2 := locB23.Cfg.Cubes.StatCap
	if locG8 := int(locB23.P.CubeGoal); locG8 > locC2 {
		locC2 = locG8
	}
	return locC2
}
