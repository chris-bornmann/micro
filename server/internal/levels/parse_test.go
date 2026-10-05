package levels

import (
	"strings"
	"testing"

	"micro/server/internal/mapdata"
)

// sample: a lit hall (room 0) with the way in; a corridor to a dark
// pillared hall (room 1) through a hidden door; and a door in the wall
// the dark hall shares with a closet (room 2), holding a potion and the
// way down.
const sample = `
#######
#<...g#   #######
#.....+,,,S::#::####
#######   #:::::+.!#
          #::#::#.>#
          ##########
`

func parse(t *testing.T, src string) *Layout {
	t.Helper()
	l, err := Parse(strings.TrimPrefix(src, "\n"), map[rune]string{'g': "goblin", '!': "healing_potion"})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestAHandDrawnLevelParsesIntoRoomsCorridorsAndDoors(t *testing.T) {
	l := parse(t, sample)
	m := l.Map
	if len(m.Rooms) != 3 {
		t.Fatalf("rooms %+v", m.Rooms)
	}
	hall, pillars, closet := m.Rooms[0], m.Rooms[1], m.Rooms[2]
	if hall.X != 1 || hall.Y != 1 || hall.W != 5 || hall.H != 2 || hall.Dark || hall.Walled {
		t.Errorf("hall %+v", hall)
	}
	if !pillars.Dark || !pillars.Walled || !m.IsWall(13, 2) || !m.IsWall(13, 4) || m.IsWall(12, 3) {
		t.Errorf("pillared hall %+v, walls %v", pillars, m.Walls)
	}
	if closet.Dark || closet.Walled || closet.W != 2 {
		t.Errorf("closet %+v", closet)
	}
	if len(m.Corridors) != 2 {
		t.Fatalf("corridors %+v", m.Corridors)
	}
	var long, shared mapdata.Corridor
	for _, c := range m.Corridors {
		if len(c.Path) > 1 {
			long = c
		} else {
			shared = c
		}
	}
	if long.RoomA != 0 || long.RoomB != 1 || len(long.Path) != 5 || !long.DoorB.Hidden || long.DoorA.Hidden {
		t.Errorf("corridor %+v", long)
	}
	if shared.DoorA != shared.DoorB || shared.DoorA.X != 16 || shared.RoomA == shared.RoomB {
		t.Errorf("shared door %+v", shared)
	}
	want := map[string]mapdata.Point{"stairs_up": {X: 1, Y: 1}, "goblin": {X: 5, Y: 1}, "healing_potion": {X: 18, Y: 3}, "stairs_down": {X: 18, Y: 4}}
	if len(l.Placed) != len(want) {
		t.Fatalf("placed %+v", l.Placed)
	}
	for _, p := range l.Placed {
		if w := want[p.ID]; w.X != p.X || w.Y != p.Y {
			t.Errorf("%s at (%d,%d), want %v", p.ID, p.X, p.Y, w)
		}
	}
}

// TestYouCanWalkTheWholeDrawing: from the way in, through the hidden door
// and the shared one, to the way down.
func TestYouCanWalkTheWholeDrawing(t *testing.T) {
	m := parse(t, sample).Map
	if _, n, ok := m.Route(1, 1, 18, 4, nil); !ok || n < 15 {
		t.Errorf("no way through: %d steps, ok %v", n, ok)
	}
	if m.CanMove(5, 1, 6, 1) {
		t.Error("walked through the hall's wall")
	}
}

func TestDrawingMistakes(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"unknown":         {"#####\n#<.x#\n#####", `line 2, column 4: 'x' is not in the legend`},
		"no way in":       {"####\n#..#\n####", "no way in"},
		"open edge":       {"#####\n#<..\n#####", "line 2, column 5: a room's edge must be wall or door"},
		"branch":          {"#####   #####\n#<..+,,,+...#\n#####,  #####\n     ,\n   ####\n   #..+", "may not branch"},
		"dead end":        {"#####\n#<..+,,,\n#####", "must run from one door to another"},
		"door to nowhere": {"#####\n#<..+\n#####", "a door leads nowhere"},
	} {
		_, err := Parse(tc.src, nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want an error mentioning %q, got %v", name, tc.want, err)
		}
	}
}
