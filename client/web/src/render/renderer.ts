// The seam between the app and whatever draws the map. Everything above this
// file deals in Grids; only an implementation knows about canvases, fonts or
// rot.js. A richer renderer — animated glyphs, tiles, WebGL — implements the
// same interface and nothing else changes.

/** One cell of the map. A glyph of " " means nothing is there. */
export interface Cell {
  glyph: string;
  /** "#rrggbb". */
  color: string;
  /**
   * What is happening to whatever is in the cell, for a renderer that wants
   * to show it — "burning" might flicker. Unused today: the protocol sends no
   * effects yet (see README, "Animation").
   */
  effect?: string;
}

/** A whole screen of cells, row-major. */
export interface Grid {
  width: number;
  height: number;
  cells: Cell[];
}

export interface GridRenderer {
  /** Attach to a container; the renderer fills it. */
  mount(container: HTMLElement): void;
  /** Show grid. Called with a whole new grid each turn; the renderer may
   * redraw only what changed. */
  draw(grid: Grid): void;
  /** The container changed size. */
  resize(): void;
  /** Detach and release everything. */
  destroy(): void;
}

/** The cell at (x, y), or undefined off the grid. */
export function cellAt(g: Grid, x: number, y: number): Cell | undefined {
  if (x < 0 || y < 0 || x >= g.width || y >= g.height) return undefined;
  return g.cells[y * g.width + x];
}
