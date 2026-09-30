// Items: picking them up, using them, equipping them, dropping them,
// and the candidate lists each of those offers when more than one
// carried item would match.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"

	"map_api/armor"
	"map_api/item"
	"map_api/mapdata"
	"map_api/overlay"
	"map_api/player"
	"map_api/potion"
	"map_api/scroll"
	"map_api/torch"
	"map_api/weapon"
)

// relocateItem returns raw (an item's own JSON) with its generic X/Y
// fields overwritten to x,y — used when an item changes location without
// going through its owning package's Init (e.g. dropping something from
// Inventory back onto the floor at the Player's current tile).
func relocateItem(raw json.RawMessage, x, y int) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("parse item: %w", err)
	}
	xJSON, err := json.Marshal(x)
	if err != nil {
		return nil, fmt.Errorf("marshal x: %w", err)
	}
	yJSON, err := json.Marshal(y)
	if err != nil {
		return nil, fmt.Errorf("marshal y: %w", err)
	}
	fields["X"] = xJSON
	fields["Y"] = yJSON
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("marshal relocated item: %w", err)
	}
	return out, nil
}

// applyPickup runs the pickup pipeline (player.PickUp, then remove that
// specific item from FloorItems by ID — not by index, since with 2+
// items at a tile the chosen one may not be FloorItems[0]) against one
// specific item — shared by pickUp's 1-item auto path and
// resolvePendingSelection's picker-completion path. If the Player's
// Inventory is already full, the item is left on the floor and a
// friendly message is reported rather than an error.
func applyPickup(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	updatedSelf, err := player.PickUp(state.PlayerState, raw)
	if errors.Is(err, player.ErrInventoryFull) {
		return "Your inventory is full.", nil
	}
	if err != nil {
		return "", fmt.Errorf("player pick up: %w", err)
	}
	state.PlayerState = updatedSelf

	idx := -1
	for i, fi := range state.FloorItems {
		fit, err := itemInfo(fi)
		if err != nil {
			return "", err
		}
		if fit.ID == it.ID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return "", fmt.Errorf("hack: picked-up item %q no longer on the floor", it.ID)
	}
	state.FloorItems = append(state.FloorItems[:idx], state.FloorItems[idx+1:]...)
	return fmt.Sprintf("You pick up the %s.", it.Name), nil
}

// pickUp finds every pickable (non-Fixed) state.FloorItems entry at the
// Player's own tile. With exactly one, it's picked up immediately
// (matches the previous single-item behavior). With 2+, a picker opens
// (state.PendingSelection) instead of blindly grabbing the first one.
// Returns (message, done) with the same meaning as
// useItem/equipItem/dropItem's: done is false only when a picker was
// opened.
func pickUp(state *GameState, playerObj overlay.Object) (string, bool, error) {
	cands, err := pickupCandidatesAt(state, playerObj.X, playerObj.Y)
	if err != nil {
		return "", true, err
	}
	switch len(cands) {
	case 0:
		return "There is nothing here to pick up.", true, nil
	case 1:
		it, err := itemInfo(cands[0])
		if err != nil {
			return "", true, err
		}
		msg, err := applyPickup(state, it, cands[0])
		return msg, true, err
	default:
		state.PendingSelection = &pendingSelection{Action: pendingPickup, Candidates: wrapRaw(cands)}
		return "", false, nil
	}
}

// useItemEffect calls the Use function belonging to whichever category
// owns raw, identified generically via category — the seam a future item
// category (food, ...) plugs a new case into.
func useItemEffect(category item.Category, raw json.RawMessage) (json.RawMessage, error) {
	switch category {
	case item.CategoryPotion:
		out, err := potion.Use(raw)
		if err != nil {
			return nil, fmt.Errorf("potion use: %w", err)
		}
		var resp struct {
			Effect item.Effect `json:"effect"`
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, fmt.Errorf("parse potion use response: %w", err)
		}
		return json.Marshal(resp.Effect)
	case item.CategoryScroll:
		out, err := scroll.Use(raw)
		if err != nil {
			return nil, fmt.Errorf("scroll use: %w", err)
		}
		var resp struct {
			Effect item.Effect `json:"effect"`
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, fmt.Errorf("parse scroll use response: %w", err)
		}
		return json.Marshal(resp.Effect)
	default:
		return nil, fmt.Errorf("hack: no Use handler for item category %q", category)
	}
}

// resolveEffectTarget fills in game-state-dependent fields an item
// category's own Use couldn't have computed on its own — today, just a
// random unoccupied destination for item.EffectTeleport (scroll.Use only
// knows it's producing a teleport-kind effect, not where the map's open
// tiles currently are; only hack has that). A no-op for every other
// kind, so this is safe to call unconditionally right after
// useItemEffect.
func resolveEffectTarget(state *GameState, effectJSON json.RawMessage) (json.RawMessage, error) {
	var effect item.Effect
	if err := json.Unmarshal(effectJSON, &effect); err != nil {
		return nil, fmt.Errorf("parse effect: %w", err)
	}
	if effect.Kind != item.EffectTeleport {
		return effectJSON, nil
	}
	x, y, ok := randomUnoccupiedTile(state)
	if !ok {
		return nil, fmt.Errorf("hack: no unoccupied tile available to teleport to")
	}
	effect.X, effect.Y = x, y
	return json.Marshal(effect)
}

// randomUnoccupiedTile picks a uniformly random walkable tile on
// state.Map that isn't the Player's own current position or any live
// (non-Dead) monster's position — a dead monster doesn't block,
// matching the same exclusion objectsToRender/handleKey already apply
// elsewhere (see the movement fix in mapToRender); floor items never
// block anything in this game either, so they're not excluded here.
// Uses the global math/rand, matching action.RollDamage's convention
// for turn-time gameplay randomness — spawn-time positions are the only
// thing seeded from m.Seed. ok is false only in the degenerate case of
// no unoccupied tile existing at all.
func randomUnoccupiedTile(state *GameState) (x, y int, ok bool) {
	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return 0, 0, false
	}
	liveMonsters, err := liveMonsterObjects(state)
	if err != nil {
		return 0, 0, false
	}

	occupied := map[mapdata.Point]bool{{X: playerObj.X, Y: playerObj.Y}: true}
	for _, m := range liveMonsters {
		occupied[mapdata.Point{X: m.X, Y: m.Y}] = true
	}

	var candidates []mapdata.Point
	for _, p := range state.Map.WalkableTiles() {
		if !occupied[p] {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return 0, 0, false
	}
	chosen := candidates[rand.Intn(len(candidates))]
	return chosen.X, chosen.Y, true
}

// tileNearPlayer returns a random walkable tile in the same room as
// (playerX, playerY), excluding the Player's own tile — used to place
// the starting flask of oil somewhere immediately reachable rather than
// in a random room elsewhere on the map, the way its own package-level
// Init would otherwise place it. ok is false if the Player isn't in a
// room (shouldn't happen) or the room is 1x1.
func tileNearPlayer(m *mapdata.Map, playerX, playerY int) (x, y int, ok bool) {
	room, ok := m.RoomAt(playerX, playerY)
	if !ok {
		return 0, 0, false
	}
	var candidates []mapdata.Point
	for dx := 0; dx < room.W; dx++ {
		for dy := 0; dy < room.H; dy++ {
			cx, cy := room.X+dx, room.Y+dy
			if cx == playerX && cy == playerY {
				continue
			}
			candidates = append(candidates, mapdata.Point{X: cx, Y: cy})
		}
	}
	if len(candidates) == 0 {
		return 0, 0, false
	}
	chosen := candidates[rand.Intn(len(candidates))]
	return chosen.X, chosen.Y, true
}

// quaffCandidates returns every Inventory entry with item.UsageOneShot,
// in Inventory order — the set useItem chooses among, whether that's
// auto-picking a sole match or prompting a picker over 2+.
func quaffCandidates(state *GameState) ([]json.RawMessage, error) {
	inv, err := player.Inventory(state.PlayerState)
	if err != nil {
		return nil, err
	}
	var out []json.RawMessage
	for _, raw := range inv {
		it, err := itemInfo(raw)
		if err != nil {
			return nil, err
		}
		if it.Usage == item.UsageOneShot {
			out = append(out, raw)
		}
	}
	return out, nil
}

// equippableCategories are the item.Category values the "E" command
// offers — extend this map (and applyEquip's dispatch below) when a new
// equip-slot category (ring, shield, helm, ...) gets its own package;
// that's the only place a future category needs to be added.
var equippableCategories = map[item.Category]bool{
	item.CategoryWeapon: true,
	item.CategoryArmor:  true,
}

// equipCandidates is quaffCandidates' counterpart for equipping: every
// Inventory entry whose Category is in equippableCategories, in Inventory
// order.
func equipCandidates(state *GameState) ([]json.RawMessage, error) {
	inv, err := player.Inventory(state.PlayerState)
	if err != nil {
		return nil, err
	}
	var out []json.RawMessage
	for _, raw := range inv {
		it, err := itemInfo(raw)
		if err != nil {
			return nil, err
		}
		if equippableCategories[it.Category] {
			out = append(out, raw)
		}
	}
	return out, nil
}

// applyQuaff runs the use-and-consume pipeline (useItemEffect +
// resolveEffectTarget + player.ApplyEffect + player.TakeItem) against one
// specific item — shared by useItem's 0/1-candidate auto path and
// resolvePendingSelection's picker-completion path.
func applyQuaff(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	effectJSON, err := useItemEffect(it.Category, raw)
	if err != nil {
		return "", err
	}
	effectJSON, err = resolveEffectTarget(state, effectJSON)
	if err != nil {
		return "", err
	}

	applyJSON, err := player.ApplyEffect(state.PlayerState, effectJSON)
	if err != nil {
		return "", fmt.Errorf("player apply effect: %w", err)
	}
	var aresp effectEnvelope
	if err := json.Unmarshal(applyJSON, &aresp); err != nil {
		return "", fmt.Errorf("parse apply-effect response: %w", err)
	}
	state.PlayerState = aresp.Self

	updatedSelf, err := player.TakeItem(state.PlayerState, it.ID)
	if err != nil {
		return "", fmt.Errorf("player take item: %w", err)
	}
	state.PlayerState = updatedSelf

	return effectMessage(it.Name, aresp.Outcome), nil
}

// applyWield equips one specific item as the Player's wielded weapon —
// called by applyEquip's weapon branch, shared by equipItem's
// 0/1-candidate auto path and resolvePendingSelection's picker-completion
// path.
func applyWield(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	stats, err := weapon.Stats(raw)
	if err != nil {
		return "", fmt.Errorf("weapon stats: %w", err)
	}
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return "", fmt.Errorf("marshal weapon stats: %w", err)
	}
	updatedSelf, err := player.Wield(state.PlayerState, it.ID, statsJSON)
	if err != nil {
		return "", fmt.Errorf("player wield: %w", err)
	}
	state.PlayerState = updatedSelf
	return fmt.Sprintf("You wield the %s.", it.Name), nil
}

// applyWear equips one specific item into its own item.ArmorStats.Slot —
// called by applyEquip's armor branch, shared by equipItem's
// 0/1-candidate auto path and resolvePendingSelection's picker-completion
// path.
func applyWear(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	stats, err := armor.Stats(raw)
	if err != nil {
		return "", fmt.Errorf("armor stats: %w", err)
	}
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return "", fmt.Errorf("marshal armor stats: %w", err)
	}
	updatedSelf, err := player.Wear(state.PlayerState, it.ID, statsJSON)
	if err != nil {
		return "", fmt.Errorf("player wear: %w", err)
	}
	state.PlayerState = updatedSelf
	return fmt.Sprintf("You put on the %s.", it.Name), nil
}

// applyEquip equips one specific item, dispatching to the equip logic for
// its own Category — shared by equipItem's 0/1-candidate auto path and
// resolvePendingSelection's picker-completion path. Mirrors
// useItemEffect's category dispatch.
func applyEquip(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	switch it.Category {
	case item.CategoryWeapon:
		return applyWield(state, it, raw)
	case item.CategoryArmor:
		return applyWear(state, it, raw)
	default:
		return "", fmt.Errorf("hack: %s is not equippable", it.Name)
	}
}

// applyLight lights one specific torch, moving it into the Player's
// LitTorch slot — shared by useItem's 0/1-candidate auto path (via
// applyUse) and resolvePendingSelection's picker-completion path.
// Refusing a second torch while one is already lit (ErrTorchAlreadyLit)
// is reported as a friendly message rather than an error, the same way
// applyPickup handles player.ErrInventoryFull.
func applyLight(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	lifespan, err := torch.Lifespan(raw)
	if err != nil {
		return "", fmt.Errorf("torch lifespan: %w", err)
	}
	updatedSelf, err := player.LightTorch(state.PlayerState, it.ID, lifespan)
	if errors.Is(err, player.ErrTorchAlreadyLit) {
		return "Your torch is already lit.", nil
	}
	if err != nil {
		return "", fmt.Errorf("player light torch: %w", err)
	}
	state.PlayerState = updatedSelf
	return fmt.Sprintf("You light the %s. It should burn for %d turns.", it.Name, lifespan), nil
}

// applyUse routes a chosen use-picker candidate to the pipeline that
// actually knows how to activate its category — the seam a future
// one-shot-picker category plugs into, the same idea as useItemEffect's
// own category switch one level down. useItem and
// resolvePendingSelection's pendingQuaff case both call this rather than
// applyQuaff directly, since the 'A' picker now mixes potions and
// torches together.
func applyUse(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	switch it.Category {
	case item.CategoryTorch:
		return applyLight(state, it, raw)
	default:
		return applyQuaff(state, it, raw)
	}
}

// useItem uses the sole one-shot item in the Player's Inventory if
// exactly one is carried, opens a picker (setting
// state.PendingSelection) if 2+ are carried, or reports "You have
// nothing to use." if none are. Returns (message, done): done is false
// only when a picker was opened — no turn consumed yet;
// resolvePendingSelection finishes the job on the follow-up keypress.
func useItem(state *GameState) (string, bool, error) {
	cands, err := quaffCandidates(state)
	if err != nil {
		return "", true, err
	}
	switch len(cands) {
	case 0:
		return "You have nothing to use.", true, nil
	case 1:
		it, err := itemInfo(cands[0])
		if err != nil {
			return "", true, err
		}
		msg, err := applyUse(state, it, cands[0])
		return msg, true, err
	default:
		state.PendingSelection = &pendingSelection{Action: pendingQuaff, Candidates: wrapRaw(cands)}
		return "", false, nil
	}
}

// equipItem is useItem's counterpart for equipping: equips the sole
// equippable item (weapon or armor — see equippableCategories) if exactly
// one is carried, opens a picker over both categories together if 2+ are,
// or reports "You have nothing to equip." if none are. Returns (message,
// done) with the same meaning as useItem's.
func equipItem(state *GameState) (string, bool, error) {
	cands, err := equipCandidates(state)
	if err != nil {
		return "", true, err
	}
	switch len(cands) {
	case 0:
		return "You have nothing to equip.", true, nil
	case 1:
		it, err := itemInfo(cands[0])
		if err != nil {
			return "", true, err
		}
		msg, err := applyEquip(state, it, cands[0])
		return msg, true, err
	default:
		state.PendingSelection = &pendingSelection{Action: pendingEquip, Candidates: wrapRaw(cands)}
		return "", false, nil
	}
}

// applyDrop drops one specific carried item: player.Drop removes it from
// Wielded (auto-unwielding, reverting combat to bare hands), LitTorch, or
// Inventory. A dropped lit torch is never placed on the floor — it
// simply disappears — so that case returns early before the
// relocate+FloorItems step everything else goes through. Message varies
// depending on which of the three it was.
func applyDrop(state *GameState, it item.Item, raw json.RawMessage) (string, error) {
	dropJSON, err := player.Drop(state.PlayerState, it.ID)
	if err != nil {
		return "", fmt.Errorf("player drop: %w", err)
	}
	var dresp dropEnvelope
	if err := json.Unmarshal(dropJSON, &dresp); err != nil {
		return "", fmt.Errorf("parse drop response: %w", err)
	}
	state.PlayerState = dresp.Self

	if dresp.WasLitTorch {
		return fmt.Sprintf("You drop the %s. It goes out and falls apart.", it.Name), nil
	}

	playerObj, err := playerObject(state.PlayerState)
	if err != nil {
		return "", err
	}
	relocated, err := relocateItem(dresp.Item, playerObj.X, playerObj.Y)
	if err != nil {
		return "", err
	}
	state.FloorItems = append(state.FloorItems, relocated)

	if dresp.WasWielded {
		return fmt.Sprintf("You drop the %s. You are now fighting bare-handed.", it.Name), nil
	}
	if dresp.WasWorn {
		return fmt.Sprintf("You take off the %s and drop it.", it.Name), nil
	}
	return fmt.Sprintf("You drop the %s.", it.Name), nil
}

// dropItem always opens the picker when 1+ items are carried — unlike
// useItem/equipItem, there's no "auto-act on the sole candidate"
// shortcut, since drop is meant to show everything carried on every
// press, not just when there's an ambiguous choice. Returns "You have
// nothing to drop." (done=true, no turn) if nothing is carried at all.
func dropItem(state *GameState) (string, bool, error) {
	items, err := carriedItems(state)
	if err != nil {
		return "", true, err
	}
	if len(items) == 0 {
		return "You have nothing to drop.", true, nil
	}
	state.PendingSelection = &pendingSelection{Action: pendingDrop, Candidates: items}
	return "", false, nil
}
