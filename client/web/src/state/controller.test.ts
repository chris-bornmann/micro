import { describe, expect, it, vi } from "vitest";
import { ApiError, type Api } from "../api/client";
import type { CommandResponse, Frame } from "../api/protocol";
import { Controller, type IdStorage } from "./controller";
import { createStore } from "./game";

const frameAt = (turn: number, extra: Partial<Frame> = {}): Frame => ({
  turn,
  title: "t",
  level: 1,
  width: 1,
  height: 1,
  rows: ["@"],
  colors: [[0]],
  palette: ["#ffffff"],
  hud: [],
  game_over: false,
  ...extra,
});

function memoryIds(initial: string | null = null): IdStorage {
  let id = initial;
  return { get: () => id, set: (v) => (id = v), clear: () => (id = null) };
}

function fakeApi(over: Partial<Api> = {}): Api {
  return {
    newGame: vi.fn(async () => ({ game_id: "new", frame: frameAt(0) })),
    frame: vi.fn(async () => frameAt(5)),
    command: vi.fn(async (): Promise<CommandResponse> => ({ messages: [], frame: frameAt(1) })),
    view: vi.fn(async () => ({ name: "help", title: "Help", lines: ["?"] })),
    ...over,
  };
}

async function started(api: Api) {
  const store = createStore();
  const c = new Controller(store, api, memoryIds());
  await c.start();
  return { store, c };
}

describe("Controller", () => {
  it("resumes a remembered game the server still has", async () => {
    const store = createStore();
    const api = fakeApi();
    await new Controller(store, api, memoryIds("old")).resume();
    expect(api.frame).toHaveBeenCalledWith("old");
    expect(store.gameId.value).toBe("old");
    expect(store.frame.value?.turn).toBe(5);
  });

  it("starts a new game when the server has forgotten the old one", async () => {
    const store = createStore();
    const ids = memoryIds("gone");
    const api = fakeApi({
      frame: vi.fn(async () => {
        throw new ApiError(404, "no such game");
      }),
    });
    await new Controller(store, api, ids).resume(42);
    expect(api.newGame).toHaveBeenCalledWith(42);
    expect(store.gameId.value).toBe("new");
    expect(ids.get()).toBe("new");
  });

  it("opens a prompt without touching the frame", async () => {
    const api = fakeApi({
      command: vi.fn(async (): Promise<CommandResponse> => ({
        messages: [],
        prompt: { kind: "choose", title: "Choose one", lines: ["A) torch"] },
      })),
    });
    const { store, c } = await started(api);
    await c.handle({ type: "send", key: "A" });
    expect(store.mode.value).toBe("prompt");
  });

  it("a character prompt puts keys in answer mode", async () => {
    const api = fakeApi({
      command: vi.fn(async (): Promise<CommandResponse> => ({
        messages: [],
        prompt: { kind: "character", title: "Scroll", lines: ["Summon which monster?"] },
      })),
    });
    const { store, c } = await started(api);
    await c.handle({ type: "send", key: "A" });
    expect(store.mode.value).toBe("answer");
    expect(store.frame.value?.turn).toBe(0);
  });

  it("a frame closes the prompt and logs the turn's messages", async () => {
    const api = fakeApi({
      command: vi.fn(async (): Promise<CommandResponse> => ({
        messages: ["One.", "Two."],
        frame: frameAt(3),
      })),
    });
    const { store, c } = await started(api);
    store.dialog.value = { kind: "prompt", prompt: { kind: "choose", title: "", lines: [] } };
    await c.handle({ type: "send", key: "B" });
    expect(store.dialog.value).toBeNull();
    expect(store.log.value.slice(-2).map((m) => [m.turn, m.text])).toEqual([
      [3, "One."],
      [3, "Two."],
    ]);
  });

  it("ignores a frame older than the one on screen", async () => {
    const api = fakeApi({
      command: vi.fn(async (): Promise<CommandResponse> => ({ messages: ["Old."], frame: frameAt(2) })),
    });
    const { store, c } = await started(api);
    store.frame.value = frameAt(7);
    await c.handle({ type: "send", key: "." });
    expect(store.frame.value.turn).toBe(7);
    expect(store.log.value.some((m) => m.text === "Old.")).toBe(false);
  });

  it("drops a key pressed while a request is in flight", async () => {
    let release!: () => void;
    const api = fakeApi({
      command: vi.fn(
        () => new Promise<CommandResponse>((r) => (release = () => r({ messages: [], frame: frameAt(1) }))),
      ),
    });
    const { store, c } = await started(api);
    const first = c.handle({ type: "send", key: "Up" });
    await c.handle({ type: "send", key: "Up" }); // dropped
    expect(api.command).toHaveBeenCalledTimes(1);
    release();
    await first;
    expect(store.busy.value).toBe(false);
  });

  it("shows a view, and closes it locally", async () => {
    const { store, c } = await started(fakeApi());
    await c.handle({ type: "view", name: "help" });
    expect(store.mode.value).toBe("view");
    await c.handle({ type: "close" });
    expect(store.mode.value).toBe("play");
  });

  it("reports an error and recovers", async () => {
    const api = fakeApi({
      command: vi.fn(async () => {
        throw new ApiError(500, "boom");
      }),
    });
    const { store, c } = await started(api);
    await c.handle({ type: "send", key: "Up" });
    expect(store.error.value).toBe("boom");
    expect(store.busy.value).toBe(false);
  });

  it("knows when the game is over", async () => {
    const api = fakeApi({
      command: vi.fn(async (): Promise<CommandResponse> => ({
        messages: [],
        frame: frameAt(9, { game_over: true }),
      })),
    });
    const { store, c } = await started(api);
    await c.handle({ type: "send", key: "." });
    expect(store.mode.value).toBe("over");
  });
});
