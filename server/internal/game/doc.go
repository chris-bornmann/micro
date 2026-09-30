// Package game is the coordinator: it holds the central game state and drives
// every other service.
//
// It is the whole of the server's game logic, and its exported surface is
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
