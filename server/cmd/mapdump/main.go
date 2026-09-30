// Command mapdump generates a level with mapgen and writes it as JSON, so
// generation output can be inspected or diffed directly without starting a
// game.
//
// It deliberately cannot render. Drawing a map is the client's job, and the
// only map a client is ever shown is the fog-of-war-filtered one the server
// sends it — so a second, unfiltered rendering path here would be a cheat
// tool, and one that could drift from the real one. To look at a level
// visually, play it; to look at an unexplored level visually, run the server
// with -show-full-map and connect the terminal client.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"micromap/server/internal/mapgen"
)

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
	outFile := flag.String("out", "", "write to this file instead of stdout")
	flag.Parse()

	m, err := mapgen.Generate(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mapdump:", err)
		os.Exit(1)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mapdump:", err)
		os.Exit(1)
	}

	if *outFile == "" {
		fmt.Println(string(data))
		return
	}
	if err := os.WriteFile(*outFile, data, 0644); err != nil {
		fmt.Fprintln(os.Stderr, "mapdump:", err)
		os.Exit(1)
	}
}
