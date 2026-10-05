import { describe, expect, it } from "vitest";
import { parseCommandResponse, parseFrame, ProtocolError } from "./protocol";

// The example frame from PROTOCOL.md, made consistent (its rows there are
// illustrative and shorter than its width).
export const exampleFrame = {
  turn: 12,
  title: "Hack — level 1, seed 1234",
  level: 1,
  width: 6,
  height: 2,
  rows: ["   ###", "   #.@"],
  colors: [
    [0, 0, 0, 1, 1, 1],
    [0, 0, 0, 1, 2, 3],
  ],
  palette: ["#000000", "#616161", "#ada78c", "#ffffff"],
  hud: ["Level: 1", "HP: 18/18", "Strength: 16"],
  game_over: false,
};

describe("parseFrame", () => {
  it("accepts a frame that follows the protocol", () => {
    expect(parseFrame(exampleFrame)).toEqual(exampleFrame);
  });

  it("treats a missing hud as empty, as the protocol allows", () => {
    const noHud: Record<string, unknown> = { ...exampleFrame };
    delete noHud.hud;
    expect(parseFrame(noHud).hud).toEqual([]);
  });

  it("measures rows in code points, not UTF-16 units", () => {
    const f = { ...exampleFrame, rows: ["   ###", "   #.𐀀"] };
    expect(() => parseFrame(f)).not.toThrow();
  });

  it.each([
    ["a row of the wrong width", { rows: ["   ##", "   #.@"] }],
    ["too few color rows", { colors: [[0, 0, 0, 1, 1, 1]] }],
    [
      "a color outside the palette",
      {
        colors: [
          [0, 0, 0, 1, 1, 9],
          [0, 0, 0, 1, 2, 3],
        ],
      },
    ],
    ["a malformed palette entry", { palette: ["#000", "#616161", "#ada78c", "#ffffff"] }],
    ["game_over missing", { game_over: undefined }],
    ["a fractional turn", { turn: 1.5 }],
  ])("rejects %s", (_, change) => {
    expect(() => parseFrame({ ...exampleFrame, ...change })).toThrow(ProtocolError);
  });
});

describe("parseCommandResponse", () => {
  it("accepts a frame with messages", () => {
    const r = parseCommandResponse({ messages: ["You hit it."], frame: exampleFrame });
    expect(r.frame?.turn).toBe(12);
    expect(r.messages).toEqual(["You hit it."]);
  });

  it("accepts a prompt, with messages defaulting to none", () => {
    const r = parseCommandResponse({ prompt: { kind: "direction", title: "Throw", lines: ["Which way?"] } });
    expect(r.prompt?.kind).toBe("direction");
    const c = parseCommandResponse({ prompt: { kind: "character", title: "Scroll", lines: ["Which?"] } });
    expect(c.prompt?.kind).toBe("character");
    expect(r.messages).toEqual([]);
  });

  it("requires exactly one of frame and prompt", () => {
    expect(() => parseCommandResponse({ messages: [] })).toThrow(ProtocolError);
    expect(() =>
      parseCommandResponse({ frame: exampleFrame, prompt: { kind: "choose", title: "", lines: [] } }),
    ).toThrow(ProtocolError);
  });

  it("rejects an unknown prompt kind", () => {
    expect(() => parseCommandResponse({ prompt: { kind: "riddle", title: "", lines: [] } })).toThrow(
      ProtocolError,
    );
  });
});
