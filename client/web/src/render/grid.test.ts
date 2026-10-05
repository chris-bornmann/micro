import { describe, expect, it } from "vitest";
import { gridFromFrame } from "./grid";
import { cellAt } from "./renderer";
import { brighten } from "./palette";

const frame = {
  width: 3,
  height: 2,
  rows: ["#.@", " 𐀀 "],
  colors: [
    [1, 2, 3],
    [0, 3, 0],
  ],
  palette: ["#000000", "#616161", "#ada78c", "#ffffff"],
};

describe("gridFromFrame", () => {
  it("maps each cell to its glyph and palette color", () => {
    const g = gridFromFrame(frame);
    expect(cellAt(g, 0, 0)).toEqual({ glyph: "#", color: "#616161" });
    expect(cellAt(g, 2, 0)).toEqual({ glyph: "@", color: "#ffffff" });
    expect(cellAt(g, 0, 1)).toEqual({ glyph: " ", color: "#000000" });
  });

  it("keeps a glyph outside the basic plane in one cell", () => {
    const g = gridFromFrame(frame);
    expect(cellAt(g, 1, 1)?.glyph).toBe("𐀀");
    expect(cellAt(g, 2, 1)?.glyph).toBe(" ");
  });

  it("restyles through the palette", () => {
    const g = gridFromFrame(frame, () => "#ff0000");
    expect(g.cells.every((c) => c.color === "#ff0000")).toBe(true);
  });

  it("returns nothing off the grid", () => {
    expect(cellAt(gridFromFrame(frame), 3, 0)).toBeUndefined();
  });
});

describe("brighten", () => {
  // The Go client's gamma curve at 1.6 (client/internal/render/brighten.go).
  it.each([
    ["#3c3c4c", 1.6, "#676778"],
    ["#000000", 1.6, "#000000"],
    ["#ffffff", 1.6, "#ffffff"],
    ["#3c3c4c", 1, "#3c3c4c"],
  ])("brighten(%s, %d) = %s", (hex, amount, want) => {
    expect(brighten(hex, amount)).toBe(want);
  });
});
