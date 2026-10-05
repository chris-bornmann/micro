import type { Store } from "../state/game";

/** The status strip under the map, exactly as the server formatted it. */
export function Hud({ store }: { store: Store }) {
  const hud = store.frame.value?.hud ?? [];
  return (
    <ul class="hud" aria-label="Status">
      {hud.map((line) => (
        <li key={line}>{line}</li>
      ))}
    </ul>
  );
}
