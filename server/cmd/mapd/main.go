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
//
// By default the server is anonymous, for play on one's own machine. With
// -auth cognito, every request must carry an access token from the
// Cognito user pool -cognito-pool, and each player has their own games.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"micro/server/internal/auth"
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
	revealHidden := flag.Bool("reveal-hidden", false, "cheat: draw hidden doors and invisible monsters, in magenta")
	showFullMap := flag.Bool("show-full-map", false, "cheat: ignore fog of war and send the entire generated level (invisible monsters stay hidden)")
	cheatMode := flag.Bool("cheat-mode", false, "cheat: start the player with 1000 hit points")
	playerFile := flag.String("player", "", "testing: load the player from this YAML file instead of the built-in one (see internal/content/data/README.md)")
	corsOrigins := flag.String("cors-origin", "", "comma-separated origins whose web pages may call this server, e.g. http://localhost:5173 (default: none)")
	webDir := flag.String("web-dir", "", "serve the built web client from this directory at / (default: none)")
	authMode := flag.String("auth", "none", "who may play: none (anyone, anonymously) or cognito (signed-in users, each with their own games)")
	cognitoPool := flag.String("cognito-pool", "", "with -auth cognito: the user pool ID, e.g. us-east-1_AbCdEf123")
	cognitoClient := flag.String("cognito-client", "", "with -auth cognito: the user pool's app client ID")
	cognitoDomain := flag.String("cognito-domain", "", "with -auth cognito: the hosted sign-in page's domain, e.g. micro-hack.auth.us-east-1.amazoncognito.com")
	flag.Parse()

	logger := log.New(os.Stderr, "mapd: ", log.LstdFlags)

	// Check the game's content before serving anything: a mistake in a
	// YAML file should stop the server here, with the file and line, not
	// fail the first new game.
	if err := game.LoadContent(*playerFile); err != nil {
		fmt.Fprintln(os.Stderr, "mapd: content:", err)
		os.Exit(1)
	}

	// newGame closes over the server's own settings, so the only thing a
	// request can influence is the seed.
	newGame := func(seed *int64) (*game.Game, error) {
		c := cfg
		if seed != nil {
			c.Seed = *seed
		}
		return game.NewGame(c, *revealHidden, *showFullMap, *cheatMode)
	}

	opts := httpapi.Options{Logger: logger}
	switch *authMode {
	case "none":
	case "cognito":
		if *cognitoDomain == "" {
			fmt.Fprintln(os.Stderr, "mapd: -auth cognito needs -cognito-domain")
			os.Exit(2)
		}
		c, err := auth.NewCognito(*cognitoPool, *cognitoClient)
		if err != nil {
			fmt.Fprintln(os.Stderr, "mapd: -auth cognito:", err)
			os.Exit(2)
		}
		opts.Auth = c
		opts.SignIn = &httpapi.SignIn{Domain: *cognitoDomain, ClientID: *cognitoClient}
	default:
		fmt.Fprintf(os.Stderr, "mapd: -auth %q: want none or cognito\n", *authMode)
		os.Exit(2)
	}
	if *webDir != "" {
		opts.Web = os.DirFS(*webDir)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.WithCORS(httpapi.New(newGame, opts), splitOrigins(*corsOrigins)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if *revealHidden || *showFullMap || *cheatMode {
		logger.Printf("WARNING: cheats enabled (reveal-hidden=%v show-full-map=%v cheat-mode=%v)", *revealHidden, *showFullMap, *cheatMode)
	}
	// The launch configuration's compound start races the client against this
	// line, so say it in a form that is obvious in a log.
	logger.Printf("listening on %s (auth: %s)", *addr, *authMode)

	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "mapd:", err)
		os.Exit(1)
	}
}

// splitOrigins parses -cors-origin's comma-separated list, ignoring blanks.
func splitOrigins(list string) []string {
	var out []string
	for _, o := range strings.Split(list, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	return out
}
