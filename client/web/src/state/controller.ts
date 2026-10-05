// The only thing that changes state: it turns intents into API calls and
// API responses into store updates. It knows nothing about the DOM.

import { ApiError, type Api } from "../api/client";
import type { CommandResponse, Frame, ViewName } from "../api/protocol";
import type { Intent } from "../input/keymap";
import { LOG_LIMIT, type Store } from "./game";

/** Where the current game's ID is remembered across reloads. */
export interface IdStorage {
  get(): string | null;
  set(id: string): void;
  clear(): void;
}

export class Controller {
  private nextLogId = 1;

  constructor(
    private readonly store: Store,
    private readonly api: Api,
    private readonly ids: IdStorage,
  ) {}

  /** Picks up the remembered game if the server still has it — it keeps
   * games only in memory — and otherwise starts a new one. */
  async resume(seed?: number): Promise<void> {
    const id = this.ids.get();
    if (id) {
      try {
        const frame = await this.api.frame(id);
        this.store.gameId.value = id;
        this.show(frame);
        this.note(frame.turn, "Welcome back.");
        return;
      } catch (e) {
        if (!(e instanceof ApiError && e.status === 404)) return this.fail(e);
        this.ids.clear();
      }
    }
    await this.start(seed);
  }

  async start(seed?: number): Promise<void> {
    await this.guard(async () => {
      const res = await this.api.newGame(seed);
      this.ids.set(res.game_id);
      this.store.gameId.value = res.game_id;
      this.store.frame.value = null; // a new game's turn 0 is not stale
      this.store.dialog.value = null;
      this.show(res.frame);
      this.note(res.frame.turn, "A new game begins. Press ? for help.");
    });
  }

  /** Carries out what a keypress meant. */
  handle(intent: Intent): Promise<void> {
    switch (intent.type) {
      case "send":
        return this.send(intent.key);
      case "view":
        return this.openView(intent.name);
      case "close":
        this.store.dialog.value = null;
        return Promise.resolve();
      case "newGame":
        return this.start();
    }
  }

  private async send(key: string): Promise<void> {
    const id = this.store.gameId.value;
    if (!id) return;
    this.store.lastKey.value = key;
    await this.guard(async () => {
      const res = await this.api.command(id, key);
      this.apply(res);
    });
  }

  private async openView(name: ViewName): Promise<void> {
    const id = this.store.gameId.value;
    if (!id) return;
    await this.guard(async () => {
      this.store.dialog.value = { kind: "view", view: await this.api.view(id, name) };
    });
  }

  private apply(res: CommandResponse): void {
    if (res.prompt) {
      // Nothing moved: the frame on screen is still right.
      this.store.dialog.value = { kind: "prompt", prompt: res.prompt };
      return;
    }
    this.store.dialog.value = null;
    if (!this.show(res.frame)) return;
    for (const m of res.messages) this.note(res.frame.turn, m);
  }

  /** Shows f unless it is older than what is on screen — a response that
   * arrived out of order. Reports whether it was shown. */
  private show(f: Frame): boolean {
    const cur = this.store.frame.value;
    if (cur && f.turn < cur.turn) return false;
    this.store.frame.value = f;
    return true;
  }

  private note(turn: number, text: string): void {
    const log = [...this.store.log.value, { id: this.nextLogId++, turn, text }];
    this.store.log.value = log.length > LOG_LIMIT ? log.slice(log.length - LOG_LIMIT) : log;
  }

  /** Runs one request at a time: a key pressed while one is in flight is
   * dropped, so a held key can't flood the server. */
  private async guard(fn: () => Promise<void>): Promise<void> {
    if (this.store.busy.value) return;
    this.store.busy.value = true;
    try {
      await fn();
      this.store.error.value = null;
    } catch (e) {
      this.fail(e);
    } finally {
      this.store.busy.value = false;
    }
  }

  private fail(e: unknown): void {
    this.store.error.value = e instanceof Error ? e.message : String(e);
  }
}

/** sessionStorage-backed IdStorage: one game per browser tab. */
export function sessionIds(key = "micro.gameId"): IdStorage {
  return {
    get: () => sessionStorage.getItem(key),
    set: (id) => sessionStorage.setItem(key, id),
    clear: () => sessionStorage.removeItem(key),
  };
}
