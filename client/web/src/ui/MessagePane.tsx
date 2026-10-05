import { useEffect, useRef } from "preact/hooks";
import type { Store } from "../state/game";

/** Every turn's messages, oldest first, scrolled to the newest. */
export function MessagePane({ store }: { store: Store }) {
  const end = useRef<HTMLLIElement>(null);
  const log = store.log.value;
  useEffect(() => end.current?.scrollIntoView({ block: "end" }), [log]);

  return (
    <section class="messages" aria-label="Messages">
      <ol aria-live="polite">
        {log.map((m) => (
          <li key={m.id}>
            <span class="turn">{m.turn}</span> {m.text}
          </li>
        ))}
        <li ref={end} class="end" aria-hidden="true" />
      </ol>
    </section>
  );
}
