import { useEffect, useRef } from "preact/hooks";
import type { Store } from "../state/game";

/**
 * The pop-up for prompts and views, on the native <dialog>, which handles
 * focus and modality. Keys are not handled here: the app's one keyboard
 * listener interprets them (see input/keymap.ts), so a dialog behaves the
 * same however it was opened.
 */
export function Dialog({ store }: { store: Store }) {
  const ref = useRef<HTMLDialogElement>(null);
  const d = store.dialog.value;

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (d && !el.open) el.showModal();
    if (!d && el.open) el.close();
  }, [d]);

  const title = d?.kind === "prompt" ? d.prompt.title : d?.view.title;
  const lines = d?.kind === "prompt" ? d.prompt.lines : (d?.view.lines ?? []);
  const hints = {
    choose: "Press a letter, or any other key to cancel.",
    direction: "Press an arrow key, or any other key to cancel.",
    character: "Type a character, or press Escape to cancel.",
  };
  const hint = d?.kind === "prompt" ? hints[d.prompt.kind] : "Press any key to close.";

  return (
    <dialog
      ref={ref}
      class="popup"
      aria-labelledby="popup-title"
      // Escape would close a <dialog> on its own. The app's keyboard
      // listener already handles it — cancelling a prompt on the server too —
      // so the dialog must not also close itself.
      onCancel={(e) => e.preventDefault()}
    >
      <h2 id="popup-title">{title}</h2>
      <pre>{lines.join("\n")}</pre>
      <p class="hint">{hint}</p>
    </dialog>
  );
}
