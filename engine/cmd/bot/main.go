package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"steelwind/mech"
)

func main() {
	locLines := make(chan []byte, 256)
	go func() {
		locR30 := bufio.NewReaderSize(os.Stdin, 1<<20)
		for {
			locLine2, locErr10 := locR30.ReadBytes('\n')
			if len(locLine2) > 0 {
				locLines <- locLine2
			}
			if locErr10 != nil {
				close(locLines)
				return
			}
		}
	}()
	locOut3 := bufio.NewWriterSize(os.Stdout, 1<<16)
	locSend := func(locV9 any) {
		locB60, locErr11 := json.Marshal(locV9)
		if locErr11 != nil {
			return
		}
		locOut3.Write(locB60)
		locOut3.WriteByte('\n')
		locOut3.Flush()
	}
	var locBrain *mech.Brain
	var locProbe struct {
		Type string `json:"type"`
	}
	for {
		locFirst4, locOk17 := <-locLines
		if !locOk17 {
			return
		}
		locBatch := [][]byte{locFirst4}
	drain:
		for {
			select {
			case locL13, locOk18 := <-locLines:
				if !locOk18 {
					break drain
				}
				locBatch = append(locBatch, locL13)
			default:
				break drain
			}
		}
		var locLatest []byte
		for _, locRaw3 := range locBatch {
			locProbe.Type = ""
			if json.Unmarshal(locRaw3, &locProbe) != nil {
				continue
			}
			switch locProbe.Type {
			case "hello":
				var locH19 mech.Hello
				if locErr12 := json.Unmarshal(locRaw3, &locH19); locErr12 != nil {
					fmt.Fprintln(os.Stderr, "hello:", locErr12)
				}
				locDir2 := locH19.Map.Dir
				if locDir2 == "" {
					locDir2 = "/arena"
				}
				locW63, locErr8 := mech.LoadWorld(locDir2)
				if locErr8 != nil {
					fmt.Fprintln(os.Stderr, "map:", locErr8, "- playing on a flat map")
					locW63 = mech.FlatWorld()
				}
				locBrain = mech.NewBrain(&locH19, locW63)
				locSend(map[string]string{"type": "ready", "name": "steelwind"})
			case "obs":
				locLatest = locRaw3
			case "end":
				return
			}
		}
		if locLatest == nil {
			continue
		}
		var locO26 mech.Obs
		if locErr9 := json.Unmarshal(locLatest, &locO26); locErr9 != nil || locBrain == nil {
			var locT57 struct {
				Tick int `json:"tick"`
			}
			json.Unmarshal(locLatest, &locT57)
			locSend(mech.Action{Tick: locT57.Tick})
			continue
		}
		locSend(locBrain.Act(&locO26))
	}
}
