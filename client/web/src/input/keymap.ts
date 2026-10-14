// What a keypress means, given what is on screen. Pure: it takes the parts
// of a KeyboardEvent it needs and returns an intent, so it is testable
// without a browser and the controller decides what to do with it.
//
// Keys are read from event.key — the character produced — not event.code,
// so `<` and `?` work on every keyboard layout.

import type { ViewName } from "../api/protocol";
import type { Mode } from "../state/game";

export type Intent =
  { type: "send"; key: string } | { type: "view"; name: ViewName } | { type: "close" } | { type: "newGame" };

export interface KeyLike {
  key: string;
  ctrlKey: boolean;
  altKey: boolean;
  metaKey: boolean;
}

const arrows: Record<string, string> = {
  ArrowUp: "Up",
  ArrowDown: "Down",
  ArrowLeft: "Left",
  ArrowRight: "Right",
};

const commands: Record<string, string> = {
  ".": ".",
  ",": ",",
  "<": "<",
  ">": ">",
  a: "A",
  e: "E",
  d: "D",
  t: "T",
  x: "X",
};

const views: Record<string, ViewName> = { i: "inventory", l: "look", s: "status", "?": "help" };

/** Keys that are only ever half of a combination. */
const modifiers = new Set(["Shift", "Control", "Alt", "Meta", "CapsLock", "AltGraph"]);

/**
 * interpret maps a key to an intent in mode, or null to let the browser have
 * it. A key held with Ctrl, Alt or Meta is always the browser's: those are
 * its shortcuts.
 */
export function interpret(e: KeyLike, mode: Mode): Intent | null {
  if (e.ctrlKey || e.altKey || e.metaKey || modifiers.has(e.key)) return null;
  const k = e.key.length === 1 ? e.key.toLowerCase() : e.key;

  switch (mode) {
    case "view":
      // Any key dismisses a view, as in the desktop client.
      return { type: "close" };

    case "prompt": {
      // A letter or a direction answers; anything else cancels, which the
      // server does when it gets a key it doesn't expect — including a
      // letter no item has.
      const dir = arrows[e.key];
      if (dir) return { type: "send", key: dir };
      if (/^[a-z]$/.test(k)) return { type: "send", key: k.toUpperCase() };
      return { type: "send", key: "Escape" };
    }

    case "answer":
      // Exactly the character typed, case and all: "t" and "T" differ.
      // Escape, or any other named key, cancels.
      if (e.key.length === 1) return { type: "send", key: e.key };
      return { type: "send", key: "Escape" };

    case "over":
      return k === "n" ? { type: "newGame" } : null;

    case "play": {
      const dir = arrows[e.key];
      if (dir) return { type: "send", key: dir };
      const cmd = commands[k];
      if (cmd) return { type: "send", key: cmd };
      const view = views[k];
      if (view) return { type: "view", name: view };
      // Unbound keys are not sent: on the server they would waste a turn.
      return null;
    }
  }
}
