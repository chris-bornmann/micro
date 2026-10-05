import { useSignalEffect } from "@preact/signals";
import type { Store } from "../state/game";
import type { GridRenderer } from "../render/renderer";
import { Dialog } from "./Dialog";
import { Hud } from "./Hud";
import { MapView } from "./MapView";
import { MessagePane } from "./MessagePane";
import { StatusBar } from "./StatusBar";

interface Props {
  store: Store;
  createRenderer: () => GridRenderer;
  brightness: number;
}

export function App({ store, createRenderer, brightness }: Props) {
  useSignalEffect(() => {
    const f = store.frame.value;
    if (f) document.title = f.title;
  });

  return (
    <div class="app">
      <main class="board">
        <MapView store={store} createRenderer={createRenderer} brightness={brightness} />
        <Hud store={store} />
      </main>
      <MessagePane store={store} />
      <StatusBar store={store} />
      <Dialog store={store} />
    </div>
  );
}
