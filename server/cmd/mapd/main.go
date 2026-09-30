// Command mapd serves the game over HTTP.
//
// Every dungeon-generation setting is a flag here, not a request field: the
// shape of the dungeon is the server's policy, and a client that could choose
// it could choose an easy one. The two cheat switches are flags for the same
// reason, and a stronger one — they exist to reveal what the game is
// otherwise hiding, so they must not be reachable from the network at all.
//
// A client may supply a seed when starting a game, and only so that a bug can
// be reproduced. Omitting it uses -seed, and -seed 0 means "pick at random".
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"micro/server/internal/game"
	"micro/server/internal/httpapi"
	"micro/server/internal/mapgen"
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
	flag.Int64Var(&cfg.Seed, "seed", cfg.Seed, "default RNG seed for new games (0 = random per game)")

	addr := flag.String("addr", ":8080", "address to listen on")
	revealHidden := flag.Bool("reveal-hidden", false, "cheat: draw hidden doors distinctly instead of disguising them as walls")
	showFullMap := flag.Bool("show-full-map", false, "cheat: ignore fog of war and send the entire generated level")
	flag.Parse()

	logger := log.New(os.Stderr, "mapd: ", log.LstdFlags)

	// newGame closes over the server's own settings, so the only thing a
	// request can influence is the seed.
	newGame := func(seed *int64) (*game.Game, error) {
		c := cfg
		if seed != nil {
			c.Seed = *seed
		}
		return game.NewGame(c, *revealHidden, *showFullMap)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.New(newGame, logger),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if *revealHidden || *showFullMap {
		logger.Printf("WARNING: cheats enabled (reveal-hidden=%v show-full-map=%v)", *revealHidden, *showFullMap)
	}
	// The launch configuration's compound start races the client against this
	// line, so say it in a form that is obvious in a log.
	logger.Printf("listening on %s", *addr)

	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "mapd:", err)
		os.Exit(1)
	}
}
