// Package game is the coordinator: it holds the central game state, turns
// keypresses into actions, and runs every turn through the rules engine
// (package rules), which decides what those actions actually do.
//
// Its exported surface is
// deliberately four methods wide (see Game). A client gets frames, turn
// messages, prompts and preformatted text; it never gets a GameState, a
// level, an item or a monster. Everything that decides what a player is
// allowed to know — fog of war, hidden doors, which monsters are perceptible
// — happens in here, before anything is serialized.
//
// Nothing in this package imports a UI toolkit, and nothing in it renders
// pixels. It produces character grids (see buildFrame and package grid);
// turning those into a window or a terminal is a client's business.
package game
