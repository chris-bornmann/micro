// GridRenderer on rot.js's canvas Display. rot.js draws a character grid well;
// we use nothing else of it, since the server already does field of view,
// pathfinding and map generation.

import Display from "rot-js/lib/display/display.js";
import { cellAt, type Cell, type Grid, type GridRenderer } from "./renderer";

export interface RotOptions {
  fontFamily?: string;
  background?: string;
}

export class RotRenderer implements GridRenderer {
  private display: Display | undefined;
  private container: HTMLElement | undefined;
  private shown: Grid | undefined;
  private readonly fontFamily: string;
  private readonly background: string;

  constructor(opts: RotOptions = {}) {
    this.fontFamily = opts.fontFamily ?? "ui-monospace, Menlo, Consolas, monospace";
    this.background = opts.background ?? "#000000";
  }

  mount(container: HTMLElement): void {
    this.container = container;
    this.display = new Display({
      width: 1,
      height: 1,
      fontFamily: this.fontFamily,
      bg: this.background,
      fg: "#ffffff",
    });
    const canvas = this.display.getContainer();
    if (canvas) container.appendChild(canvas);
  }

  draw(grid: Grid): void {
    const d = this.display;
    if (!d) return;
    const prev = this.shown;
    this.shown = grid;
    if (!prev || prev.width !== grid.width || prev.height !== grid.height) {
      d.setOptions({ width: grid.width, height: grid.height });
      this.resize(); // resizes and redraws everything
      return;
    }
    // Only cells that changed: most of a turn's screen is the same.
    for (let y = 0; y < grid.height; y++) {
      for (let x = 0; x < grid.width; x++) {
        const now = cellAt(grid, x, y);
        const was = cellAt(prev, x, y);
        if (now && (!was || now.glyph !== was.glyph || now.color !== was.color)) this.drawCell(x, y, now);
      }
    }
  }

  resize(): void {
    const d = this.display;
    const box = this.container;
    if (!d || !box || !this.shown) return;
    // Draw at device pixels, display at CSS pixels: sharp glyphs on HiDPI.
    const dpr = window.devicePixelRatio || 1;
    const size = d.computeFontSize(box.clientWidth * dpr, box.clientHeight * dpr);
    d.setOptions({ fontSize: Math.max(size, 6) });
    const canvas = d.getContainer() as HTMLCanvasElement | null;
    if (canvas) {
      canvas.style.width = `${canvas.width / dpr}px`;
      canvas.style.height = `${canvas.height / dpr}px`;
    }
    const g = this.shown;
    d.clear();
    for (let y = 0; y < g.height; y++) {
      for (let x = 0; x < g.width; x++) {
        const c = cellAt(g, x, y);
        if (c) this.drawCell(x, y, c);
      }
    }
  }

  destroy(): void {
    this.display?.getContainer()?.remove();
    this.display = undefined;
    this.container = undefined;
    this.shown = undefined;
  }

  private drawCell(x: number, y: number, c: Cell): void {
    this.display?.draw(x, y, c.glyph === " " ? null : c.glyph, c.color, this.background);
  }
}
