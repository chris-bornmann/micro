// Command demo generates a level with mapgen and prints it with render, so
// generation output can be inspected directly in a terminal.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"map_api/mapdata"
	"map_api/mapgen"
	"map_api/overlay"
	"map_api/render"
)

// demoObjects builds a couple of hardcoded stub game objects to demonstrate
// the overlay contract (see the overlay package). Real Player/Goblin/etc.
// services would report their own objects instead of this.
func demoObjects(m *mapdata.Map) []overlay.Object {
	if len(m.Rooms) == 0 {
		return nil
	}
	p := m.Rooms[0]
	objs := []overlay.Object{
		{ID: "player:1", X: p.CenterX(), Y: p.CenterY(), Glyph: '@', Color: overlay.Color{R: 255, G: 255, B: 255}, Layer: 2},
	}
	if len(m.Rooms) > 1 {
		g := m.Rooms[1]
		objs = append(objs, overlay.Object{ID: "goblin:1", X: g.CenterX(), Y: g.CenterY(), Glyph: 'g', Color: overlay.Color{G: 200}, Layer: 1})
	}
	return objs
}

func main() {
	cfg := mapgen.DefaultConfig()

	flag.IntVar(&cfg.Width, "width", cfg.Width, "map width in tiles")
	flag.IntVar(&cfg.Height, "height", cfg.Height, "map height in tiles")
	flag.IntVar(&cfg.NumRooms, "rooms", cfg.NumRooms, "number of rooms to place")
	flag.IntVar(&cfg.MinRoomSize, "min-room", cfg.MinRoomSize, "minimum room interior size")
	flag.IntVar(&cfg.MaxRoomSize, "max-room", cfg.MaxRoomSize, "maximum room interior size")
	flag.Float64Var(&cfg.DarkRoomPercent, "dark-percent", cfg.DarkRoomPercent, "percent of rooms that are dark")
	flag.Float64Var(&cfg.HiddenDoorPercent, "hidden-door-percent", cfg.HiddenDoorPercent, "percent of doors that are hidden")
	flag.Float64Var(&cfg.ExtraCorridorPercent, "extra-corridor-percent", cfg.ExtraCorridorPercent, "extra loop corridors, as a percent of room count")
	flag.Int64Var(&cfg.Seed, "seed", cfg.Seed, "RNG seed (0 = random)")
	revealHidden := flag.Bool("reveal-hidden", true, "show hidden doors distinctly instead of disguising them as walls")
	asJSON := flag.Bool("json", false, "print the generated map as JSON instead of ASCII (e.g. to pipe into the view command: --out file.json)")
	outFile := flag.String("out", "", "with --json, write to this file instead of stdout")
	demoObjectsFlag := flag.Bool("demo-objects", false, "overlay a couple of stub objects (player, goblin) to demonstrate the overlay contract")
	flag.Parse()

	m, err := mapgen.Generate(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}

	if *asJSON {
		data, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "marshal:", err)
			os.Exit(1)
		}
		if *outFile == "" {
			fmt.Println(string(data))
			return
		}
		if err := os.WriteFile(*outFile, data, 0644); err != nil {
			fmt.Fprintln(os.Stderr, "write:", err)
			os.Exit(1)
		}
		return
	}

	opts := render.Options{RevealHidden: *revealHidden, Legend: true}
	if *demoObjectsFlag {
		opts.Objects = demoObjects(m)
	}
	out := render.ANSI(m, opts)
	fmt.Print(out)
}
