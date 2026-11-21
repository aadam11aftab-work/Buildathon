package mech

// Offline checks of the tweaks on the kit's recorded stream (check/stream.jsonl.gz, a free-for-all match).
// Run with the kit path in MECHFIGHT_KIT, for example in Docker:
//   docker run --rm -v "$PWD/engine:/src" -v "<kit>:/kit:ro" -w /src -e MECHFIGHT_KIT=/kit golang:1.26.1-bookworm go test ./mech/

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func kitDir(locT55 *testing.T) string {
	locD14 := os.Getenv("MECHFIGHT_KIT")
	if locD14 == "" {
		locT55.Skip("MECHFIGHT_KIT not set (path to mechfight-kit)")
	}
	return locD14
}

type stream struct {
	hello []byte
	obs   [][]byte
}

func loadStream(locT37 *testing.T, locKit4 string) stream {
	locF4, locErr2 := os.Open(filepath.Join(locKit4, "check", "stream.jsonl.gz"))
	if locErr2 != nil {
		locT37.Fatal(locErr2)
	}
	defer locF4.Close()
	locGz, locErr2 := gzip.NewReader(locF4)
	if locErr2 != nil {
		locT37.Fatal(locErr2)
	}
	var locS8 stream
	locSc := bufio.NewScanner(locGz)
	locSc.Buffer(make([]byte, 1<<20), 1<<22)
	for locSc.Scan() {
		locLine := append([]byte(nil), locSc.Bytes()...)
		var locP20 struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(locLine, &locP20) != nil {
			continue
		}
		switch locP20.Type {
		case "hello":
			locS8.hello = locLine
		case "obs":
			locS8.obs = append(locS8.obs, locLine)
		}
	}
	if locS8.hello == nil || len(locS8.obs) == 0 {
		locT37.Fatal("empty stream")
	}
	return locS8
}

type run struct {
	acts        []Action
	modes       []string
	ts          []float64
	panics      int
	notPractice bool
	squadTicks  int
	worst       time.Duration
}

// play feeds the stream (open loop) to a fresh brain using the given tweaks; edit may change each observation.
func play(locT50 *testing.T, locKit string, locS10 stream, locSet tweakSet, locEdit func(*Obs)) run {
	locOld := tw
	tw = locSet
	defer func() { tw = locOld }()
	var locH16 Hello
	if locErr4 := json.Unmarshal(locS10.hello, &locH16); locErr4 != nil {
		locT50.Fatal(locErr4)
	}
	locW25, locErr5 := LoadWorld(filepath.Join(locKit, "arena"))
	if locErr5 != nil {
		locT50.Fatal(locErr5)
	}
	locB55 := NewBrain(&locH16, locW25)
	var locR20 run
	for _, locRaw := range locS10.obs {
		var locO9 Obs
		if locErr := json.Unmarshal(locRaw, &locO9); locErr != nil {
			locT50.Fatal(locErr)
		}
		if locEdit != nil {
			locEdit(&locO9)
		}
		locT0 := time.Now()
		locA9 := locB55.Act(&locO9)
		if locDt := time.Since(locT0); locDt > locR20.worst {
			locR20.worst = locDt
		}
		if locA9.Tick != locO9.Tick {
			locT50.Fatalf("tick %d answered as %d", locO9.Tick, locA9.Tick)
		}
		locBad := func(locV float64) bool { return math.IsNaN(locV) || math.IsInf(locV, 0) }
		if locBad(locA9.Move[0]) || locBad(locA9.Move[1]) || math.Hypot(locA9.Move[0], locA9.Move[1]) > 1.0001 {
			locT50.Fatalf("tick %d: bad move %v", locO9.Tick, locA9.Move)
		}
		if locA9.Aim == nil || locBad(locA9.Aim[0]) || locBad(locA9.Aim[1]) {
			locT50.Fatalf("tick %d: bad aim %v", locO9.Tick, locA9.Aim)
		}
		if _, locErr6 := json.Marshal(locA9); locErr6 != nil {
			locT50.Fatalf("tick %d: %v", locO9.Tick, locErr6)
		}
		if locB55.pracOn {
			locR20.squadTicks++
		}
		locR20.acts = append(locR20.acts, locA9)
		locR20.modes = append(locR20.modes, locB55.Mode)
		locR20.ts = append(locR20.ts, locO9.T)
	}
	locR20.panics, locR20.notPractice = locB55.Panics, locB55.notPractice
	if locR20.panics > 0 {
		locT50.Fatalf("%d panics recovered", locR20.panics)
	}
	return locR20
}

func diffTicks(locA28, locB52 run) (locN7 int, locFirst2 float64) {
	locFirst2 = -1
	for locI47 := range locA28.acts {
		if !reflect.DeepEqual(locA28.acts[locI47], locB52.acts[locI47]) {
			if locN7 == 0 {
				locFirst2 = locA28.ts[locI47]
			}
			locN7++
		}
	}
	return
}

func modeCount(locR21 run) string {
	locC42 := map[string]int{}
	for locI14, locM7 := range locR21.modes {
		locK14 := locM7
		if locR21.acts[locI14].Boost {
			locK14 += "+boost"
		}
		locC42[locK14]++
	}
	var locKs []string
	for locK7 := range locC42 {
		locKs = append(locKs, locK7)
	}
	sort.Strings(locKs)
	var locSb strings.Builder
	for _, locK2 := range locKs {
		fmt.Fprintf(&locSb, "%s=%d ", locK2, locC42[locK2])
	}
	return locSb.String()
}

// TestParams70c8b23 pins the free-for-all base and the original squad parameters.
func TestParams70c8b23(locT20 *testing.T) {
	locWant := Params{RetreatHP: 0.38, RecoverHP: 0.8, GunRange: 85, LightRange: 28, LaserRange: 85, EngageDist: 60, FarmNPC: true,
		Ghost: true, HomeExpo: 120, LootRadius: 90, CrateUntil: 150, Dodge: true, NoMortar: true,
		Peek: true, LaserFirst: true, CrowdHide: true, Practice: true}
	if locGot := DefaultParams(); locGot != locWant {
		locT20.Fatalf("DefaultParams changed (free-for-all base):\n got %+v\nwant %+v", locGot, locWant)
	}
	locOld2 := tw
	tw = originalTweaks()
	defer func() { tw = locOld2 }()
	locSq := locWant
	locSq.Ghost, locSq.Peek, locSq.LaserFirst, locSq.LaserRange, locSq.LootRadius, locSq.CrateUntil = false, false, true, 120, 150, 1e9
	locSq.Stick, locSq.HPw, locSq.LowBonus, locSq.MaxTarget, locSq.LoseRule, locSq.FightLoot, locSq.FarmHP = 120, 0.2, 100, 240, true, 70, 0.5
	locSq.LaserSeek, locSq.LaserUp, locSq.Lethal, locSq.Finish, locSq.Pursue = 240, true, true, 14, 1.01
	if locGot2 := squadParams(DefaultParams()); locGot2 != locSq {
		locT20.Fatalf("squadParams with original tweaks differs from 70c8b23:\n got %+v\nwant %+v", locGot2, locSq)
	}
	locT20.Logf("tweaks in use: %+v", locOld2)
}

// TestFreeForAllUnchanged: on the recorded free-for-all match the tweaks in use must play exactly like 70c8b23.
func TestFreeForAllUnchanged(locT29 *testing.T) {
	locKit2 := kitDir(locT29)
	locS9 := loadStream(locT29, locKit2)
	locRef3 := play(locT29, locKit2, locS9, originalTweaks(), nil)
	locRef2 := play(locT29, locKit2, locS9, originalTweaks(), nil)
	if !locRef3.notPractice || locRef3.squadTicks > 0 {
		locT29.Fatalf("free-for-all not detected (notPractice %v, squad ticks %d)", locRef3.notPractice, locRef3.squadTicks)
	}
	locNoise, _ := diffTicks(locRef3, locRef2)
	locCur4 := play(locT29, locKit2, locS9, tw, nil)
	locN9, locFirst := diffTicks(locRef3, locCur4)
	locT29.Logf("%d obs, worst step %v; original vs original: %d ticks differ; original vs tweaks: %d differ (first at t=%.1f)",
		len(locS9.obs), locCur4.worst, locNoise, locN9, locFirst)
	if locNoise == 0 && locN9 > 0 {
		locT29.Fatalf("the tweaks change free-for-all play: %d ticks differ, first at t=%.1f", locN9, locFirst)
	}
	if locN9 > 2*locNoise+3 {
		locT29.Fatalf("the tweaks change free-for-all play beyond run-to-run noise: %d vs %d", locN9, locNoise)
	}
}

// TestSquadStream: the same match turned into a practice-like one (nobody else dies, so the squad parameters
// take over after 20 s; our HP held at 60 % and enemy HP scaled down so that the lose rule and the finishing
// code run). No panics or bad actions, identical play before the squad switch.
func TestSquadStream(locT18 *testing.T) {
	locKit3 := kitDir(locT18)
	locS13 := loadStream(locT18, locKit3)
	locEdit2 := func(locO21 *Obs) {
		locO21.Alive = 16
		locO21.Self.HP = math.Max(locO21.Self.HP, 0.6*locO21.Self.MaxHP)
		for locI21 := range locO21.Mechs {
			if locO21.Mechs[locI21].Kind == "mech" {
				locO21.Mechs[locI21].HP *= 0.3
			}
		}
	}
	locRef := play(locT18, locKit3, locS13, originalTweaks(), locEdit2)
	locCur3 := play(locT18, locKit3, locS13, tw, locEdit2)
	if locRef.notPractice || locRef.squadTicks == 0 {
		locT18.Fatalf("squad mode not reached (notPractice %v, squad ticks %d)", locRef.notPractice, locRef.squadTicks)
	}
	locN10, locFirst3 := diffTicks(locRef, locCur3)
	locT18.Logf("squad ticks %d/%d, worst step %v; %d ticks differ from 70c8b23 (first at t=%.1f)", locCur3.squadTicks, len(locS13.obs), locCur3.worst, locN10, locFirst3)
	locT18.Logf("70c8b23 modes: %s", modeCount(locRef))
	locT18.Logf("tweaks  modes: %s", modeCount(locCur3))
	if tw.hopT == 0 && locN10 > 0 && locFirst3 < math.Min(tw.squadT, 20) {
		locT18.Fatalf("play differs before the squad switch (t=%.1f)", locFirst3)
	}
}
