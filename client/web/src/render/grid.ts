// Turning the server's frame into a Grid: pure data, no drawing.

import type { Cell, Grid } from "./renderer";

/** The part of a frame a grid is made from — kept structural, so render
 * does not depend on the api layer. */
export interface GridSource {
  width: number;
  height: number;
  rows: string[];
  colors: number[][];
  palette: string[];
}

/**
 * Builds a Grid from a frame. Rows are split by code point, never by UTF-16
 * unit, so any glyph is one cell. restyle may substitute palette colors —
 * the protocol's way for a client to restyle without learning what a color
 * means.
 */
export function gridFromFrame(f: GridSource, restyle: (color: string) => string = (c) => c): Grid {
  const palette = f.palette.map(restyle);
  const cells: Cell[] = [];
  for (let y = 0; y < f.height; y++) {
    const glyphs = [...(f.rows[y] ?? "")];
    const colors = f.colors[y] ?? [];
    for (let x = 0; x < f.width; x++) {
      cells.push({ glyph: glyphs[x] ?? " ", color: palette[colors[x] ?? 0] ?? "#000000" });
    }
  }
  return { width: f.width, height: f.height, cells };
}
