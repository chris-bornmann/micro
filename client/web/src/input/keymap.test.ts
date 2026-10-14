import { describe, expect, it } from "vitest";
import { interpret } from "./keymap";

const key = (k: string, mods: Partial<{ ctrlKey: boolean; altKey: boolean; metaKey: boolean }> = {}) => ({
  key: k,
  ctrlKey: false,
  altKey: false,
  metaKey: false,
  ...mods,
});

describe("playing", () => {
  it.each([
    ["ArrowUp", "Up"],
    ["ArrowDown", "Down"],
    ["ArrowLeft", "Left"],
    ["ArrowRight", "Right"],
    [".", "."],
    [",", ","],
    ["<", "<"],
    [">", ">"],
    ["a", "A"],
    ["E", "E"],
    ["d", "D"],
    ["t", "T"],
    ["x", "X"],
  ])("%s sends %s", (k, sent) => {
    expect(interpret(key(k), "play")).toEqual({ type: "send", key: sent });
  });

  it.each([
    ["i", "inventory"],
    ["L", "look"],
    ["s", "status"],
    ["?", "help"],
  ])("%s opens the %s view", (k, name) => {
    expect(interpret(key(k), "play")).toEqual({ type: "view", name });
  });

  it("leaves unbound keys and modifiers to the browser", () => {
    expect(interpret(key("q"), "play")).toBeNull();
    expect(interpret(key("Shift"), "play")).toBeNull();
    expect(interpret(key("ArrowUp", { ctrlKey: true }), "play")).toBeNull();
    expect(interpret(key("r", { metaKey: true }), "play")).toBeNull();
  });
});

describe("in a prompt", () => {
  it("sends a letter or a direction", () => {
    expect(interpret(key("b"), "prompt")).toEqual({ type: "send", key: "B" });
    expect(interpret(key("ArrowLeft"), "prompt")).toEqual({ type: "send", key: "Left" });
  });

  it("sends every letter, commands and views included, for the server to judge", () => {
    for (const k of ["k", "z", "Z", "i", "l", "s", "d"]) {
      expect(interpret(key(k), "prompt")).toEqual({ type: "send", key: k.toUpperCase() });
    }
  });

  it("cancels on anything else", () => {
    expect(interpret(key("Escape"), "prompt")).toEqual({ type: "send", key: "Escape" });
    expect(interpret(key("5"), "prompt")).toEqual({ type: "send", key: "Escape" });
  });
});

describe("answering a character prompt", () => {
  it("sends the character exactly as typed", () => {
    for (const k of ["T", "t", "g", "#", "?"]) {
      expect(interpret(key(k), "answer")).toEqual({ type: "send", key: k });
    }
  });

  it("cancels on a named key", () => {
    expect(interpret(key("Escape"), "answer")).toEqual({ type: "send", key: "Escape" });
    expect(interpret(key("ArrowLeft"), "answer")).toEqual({ type: "send", key: "Escape" });
  });
});

describe("elsewhere", () => {
  it("any key closes a view", () => {
    expect(interpret(key("x"), "view")).toEqual({ type: "close" });
  });

  it("after the game is over, only N does anything", () => {
    expect(interpret(key("n"), "over")).toEqual({ type: "newGame" });
    expect(interpret(key("ArrowUp"), "over")).toBeNull();
  });
});
