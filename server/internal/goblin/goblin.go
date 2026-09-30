// Package goblin will eventually be its own standalone Goblin microservice.
// It follows the same shape as player: a private type embedding
// overlay.Object, plus Init and Action functions that are JSON-in/JSON-out.
// Unlike player, the Goblin isn't keyboard-driven — Action computes its
// move autonomously from the current world state, and it owns its own
// memory (whether it has ever detected the Player) rather than relying on
// the coordinator to track that for it: the coordinator only ever feeds
// this package information and stores/forwards whatever it returns — it
// never calls back into hack or into any other entity.
package goblin

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"

	"micro/server/internal/action"
	"micro/server/internal/attributes"
	"micro/server/internal/fire"
	"micro/server/internal/item"
	"micro/server/internal/mapdata"
	"micro/server/internal/overlay"
	"micro/server/internal/slide"
	spawnmodel "micro/server/internal/spawn"
	"micro/server/internal/status"
	"micro/server/internal/world"
)

var nextID int64
var nextCorpseID int64

// NativeLevel is the dungeon level a goblin is tuned for — both the
// Level a spawned goblin reports (see Object.Level below) and the
// center of SpawnCount's own weight curve. A goblin is common and weak,
// so it's native to the shallowest possible level.
const NativeLevel = 1

// Spawn-count tuning (see SpawnCount) — a goblin is common: baseDensity
// is the expected count on its own native level, and sigmaUp controls
// how gently that count tapers off on deeper (easier-for-a-goblin)
// levels. sigmaDown/maxAbove are unused in practice — NativeLevel is
// already the shallowest level, so a goblin can never be "too strong
// for the floor" — but spawn.MonsterWeight still takes them.
const (
	spawnBaseDensity = 8.0
	spawnSigmaUp     = 6.0
	spawnSigmaDown   = 2.0
	spawnMaxAbove    = 4.0
)

// SpawnCount decides how many goblins spawn on dungeonLevel — see
// spawn.GenericSpawnCount. alreadySpawned is accepted only to match
// monsterService's shared SpawnCount signature; a goblin is common, not
// single-shot, so it's ignored.
func SpawnCount(dungeonLevel int, alreadySpawned bool, rng *rand.Rand) int {
	return spawnmodel.GenericSpawnCount(NativeLevel, dungeonLevel, spawnBaseDensity, spawnSigmaUp, spawnSigmaDown, spawnMaxAbove, rng)
}

// monster is this package's private representation of a goblin. It embeds
// overlay.Object so the generic fields (ID, X, Y, Glyph, Color, Layer)
// stay field-for-field identical to the overlay contract render consumes —
// anything that only needs to draw the goblin can decode this JSON as a
// plain overlay.Object and silently ignore Aware. Aware is this package's
// own private memory: whether it has ever detected the Player.
type monster struct {
	overlay.Object
	Aware bool
	HP    int
	// MaxHP is the goblin's hit-point ceiling, set once at Init and never
	// changed afterward — same role as player.person.MaxHP. Nothing
	// currently heals a goblin, so nothing clamps against it yet, but
	// it's tracked from the start for the same forward-compatibility
	// reason.
	MaxHP int
	// Wielded is the Goblin's pre-equipped weapon's combat-stats
	// snapshot, if InitArmed spawned it with one — set once at spawn,
	// never changed afterward (the Goblin has no PickUp/Wield of its
	// own this pass). nil for a goblin spawned via plain Init, which
	// fights bare-handed.
	Wielded *item.WeaponStats `json:"wielded,omitempty"`
	// Fleeing reports whether the goblin is currently running from the
	// Player after a hit dropped it to/below fleeThreshold(MaxHP). Set
	// once, in Defend, at the moment that threshold is crossed (not
	// re-triggered by subsequent hits while already fleeing, so a
	// goblin already underway doesn't flip-flop destinations); cleared
	// in Action once it either recovers (unreachable today — nothing
	// heals a goblin yet, kept for forward-compatibility) or reaches
	// FleeRoomID.
	Fleeing bool
	// FleeRoomID is the room ID a fleeing goblin is heading for, and
	// FleeX/FleeY is the specific cell within it — a random interior
	// tile, not the room's center, so the goblin actually commits to
	// somewhere inside the room rather than stopping the moment it
	// crosses the threshold (which without a real interior target tends
	// to land it just barely inside the doorway). Defend can't choose
	// any of this (no map access), so it just starts the episode with
	// FleeRoomID at -1 ("fleeing, no destination chosen yet"); Action
	// picks a room and cell lazily the next time it runs and commits to
	// them until the flee ends.
	FleeRoomID   int
	FleeX, FleeY int
	// Attributes are the goblin's core stats, rolled once at spawn from
	// this package's own range (2-6) and never changed afterward.
	// Corpse preserves this field verbatim along with everything else
	// non-inventory, so a future reincarnation feature restores the same
	// values rather than re-rolling them. Nothing consumes them yet —
	// see the attributes package doc.
	Attributes attributes.Attributes `json:"attributes"`
	// BurningTurnsRemaining counts down once per Tick while the goblin
	// is on fire (see overlay.Object.Burning, Defend's Ignites
	// handling) — mirrors player.person's own field of the same name.
	BurningTurnsRemaining int `json:"burning_turns_remaining,omitempty"`
	// BurningStartsNextTurn mirrors player.person's own field of the
	// same name: true for exactly one Tick after a fresh ignition, so
	// the goblin doesn't take damage the same turn it catches fire.
	BurningStartsNextTurn bool `json:"burning_starts_next_turn,omitempty"`
}

// response is what Action returns: the goblin's own resulting state
// (exactly what Init/Action have always returned, unchanged shape) plus a
// report of what actually happened this turn.
type response struct {
	Self   monster       `json:"self"`
	Result action.Result `json:"result"`
}

// defendResponse is what Defend returns: the goblin's own resulting state
// plus a report of the incoming attack's outcome.
type defendResponse struct {
	Self    monster              `json:"self"`
	Outcome action.DefendOutcome `json:"outcome"`
}

// moveResult marshals self alongside a "move" result at self's current
// position — used whenever nothing happened this turn (not yet aware, no
// legal route) as well as after an actual successful move.
func moveResult(self monster) ([]byte, error) {
	return json.Marshal(response{Self: self, Result: action.Result{Action: "move", X: self.X, Y: self.Y}})
}

// fleeThreshold is the HP at or below which a goblin starts fleeing —
// the ceiling of 10% of maxHP, minimum 1. A literal maxHP/10 (integer
// division) rounds to 0 across the goblin's entire HP range (5-10),
// which would make fleeing silently never trigger; this fixes that. For
// the current range this works out to 1 HP for every goblin.
func fleeThreshold(maxHP int) int {
	t := (maxHP + 9) / 10
	if t < 1 {
		t = 1
	}
	return t
}

// Init creates a new goblin on the given generated level (a JSON-encoded
// mapdata.Map) and returns it as JSON, fighting bare-handed. spawnIndex
// distinguishes this goblin from any others populateLevel spawns
// alongside it on the same level — see spawn's own doc comment for why
// that matters.
func Init(mapJSON []byte, spawnIndex int) ([]byte, error) {
	g, err := spawn(mapJSON, spawnIndex)
	if err != nil {
		return nil, err
	}
	return json.Marshal(g)
}

// InitArmed is Init's counterpart for spawning a goblin already wielding
// a weapon — spawn-time configuration, not new AI behavior (the Goblin
// still has no PickUp/Wield; see weapon.GoblinStarter). weaponStatsJSON
// is a JSON-encoded item.WeaponStats, exactly the shape weapon.Stats/
// weapon.GoblinStarter produce and player.Wield accepts.
func InitArmed(mapJSON, weaponStatsJSON []byte, spawnIndex int) ([]byte, error) {
	g, err := spawn(mapJSON, spawnIndex)
	if err != nil {
		return nil, err
	}
	var stats item.WeaponStats
	if err := json.Unmarshal(weaponStatsJSON, &stats); err != nil {
		return nil, fmt.Errorf("goblin: parse weapon stats: %w", err)
	}
	g.Wielded = &stats
	return json.Marshal(g)
}

// spawn is Init/InitArmed's shared body: parses the map, rolls a spawn
// position and starting HP from a seeded rng, and sets Object.Kind to
// "goblin" so any wielded weapon's VsKind matchups (see
// item.WeaponStats.Range) can reference this goblin generically.
//
// spawnIndex is this goblin's position (0, 1, 2, ...) among however many
// populateLevel spawns on the same level (see hack/levels.go), and folds
// into the rng seed alongside m.Seed. Without it, every goblin spawned
// for the same map would re-seed from the exact same value and roll the
// identical room/position/HP/attributes — landing every one of them on
// the same cell and, since Action then computes identical moves from
// identical state, keeping them in lockstep forever. The multiplier
// (1_000_000) keeps this package's per-instance fan-out from ever
// landing on one of the small m.Seed+N offsets other packages reserve
// for themselves (see e.g. potion.go/scroll.go's own offset comments) —
// spawnIndex 0 reproduces the original m.Seed+1 exactly, so a
// single-goblin level's spawn is unchanged.
func spawn(mapJSON []byte, spawnIndex int) (monster, error) {
	var m mapdata.Map
	if err := json.Unmarshal(mapJSON, &m); err != nil {
		return monster{}, fmt.Errorf("goblin: parse map: %w", err)
	}
	if len(m.Rooms) == 0 {
		return monster{}, fmt.Errorf("goblin: map has no rooms to spawn in")
	}

	// Seed from m.Seed+1, not m.Seed directly — player.Init seeds its own
	// RNG from m.Seed, so reusing that exact value here would draw the
	// identical room/offset every time and spawn on top of the Player.
	rng := rand.New(rand.NewSource(m.Seed + 1 + int64(spawnIndex)*1_000_000))
	r := m.Rooms[rng.Intn(len(m.Rooms))]
	x := r.X + rng.Intn(r.W)
	y := r.Y + rng.Intn(r.H)
	// Drawn immediately after spawn position, from the same seeded rng, so
	// starting HP is reproducible alongside spawn point for a given map
	// seed. Only this package knows the Goblin's HP range (5-10). The
	// goblin starts at full health, so MaxHP is set to the same rolled
	// value.
	hp := 5 + rng.Intn(6)
	// Drawn immediately after HP, from the same seeded rng, so attributes
	// are reproducible alongside spawn point and HP for a given map
	// seed. Only this package knows the Goblin's attribute range (2-6).
	attrs := attributes.Range{Min: 2, Max: 6}.Roll(rng)

	return monster{
		Object: overlay.Object{
			ID:    fmt.Sprintf("goblin:%d", atomic.AddInt64(&nextID, 1)),
			X:     x,
			Y:     y,
			Glyph: 'g',
			Color: overlay.Color{G: 200},
			Layer: 1,
			Kind:  "goblin",
			// Half the Player's baseline weight — a smaller humanoid;
			// see overlay.Object.Weight.
			Weight: 50,
			// A goblin's Level never changes, fixed at NativeLevel for
			// its whole lifetime — unlike the Player's; see
			// overlay.Object.Level.
			Level: NativeLevel,
		},
		Aware:      false,
		HP:         hp,
		MaxHP:      hp,
		Attributes: attrs,
	}, nil
}

// Action computes the goblin's move for this turn. selfJSON is exactly
// what this goblin last returned from Init or Action — its own private
// memory, including whether it has ever detected the Player — and
// augmentedMapJSON is the current map plus the Player's position (and any
// other objects), fed by the coordinator. The goblin never looks for
// itself in augmentedMapJSON's object list; selfJSON is the sole source of
// truth for its own position and state. Returns {"self": ..., "result":
// ...} as JSON: self is the goblin's own resulting state, result reports
// what happened.
//
// Behavior:
//   - Not yet aware of the Player: stays put (a no-op move result) unless
//     the Player is currently in the same room — or standing in that
//     room's doorway, which is an open threshold, not a barrier, so it
//     counts the same as being in the room (mapdata.Map.RoomsAt) — in
//     which case it becomes aware and immediately starts closing in this
//     same turn. A goblin doesn't chase you across the map, it waits
//     until you're actually visible to it.
//   - Once aware (this turn or a previous one): heads toward the Player
//     via the shortest legal route (mapdata.Map.NextStep, which respects
//     room/corridor door-crossing rules — so it follows through doors and
//     down corridors, not just within the room it first noticed you in),
//     one tile per turn.
//   - If that next tile is occupied by another object (i.e. the Player),
//     it doesn't move there — it attacks instead. This recurs every turn
//     the Player stays adjacent, and resumes as ordinary movement as soon
//     as that tile frees up.
//   - No legal route to the Player: a no-op move result.
//   - A Player who's currently Invisible (see overlay.Object.Invisible)
//     can't be perceived at all: an unaware goblin never becomes aware of
//     one, and an already-aware goblin immediately loses track (Aware
//     resets to false) and stays put rather than continuing to close in
//     on a position it can no longer actually perceive. Turning invisible
//     genuinely breaks off a chase, not just attacks.
//   - A Player who's currently Sneaking (see overlay.Object.Sneaking,
//     granted by worn equipment such as shoes of sneaking) is weaker
//     than Invisible and only matters while not yet Aware: standing
//     exactly on a doorway threshold no longer counts as being "in" the
//     room for the purposes of becoming aware (mapdata.Map.RoomAt,
//     strict containment, replaces RoomsAt's threshold-inclusive rule
//     for the Player's side of the check only) — but stepping fully
//     into the room is detected immediately as normal, and an
//     already-aware goblin keeps chasing a sneaking Player regardless of
//     where they stand. Sneaking prevents being spotted in the first
//     place; it isn't real-time camouflage.
//   - Fleeing (self.Fleeing, set by Defend when a hit drops HP to/below
//     fleeThreshold(MaxHP)) overrides everything above: checked first,
//     before Invisible/Aware are even consulted, so a fleeing goblin
//     keeps moving toward its chosen destination (FleeRoomID/FleeX/FleeY
//     — a specific random cell inside a room other than the Player's,
//     picked lazily here via pickFleeRoomAndCell the first time Action
//     runs after Defend starts the episode, since Defend has no map to
//     pick one itself) every turn regardless of whether it can currently
//     see the Player — it doesn't need to see you to run away. Fleeing
//     ends, and Aware resets to false (it has to be rediscovered before
//     the chase can resume), either once HP recovers above the
//     threshold (unreachable today — nothing heals a goblin yet — kept
//     for forward compatibility) or once it gets as close as it can to
//     (FleeX,FleeY) — normally exact arrival, since rooms have no
//     internal obstacles.
func Action(selfJSON, augmentedMapJSON []byte) ([]byte, error) {
	var self monster
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("goblin: parse self: %w", err)
	}

	var aug world.AugmentedMap
	if err := json.Unmarshal(augmentedMapJSON, &aug); err != nil {
		return nil, fmt.Errorf("goblin: parse augmented map: %w", err)
	}

	playerIdx := indexByPrefix(aug.Objects, "player:")
	if playerIdx == -1 {
		return nil, fmt.Errorf("goblin: no player object found in augmented map")
	}
	p := aug.Objects[playerIdx]

	if self.Fleeing && self.FleeRoomID == -1 {
		if room, cellX, cellY, ok := pickFleeRoomAndCell(aug.Map, p.X, p.Y); ok {
			self.FleeRoomID, self.FleeX, self.FleeY = room.ID, cellX, cellY
		} else {
			self.Fleeing = false // nowhere to flee to (e.g. a single-room level)
		}
	}
	if self.Fleeing && self.HP > fleeThreshold(self.MaxHP) {
		self.Fleeing, self.FleeRoomID, self.Aware = false, -1, false
	}
	if self.Fleeing {
		return fleeStep(self, aug)
	}

	if p.Invisible {
		self.Aware = false
		return moveResult(self)
	}

	if !self.Aware {
		playerRooms := aug.Map.RoomsAt(p.X, p.Y)
		if p.Sneaking {
			// While sneaking, merely standing on a doorway threshold
			// doesn't count as being "in" the room for detection
			// purposes — only genuinely standing inside one does.
			// Rendering/fog-of-war is unaffected: the Player can still
			// see into the room from the threshold exactly as before;
			// this only changes whether the Player can be SEEN from
			// there. Only gates this initial detection — an already-
			// Aware goblin's continued chase below is untouched, so
			// retreating to a doorway mid-chase doesn't break tracking.
			playerRooms = map[int]bool{}
			if r, ok := aug.Map.RoomAt(p.X, p.Y); ok {
				playerRooms[r.ID] = true
			}
		}
		if !sharesARoom(aug.Map.RoomsAt(self.X, self.Y), playerRooms) {
			return moveResult(self)
		}
		self.Aware = true
	}

	nx, ny, ok := aug.Map.NextStep(self.X, self.Y, p.X, p.Y)
	if !ok {
		return moveResult(self)
	}

	if target, occupied := occupantAt(aug.Objects, nx, ny); occupied {
		return json.Marshal(response{Self: self, Result: attackResult(self.Wielded, target, self.Attributes)})
	}

	fx, fy := nx, ny
	if isOily(aug.OilyTiles, nx, ny) {
		fx, fy = slide.Resolve(aug.Map, aug.Objects, nx, ny, nx-self.X, ny-self.Y)
	}
	self.X, self.Y = fx, fy
	return moveResult(self)
}

// fleeStep moves self one step toward its chosen flee cell (FleeX,
// FleeY), ignoring the Player's visibility/awareness entirely — a
// fleeing goblin keeps moving even when it can't currently see the
// Player. Mirrors the existing chase step (NextStep + occupant-check-
// and-attack) exactly, just aimed at the flee cell instead of the
// Player: if the path happens to be blocked by an occupant, it attacks
// rather than getting stuck — a cornered goblin fights back rather than
// freezing.
//
// NextStep returning !ok covers two cases at once, and both end the
// flee episode the same way: the goblin has already arrived exactly at
// (FleeX,FleeY) (NextStep of a point against itself is defined to
// report no next step — see TestNextStepSamePointIsNotOK), or no route
// there exists at all (only possible on a disconnected map; every real
// generated level is fully connected). Either way there's nothing more
// to do — "as close as possible" has already been reached.
func fleeStep(self monster, aug world.AugmentedMap) ([]byte, error) {
	nx, ny, ok := aug.Map.NextStep(self.X, self.Y, self.FleeX, self.FleeY)
	if !ok {
		self.Fleeing, self.FleeRoomID, self.Aware = false, -1, false
		return moveResult(self)
	}

	if target, occupied := occupantAt(aug.Objects, nx, ny); occupied {
		return json.Marshal(response{Self: self, Result: attackResult(self.Wielded, target, self.Attributes)})
	}

	fx, fy := nx, ny
	if isOily(aug.OilyTiles, nx, ny) {
		fx, fy = slide.Resolve(aug.Map, aug.Objects, nx, ny, nx-self.X, ny-self.Y)
	}
	self.X, self.Y = fx, fy
	return moveResult(self)
}

// pickFleeRoomAndCell picks a uniformly random room other than the one
// containing (playerX,playerY), then a uniformly random cell within
// that room's interior — the specific destination a newly-fleeing
// goblin commits to, rather than just the room's center, so it actually
// commits to somewhere inside the room instead of stopping the instant
// it crosses the threshold (which tends to land it just inside the
// doorway). Uses the global math/rand, matching the established
// convention for turn-time (not spawn-time) randomness (see
// action.RollDamage) rather than a seeded RNG, since this is a live
// gameplay decision, not something that needs to be reproducible from
// the map seed. ok is false only if no other room exists (e.g. a
// single-room level).
func pickFleeRoomAndCell(m mapdata.Map, playerX, playerY int) (room mapdata.Room, cellX, cellY int, ok bool) {
	playerRoom, _ := m.RoomAt(playerX, playerY)
	var candidates []mapdata.Room
	for _, r := range m.Rooms {
		if r.ID != playerRoom.ID {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return mapdata.Room{}, 0, 0, false
	}
	room = candidates[rand.Intn(len(candidates))]
	cellX = room.X + rand.Intn(room.W)
	cellY = room.Y + rand.Intn(room.H)
	return room, cellX, cellY, true
}

// attackResult mirrors player.attackResult: the wielded weapon's stats
// (adjusted for target.Kind) if one is wielded, or bare hands (1-2
// damage) otherwise. attackerAttributes is carried along on the Result
// unchanged (see action.Result.Attributes) so Defend can apply a
// Strength-based bonus to either branch alike.
func attackResult(wielded *item.WeaponStats, target overlay.Object, attackerAttributes attributes.Attributes) action.Result {
	if wielded != nil {
		min, max := wielded.Range(target.Kind)
		return action.Result{Action: "attack", X: target.X, Y: target.Y, Weapon: wielded.Name, DamageMin: min, DamageMax: max, Attributes: attackerAttributes}
	}
	return action.Result{Action: "attack", X: target.X, Y: target.Y, Weapon: "hands", DamageMin: 1, DamageMax: 2, Attributes: attackerAttributes}
}

// Defend applies an incoming attack (a JSON-encoded action.Result, as
// produced by whichever entity attacked) to the goblin found in selfJSON:
// rolls damage in the attack's [DamageMin,DamageMax] range, subtracts it
// from HP, and marks the goblin Dead if HP drops to 0 or below. If this
// hit drops (not already-below) HP to/below fleeThreshold(MaxHP), it
// also starts a flee episode (Fleeing=true, FleeRoomID=-1) — this is the
// one moment Defend can tell "just crossed the threshold" apart from
// "already fleeing," which is why fleeing starts here rather than being
// polled in Action (see Action's own doc comment). Returns {"self": ...,
// "outcome": ...} as JSON.
func Defend(selfJSON, attackJSON []byte) ([]byte, error) {
	var self monster
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("goblin: parse self: %w", err)
	}

	var attack action.Result
	if err := json.Unmarshal(attackJSON, &attack); err != nil {
		return nil, fmt.Errorf("goblin: parse attack: %w", err)
	}

	damage := action.RollDamage(attack.DamageMin, attack.DamageMax)
	// A flat bonus from the attacker's own Strength — floor(Strength/10),
	// so 0-9 contributes nothing, 10-19 contributes +1, 20-29 +2, and so
	// on. Go's integer division already truncates toward zero, which for
	// a non-negative Strength is exactly floor. Applies to weapon and
	// bare-handed attacks alike, since attackResult sets Attributes on
	// both branches.
	damage += attack.Attributes.Strength / 10
	self.HP -= damage
	if self.HP <= 0 {
		self.Dead = true
	}
	// Independent of damage — a hit can both hurt and add oiliness (e.g.
	// a thrown flask of oil), or neither, or either alone.
	if attack.AddsOiliness {
		self.Oiliness++
	}
	// Igniting requires already being oily — a lit torch thrown at a dry
	// target does nothing extra. A second ignition while already
	// burning extends the fire rather than resetting it, and doesn't
	// re-delay damage (only a fresh ignition starts next turn).
	if attack.Ignites && self.Oiliness > 0 {
		if self.BurningTurnsRemaining == 0 {
			self.BurningStartsNextTurn = true
		}
		self.BurningTurnsRemaining += fire.Duration(self.Oiliness)
		self.Burning = true
	}
	if !self.Dead && !self.Fleeing && self.HP <= fleeThreshold(self.MaxHP) {
		self.Fleeing = true
		self.FleeRoomID = -1
	}

	outcome := action.DefendOutcome{Damage: damage, RemainingHP: self.HP, Dead: self.Dead}
	return json.Marshal(defendResponse{Self: self, Outcome: outcome})
}

// tickResponse is what Tick returns: the goblin's own resulting state
// plus a report of what the tick actually did — mirrors player's own
// tickResponse shape.
type tickResponse struct {
	Self    monster          `json:"self"`
	Outcome item.TickOutcome `json:"outcome"`
}

// Tick applies one turn of any active time-based state to the goblin
// found in selfJSON — today, just burning (see Defend's Ignites
// handling) — mirroring player.Tick's shape for hack's own symmetric
// per-turn processing. Rolls fresh burn damage via fire.TickDamage(),
// subtracts it from HP, decrements BurningTurnsRemaining, and marks
// Dead if HP drops to 0 or below, exactly like Defend already does for
// combat damage; also re-runs the same fleeThreshold check Defend uses,
// so burn damage can trigger fleeing exactly like combat damage
// already does. A no-op (self unchanged, outcome zero-valued) if the
// goblin isn't currently burning. Returns {"self": ..., "outcome": ...}
// as JSON.
func Tick(selfJSON []byte) ([]byte, error) {
	var self monster
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("goblin: parse self: %w", err)
	}

	var burnDamage int
	var burnedOut bool
	if self.BurningStartsNextTurn {
		// The grace turn: caught fire this same turn, so no damage yet
		// — the fire actually starts burning next Tick.
		self.BurningStartsNextTurn = false
	} else if self.BurningTurnsRemaining > 0 {
		burnDamage = fire.TickDamage()
		self.HP -= burnDamage
		self.BurningTurnsRemaining--
		if self.HP <= 0 {
			self.Dead = true
		}
		if self.BurningTurnsRemaining <= 0 {
			self.Burning = false
			burnedOut = true
		}
		if !self.Dead && !self.Fleeing && self.HP <= fleeThreshold(self.MaxHP) {
			self.Fleeing = true
			self.FleeRoomID = -1
		}
	}

	outcome := item.TickOutcome{BurnDamage: burnDamage, BurnedOut: burnedOut}
	return json.Marshal(tickResponse{Self: self, Outcome: outcome})
}

// corpseItem is this package's private floor-item representation of a
// dead goblin's remains — embeds item.Item exactly as weaponItem/
// potionItem do. Source records the originating goblin's ID so a future
// reanimation feature can find the matching (still-inert) monster state;
// today there's only ever one goblin, so this is purely forward-looking,
// not load-bearing yet.
type corpseItem struct {
	item.Item
	Source string
}

// CorpseResult is Corpse's JSON response.
type CorpseResult struct {
	Self    json.RawMessage   `json:"self"`
	Item    json.RawMessage   `json:"item"`
	Wielded *item.WeaponStats `json:"wielded,omitempty"`
}

// Corpse builds a floor-item representation of self (expected to already
// have Dead=true, i.e. called right after a Defend that reported death)
// for the coordinator to add to its floor items. It also strips self's
// inventory (currently just Wielded) in the returned Self, since it's
// being dropped separately rather than kept "on" the goblin — mirrors
// player.Drop's "return the mutated Self alongside what was removed"
// shape. Every non-inventory attribute (HP, MaxHP, Aware, Kind, ID,
// position) is left exactly as it was, since that's what a future
// reanimation feature would need to restore a goblin's identity — this
// package never deletes or resets that data itself.
func Corpse(selfJSON []byte) ([]byte, error) {
	var self monster
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("goblin: parse self: %w", err)
	}

	wielded := self.Wielded
	self.Wielded = nil

	c := corpseItem{
		Item: item.Item{
			Object: overlay.Object{
				ID:    fmt.Sprintf("corpse:%d", atomic.AddInt64(&nextCorpseID, 1)),
				X:     self.X,
				Y:     self.Y,
				Glyph: '%',
				Color: overlay.Color{R: 140, G: 40, B: 40},
				Layer: 0, // floor items draw below goblin (1) and player (2)
				Kind:  self.Kind,
				// A corpse weighs what the goblin weighed, stays as oily
				// as the goblin was, and still shows if it died mid-fire.
				Weight:   self.Weight,
				Oiliness: self.Oiliness,
				Burning:  self.Burning,
			},
			Category: item.CategoryCorpse,
			Name:     self.Kind + " corpse",
			Usage:    item.UsagePersistent,
		},
		Source: self.ID,
	}
	itemRaw, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("goblin: marshal corpse item: %w", err)
	}
	selfRaw, err := json.Marshal(self)
	if err != nil {
		return nil, fmt.Errorf("goblin: marshal self: %w", err)
	}

	return json.Marshal(CorpseResult{Self: selfRaw, Item: itemRaw, Wielded: wielded})
}

// Status describes this goblin's own current condition — HP, Attributes,
// and any active modifiers — as an ordered list of label/value pairs,
// following player.Status' exact shape. Not called by hack yet (no
// monster-inspection UI exists), but registered in hack's
// monsterServices so a future one can call it generically by Kind.
func Status(selfJSON []byte) ([]byte, error) {
	var self monster
	if err := json.Unmarshal(selfJSON, &self); err != nil {
		return nil, fmt.Errorf("goblin: parse self: %w", err)
	}
	fields := []status.Field{
		{Label: "Level", Value: fmt.Sprintf("%d", self.Level)},
		{Label: "HP", Value: fmt.Sprintf("%d/%d", self.HP, self.MaxHP)},
		{Label: "Strength", Value: fmt.Sprintf("%d", self.Attributes.Strength)},
		{Label: "Intelligence", Value: fmt.Sprintf("%d", self.Attributes.Intelligence)},
		{Label: "Dexterity", Value: fmt.Sprintf("%d", self.Attributes.Dexterity)},
		{Label: "Stamina", Value: fmt.Sprintf("%d", self.Attributes.Stamina)},
	}
	if self.Wielded != nil {
		fields = append(fields, status.Field{Label: "Wielded", Value: self.Wielded.Name})
	}
	if self.Oiliness > 0 {
		fields = append(fields, status.Field{Label: "Oily", Value: fmt.Sprintf("x%d", self.Oiliness)})
	}
	if self.Burning {
		fields = append(fields, status.Field{Label: "Burning", Value: "yes"})
	}
	return json.Marshal(fields)
}

// isOily reports whether (x,y) is one of tiles.
func isOily(tiles []mapdata.Point, x, y int) bool {
	for _, p := range tiles {
		if p.X == x && p.Y == y {
			return true
		}
	}
	return false
}

// occupantAt returns whatever object occupies (x,y), if any. The goblin
// itself is never in objects (it comes from selfJSON, not the augmented
// map), so there's no self-exclusion needed here, unlike player's
// equivalent helper.
func occupantAt(objects []overlay.Object, x, y int) (overlay.Object, bool) {
	for _, o := range objects {
		if o.X == x && o.Y == y {
			return o, true
		}
	}
	return overlay.Object{}, false
}

func indexByPrefix(objects []overlay.Object, prefix string) int {
	for i, o := range objects {
		if strings.HasPrefix(o.ID, prefix) {
			return i
		}
	}
	return -1
}

// sharesARoom reports whether a and b (each a set of room IDs from
// mapdata.Map.RoomsAt) have any room in common.
func sharesARoom(a, b map[int]bool) bool {
	for id := range a {
		if b[id] {
			return true
		}
	}
	return false
}
