import type { Store } from "../state/game";

/** The last key sent, any error, and what to do when the game is over. */
export function StatusBar({ store }: { store: Store }) {
  const err = store.error.value;
  const over = store.frame.value?.game_over;
  return (
    <footer class="status">
      {err ? <span class="error">⚠ {err}</span> : <span>Last key: {store.lastKey.value || "(none)"}</span>}
      {over && <strong class="over">Game over — press N for a new game.</strong>}
      {store.busy.value && <span class="busy">…</span>}
    </footer>
  );
}
