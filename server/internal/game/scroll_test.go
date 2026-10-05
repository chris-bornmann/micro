package game

import (
	"testing"

	"micro/server/internal/rules"
)

func TestReadingTeleportMovesToAFreeTileConsumesTheScrollAndReveals(t *testing.T) {
	m := itemsTestMap()
	g := testMonster(t, "goblin", 8, 7, 5)
	p := testPlayer(t, 7, 7, 15)
	p.AddTrait(&rules.TraitInstance{Trait: "invisible", Turns: 100}) // keep the goblin out of it
	state := newTestGameState(t, m, p, []*rules.Entity{g}, []*rules.Entity{testItem(t, "teleport_scroll", 7, 7)})

	mustKey(t, state, ",")
	if msg := mustKey(t, state, "A"); msg != "You read the scroll of teleportation and are teleported away!" {
		t.Errorf("got %q", msg)
	}
	if (p.X == 7 && p.Y == 7) || (p.X == g.X && p.Y == g.Y) {
		t.Errorf("teleported onto an occupied tile (%d,%d)", p.X, p.Y)
	}
	if _, ok := m.RoomAt(p.X, p.Y); !ok {
		t.Errorf("teleported outside the room to (%d,%d)", p.X, p.Y)
	}
	if len(p.Inventory) != 0 {
		t.Error("expected the scroll used up")
	}
	if !state.Visited.Rooms[0] {
		t.Error("expected the Player's new surroundings revealed this same turn")
	}
}
