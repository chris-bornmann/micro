// The wire format, as PROTOCOL.md describes it. These types are this
// client's own declaration of the contract: the server and the clients share
// no code, so nothing but that document and these checks keeps them in step.
//
// Every response is checked at runtime before the rest of the app sees it,
// so a server that has drifted from the protocol produces a clear error
// rather than a garbled screen.

/** One screen: a character grid plus pre-formatted text. */
export interface Frame {
  turn: number;
  title: string;
  level: number;
  width: number;
  height: number;
  /** One string per row, `width` code points each. */
  rows: string[];
  /** A palette index per cell, `height` arrays of `width`. */
  colors: number[][];
  /** "#rrggbb" values that `colors` indexes into. */
  palette: string[];
  /** Pre-formatted "Label: Value" lines. */
  hud: string[];
  game_over: boolean;
}

/** An interaction the last key opened instead of spending a turn. */
export interface Prompt {
  /** "character": answer with one character exactly as typed — case matters. */
  kind: "choose" | "direction" | "character";
  title: string;
  lines: string[];
}

/** A read-only view. */
export interface View {
  name: string;
  title: string;
  lines: string[];
}

/** Exactly one of `frame` and `prompt` is set. */
export type CommandResponse =
  | { messages: string[]; frame: Frame; prompt?: undefined }
  | { messages: string[]; frame?: undefined; prompt: Prompt };

export interface NewGameResponse {
  game_id: string;
  frame: Frame;
}

export type ViewName = "inventory" | "look" | "status" | "help";

/** The response did not match the protocol. */
export class ProtocolError extends Error {
  override name = "ProtocolError";
}

type Json = unknown;

function fail(what: string): never {
  throw new ProtocolError(`server response does not match the protocol: ${what}`);
}

function obj(v: Json, what: string): Record<string, Json> {
  if (typeof v !== "object" || v === null || Array.isArray(v)) fail(`${what} is not an object`);
  return v as Record<string, Json>;
}

function str(v: Json, what: string): string {
  if (typeof v !== "string") fail(`${what} is not a string`);
  return v;
}

function int(v: Json, what: string): number {
  if (typeof v !== "number" || !Number.isInteger(v)) fail(`${what} is not an integer`);
  return v;
}

function strings(v: Json, what: string): string[] {
  if (!Array.isArray(v)) fail(`${what} is not a list`);
  return v.map((s, i) => str(s, `${what}[${i}]`));
}

const hexColor = /^#[0-9a-fA-F]{6}$/;

/** Checks a frame, including that its grid's dimensions agree. */
export function parseFrame(v: Json): Frame {
  const o = obj(v, "frame");
  const width = int(o.width, "frame.width");
  const height = int(o.height, "frame.height");
  const rows = strings(o.rows, "frame.rows");
  const palette = strings(o.palette, "frame.palette");
  if (!Array.isArray(o.colors)) fail("frame.colors is not a list");
  const colors = o.colors.map((row: Json, y: number) => {
    if (!Array.isArray(row)) fail(`frame.colors[${y}] is not a list`);
    return row.map((c: Json, x: number) => int(c, `frame.colors[${y}][${x}]`));
  });

  if (rows.length !== height || colors.length !== height) {
    fail(`frame has ${rows.length} rows and ${colors.length} color rows, want ${height}`);
  }
  rows.forEach((row, y) => {
    // Rows are measured in code points, never UTF-16 units: a glyph outside
    // the basic plane is still one cell.
    if ([...row].length !== width) fail(`frame.rows[${y}] has ${[...row].length} cells, want ${width}`);
    const cr = colors[y] ?? [];
    if (cr.length !== width) fail(`frame.colors[${y}] has ${cr.length} entries, want ${width}`);
    cr.forEach((c, x) => {
      if (c < 0 || c >= palette.length) fail(`frame.colors[${y}][${x}] = ${c} is outside the palette`);
    });
  });
  palette.forEach((p, i) => {
    if (!hexColor.test(p)) fail(`frame.palette[${i}] = ${p} is not #rrggbb`);
  });
  if (typeof o.game_over !== "boolean") fail("frame.game_over is not a boolean");

  return {
    turn: int(o.turn, "frame.turn"),
    title: str(o.title, "frame.title"),
    level: int(o.level, "frame.level"),
    width,
    height,
    rows,
    colors,
    palette,
    hud: o.hud === undefined ? [] : strings(o.hud, "frame.hud"),
    game_over: o.game_over,
  };
}

export function parsePrompt(v: Json): Prompt {
  const o = obj(v, "prompt");
  const kind = str(o.kind, "prompt.kind");
  if (kind !== "choose" && kind !== "direction" && kind !== "character")
    fail(`prompt.kind ${kind} is unknown`);
  return { kind, title: str(o.title, "prompt.title"), lines: strings(o.lines, "prompt.lines") };
}

export function parseCommandResponse(v: Json): CommandResponse {
  const o = obj(v, "command response");
  const messages = o.messages === undefined ? [] : strings(o.messages, "messages");
  if ((o.frame === undefined) === (o.prompt === undefined)) {
    fail("a command response must have exactly one of frame and prompt");
  }
  return o.frame !== undefined
    ? { messages, frame: parseFrame(o.frame) }
    : { messages, prompt: parsePrompt(o.prompt) };
}

export function parseNewGame(v: Json): NewGameResponse {
  const o = obj(v, "new game response");
  return { game_id: str(o.game_id, "game_id"), frame: parseFrame(o.frame) };
}

export function parseFrameResponse(v: Json): Frame {
  return parseFrame(obj(v, "frame response").frame);
}

export function parseView(v: Json): View {
  const o = obj(v, "view");
  return {
    name: str(o.name, "view.name"),
    title: str(o.title, "view.title"),
    lines: strings(o.lines, "view.lines"),
  };
}
