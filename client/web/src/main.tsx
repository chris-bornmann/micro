// Wiring: the one place that knows every layer. It builds the store, the API
// client, the controller and the renderer, and connects the keyboard to them.

import { render } from "preact";
import { HttpApi } from "./api/client";
import { interpret } from "./input/keymap";
import { RotRenderer } from "./render/rot";
import { Controller, sessionIds } from "./state/controller";
import { createStore } from "./state/game";
import { App } from "./ui/App";

const params = new URLSearchParams(location.search);
const seedParam = params.get("seed");
const seed = seedParam !== null && /^-?\d+$/.test(seedParam) ? Number(seedParam) : undefined;
const brightness = Number(params.get("brightness") ?? "1.6") || 1.6;

const store = createStore();
const api = new HttpApi(import.meta.env.VITE_API_URL ?? "");
const controller = new Controller(store, api, sessionIds());

const createRenderer = () => new RotRenderer();

window.addEventListener("keydown", (e) => {
  const intent = interpret(e, store.mode.value);
  if (!intent) return;
  e.preventDefault(); // arrows must not scroll the page
  void controller.handle(intent);
});

const root = document.getElementById("app");
if (root) render(<App store={store} createRenderer={createRenderer} brightness={brightness} />, root);

void controller.resume(seed);
