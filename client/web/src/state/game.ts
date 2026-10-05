// The app's state, as signals. Nothing here knows about the DOM or how the
// game is drawn: components read these, and only the controller writes them.

import { computed, signal } from "@preact/signals";
import type { Frame, Prompt, View } from "../api/protocol";

/** What a modal dialog is showing, if anything. */
export type Dialog = { kind: "prompt"; prompt: Prompt } | { kind: "view"; view: View };

/** One line in the message pane. */
export interface LogEntry {
  /** Monotonic, for list keys. */
  id: number;
  /** The turn the message belongs to. */
  turn: number;
  text: string;
}

/** How long the message pane's history grows before old lines drop off. */
export const LOG_LIMIT = 500;

export function createStore() {
  const frame = signal<Frame | null>(null);
  const dialog = signal<Dialog | null>(null);
  const log = signal<LogEntry[]>([]);
  const lastKey = signal("");
  const error = signal<string | null>(null);
  const busy = signal(false);
  const gameId = signal<string | null>(null);

  /** What keys mean right now. */
  const mode = computed<Mode>(() => {
    const d = dialog.value;
    if (d?.kind === "prompt") return d.prompt.kind === "character" ? "answer" : "prompt";
    if (d?.kind === "view") return "view";
    if (frame.value?.game_over) return "over";
    return "play";
  });

  return { frame, dialog, log, lastKey, error, busy, gameId, mode };
}

/** "answer" is a prompt that wants a typed character rather than a letter. */
export type Mode = "play" | "prompt" | "answer" | "view" | "over";

export type Store = ReturnType<typeof createStore>;
