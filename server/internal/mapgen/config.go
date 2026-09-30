package mapgen

import "fmt"

// Config controls level generation. Percent fields are 0-100.
type Config struct {
	Width, Height int

	NumRooms                 int
	MinRoomSize, MaxRoomSize int // interior width/height range, inclusive

	// DarkRoomPercent is the approximate percentage of rooms that are dark.
	DarkRoomPercent float64
	// HiddenDoorPercent is the approximate percentage of doors that are hidden.
	HiddenDoorPercent float64
	// ExtraCorridorPercent controls how many additional corridors are added
	// beyond the minimum spanning tree, as a percentage of NumRooms. Higher
	// values produce more loops/alternate paths between rooms.
	ExtraCorridorPercent float64

	// Seed seeds the RNG. If 0, a random seed is chosen and recorded on the
	// returned Map so the layout can be reproduced later.
	Seed int64
}

// DefaultConfig returns reasonable defaults, useful as a starting point for
// callers who only want to override a few fields.
func DefaultConfig() Config {
	return Config{
		Width:                80,
		Height:               40,
		NumRooms:             10,
		MinRoomSize:          4,
		MaxRoomSize:          10,
		DarkRoomPercent:      30,
		HiddenDoorPercent:    15,
		ExtraCorridorPercent: 20,
		Seed:                 0,
	}
}

func (c Config) validate() error {
	if c.Width < 20 || c.Height < 20 {
		return fmt.Errorf("width/height must be at least 20 (got %dx%d)", c.Width, c.Height)
	}
	if c.NumRooms < 1 {
		return fmt.Errorf("numRooms must be at least 1 (got %d)", c.NumRooms)
	}
	if c.MinRoomSize < 3 {
		return fmt.Errorf("minRoomSize must be at least 3 (got %d)", c.MinRoomSize)
	}
	if c.MaxRoomSize < c.MinRoomSize {
		return fmt.Errorf("maxRoomSize (%d) must be >= minRoomSize (%d)", c.MaxRoomSize, c.MinRoomSize)
	}
	if c.DarkRoomPercent < 0 || c.DarkRoomPercent > 100 {
		return fmt.Errorf("darkRoomPercent must be between 0 and 100 (got %v)", c.DarkRoomPercent)
	}
	if c.HiddenDoorPercent < 0 || c.HiddenDoorPercent > 100 {
		return fmt.Errorf("hiddenDoorPercent must be between 0 and 100 (got %v)", c.HiddenDoorPercent)
	}
	if c.ExtraCorridorPercent < 0 {
		return fmt.Errorf("extraCorridorPercent must be >= 0 (got %v)", c.ExtraCorridorPercent)
	}
	return nil
}
