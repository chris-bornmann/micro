import { useEffect, useRef } from "preact/hooks";
import { useSignalEffect } from "@preact/signals";
import type { Store } from "../state/game";
import { gridFromFrame } from "../render/grid";
import { brighten } from "../render/palette";
import type { GridRenderer } from "../render/renderer";

interface Props {
  store: Store;
  /** Makes the renderer. Any GridRenderer will do: this is where a richer
   * one is swapped in. */
  createRenderer: () => GridRenderer;
  /** Gamma lift for dark colors; 1 shows the server's colors unchanged. */
  brightness: number;
}

/** The map: hosts a GridRenderer and feeds it a Grid each turn. */
export function MapView({ store, createRenderer, brightness }: Props) {
  const box = useRef<HTMLDivElement>(null);
  const renderer = useRef<GridRenderer | null>(null);

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const r = createRenderer();
    r.mount(el);
    renderer.current = r;
    const f = store.frame.peek();
    if (f) r.draw(gridFromFrame(f, (c) => brighten(c, brightness)));
    const observer = new ResizeObserver(() => r.resize());
    observer.observe(el);
    return () => {
      observer.disconnect();
      r.destroy();
      renderer.current = null;
    };
  }, [createRenderer, brightness, store]);

  useSignalEffect(() => {
    const f = store.frame.value;
    if (f) renderer.current?.draw(gridFromFrame(f, (c) => brighten(c, brightness)));
  });

  return <div class="map" ref={box} role="img" aria-label="Dungeon map" />;
}
