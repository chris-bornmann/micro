// Wiring: the one place that knows every layer. It builds the store, the API
// client, the controller and the renderer, and connects the keyboard to them.
// When the server has accounts, it also signs the player in first.

import { render } from "preact";
import { HttpApi, type TokenSource } from "./api/client";
import { SignIn } from "./api/signin";
import { interpret } from "./input/keymap";
import { RotRenderer } from "./render/rot";
import { Controller, sessionIds } from "./state/controller";
import { createStore } from "./state/game";
import { App } from "./ui/App";

/** Where the address's query is kept while the player is away signing in:
 * the hosted page sends them back to a bare URL, and ?seed would be lost. */
const QUERY_KEY = "micro.query";

const params = new URLSearchParams(sessionStorage.getItem(QUERY_KEY) ?? location.search);
sessionStorage.removeItem(QUERY_KEY);
const seedParam = params.get("seed");
const seed = seedParam !== null && /^-?\d+$/.test(seedParam) ? Number(seedParam) : undefined;
const brightness = Number(params.get("brightness") ?? "1.6") || 1.6;

const store = createStore();
const base = import.meta.env.VITE_API_URL ?? "";
const createRenderer = () => new RotRenderer();

const root = document.getElementById("app");
if (root) render(<App store={store} createRenderer={createRenderer} brightness={brightness} />, root);

/** Sends the browser to the sign-in page. Nothing runs after it. */
async function goSignIn(signIn: SignIn): Promise<null> {
  sessionStorage.setItem(QUERY_KEY, params.toString());
  location.assign(await signIn.signInUrl());
  return null;
}

/** Signs the player in if the server wants that, and returns how requests
 * get their token: undefined when there is no signing in to do, null when
 * the browser is on its way to the sign-in page. */
async function signInIfNeeded(): Promise<TokenSource | undefined | null> {
  const { sign_in } = await new HttpApi(base).config();
  if (!sign_in) return undefined;

  // The callback URL must match one of the app client's exactly, so it is
  // always the site's root (see deploy/cognito.tf).
  const signIn = new SignIn(sign_in, `${location.origin}/`);
  if (await signIn.finishSignIn(new URL(location.href))) {
    // Take the one-time code out of the address bar and the history.
    history.replaceState(null, "", `/${params.size ? `?${params}` : ""}`);
  }
  if (!(await signIn.token())) return goSignIn(signIn);
  // A token that can no longer be refreshed (after 30 days, or if the user
  // was removed) means signing in again.
  return async () => (await signIn.token()) ?? goSignIn(signIn);
}

async function main(): Promise<void> {
  const token = await signInIfNeeded();
  if (token === null) return;
  const controller = new Controller(store, new HttpApi(base, token), sessionIds());

  window.addEventListener("keydown", (e) => {
    const intent = interpret(e, store.mode.value);
    if (!intent) return;
    e.preventDefault(); // arrows must not scroll the page
    void controller.handle(intent);
  });
  await controller.resume(seed);
}

main().catch((e: unknown) => {
  store.error.value = e instanceof Error ? e.message : String(e);
});
