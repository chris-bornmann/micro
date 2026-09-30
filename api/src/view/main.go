// Command view opens a window and draws a generated level as a colored grid.
// It exits as soon as that window is closed.
//
// By default it generates a fresh map using the same parameters as the
// generate command (src/main.go). Pass -file to instead load a map that was
// saved earlier with `generate -json -out level.json`, which is how a
// separate map-generator service's output would be inspected.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"

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

	inFile := flag.String("file", "", "load a map previously saved as JSON instead of generating a new one")
	cellSize := flag.Int("cell-size", 10, "pixel size of each tile")
	revealHidden := flag.Bool("reveal-hidden", false, "show hidden doors distinctly instead of disguising them as walls")
	demoObjectsFlag := flag.Bool("demo-objects", false, "overlay a couple of stub objects (player, goblin) to demonstrate the overlay contract")

	flag.IntVar(&cfg.Width, "width", cfg.Width, "map width in tiles (ignored with -file)")
	flag.IntVar(&cfg.Height, "height", cfg.Height, "map height in tiles (ignored with -file)")
	flag.IntVar(&cfg.NumRooms, "rooms", cfg.NumRooms, "number of rooms to place (ignored with -file)")
	flag.IntVar(&cfg.MinRoomSize, "min-room", cfg.MinRoomSize, "minimum room interior size (ignored with -file)")
	flag.IntVar(&cfg.MaxRoomSize, "max-room", cfg.MaxRoomSize, "maximum room interior size (ignored with -file)")
	flag.Float64Var(&cfg.DarkRoomPercent, "dark-percent", cfg.DarkRoomPercent, "percent of rooms that are dark (ignored with -file)")
	flag.Float64Var(&cfg.HiddenDoorPercent, "hidden-door-percent", cfg.HiddenDoorPercent, "percent of doors that are hidden (ignored with -file)")
	flag.Float64Var(&cfg.ExtraCorridorPercent, "extra-corridor-percent", cfg.ExtraCorridorPercent, "extra loop corridors, as a percent of room count (ignored with -file)")
	flag.Int64Var(&cfg.Seed, "seed", cfg.Seed, "RNG seed, 0 = random (ignored with -file)")
	flag.Parse()

	m, err := loadMap(*inFile, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "view:", err)
		os.Exit(1)
	}

	opts := render.Options{RevealHidden: *revealHidden}
	if *demoObjectsFlag {
		opts.Objects = demoObjects(m)
	}
	img := render.Image(m, opts, *cellSize)

	a := app.NewWithID("com.map_api.view")
	w := a.NewWindow(fmt.Sprintf("Level Map — seed %d, %d rooms", m.Seed, len(m.Rooms)))

	canvasImg := canvas.NewImageFromImage(img)
	canvasImg.ScaleMode = canvas.ImageScalePixels
	canvasImg.FillMode = canvas.ImageFillOriginal

	w.SetContent(canvasImg)
	w.Resize(fyne.NewSize(float32(img.Bounds().Dx()), float32(img.Bounds().Dy())))
	w.SetFixedSize(true)

	// ShowAndRun blocks until the window is closed, then returns, so main
	// exits right after — no explicit close handler needed.
	w.ShowAndRun()
}

func loadMap(path string, cfg mapgen.Config) (*mapdata.Map, error) {
	if path == "" {
		return mapgen.Generate(cfg)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m mapdata.Map
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &m, nil
}
