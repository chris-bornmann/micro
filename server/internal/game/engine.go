// The rules engine every game runs on: the built-in behaviors and the
// game's content, loaded once.
package game

import (
	"sync"

	"micro/server/internal/content"
	"micro/server/internal/rules"
	"micro/server/internal/rules/builtin"
)

// defaultEngine loads the content the first time it is needed. Content is
// compiled in and never changes while the server runs, so every game
// shares one engine.
var defaultEngine = sync.OnceValues(func() (*rules.Engine, error) {
	reg := builtin.Registry()
	cat, err := content.Load(reg)
	if err != nil {
		return nil, err
	}
	return &rules.Engine{Reg: reg, Cat: cat}, nil
})

// playerEngine is the engine for a hand-crafted player, once LoadContent
// has been given one: nil means every game runs on defaultEngine.
var playerEngine *rules.Engine

// LoadContent loads and checks the game's content, so that a server can
// refuse to start on a mistake in it rather than fail its first request.
// playerFile, if not "", is a YAML file whose player replaces the
// built-in one in every game from now on — for testing with a
// hand-crafted player (see content.LoadWithPlayer). Call it before serving
// any game.
func LoadContent(playerFile string) error {
	if playerFile == "" {
		_, err := defaultEngine()
		return err
	}
	reg := builtin.Registry()
	cat, err := content.LoadWithPlayer(reg, playerFile)
	if err != nil {
		return err
	}
	playerEngine = &rules.Engine{Reg: reg, Cat: cat}
	return nil
}

// engine is the engine new games run on.
func engine() (*rules.Engine, error) {
	if playerEngine != nil {
		return playerEngine, nil
	}
	return defaultEngine()
}
