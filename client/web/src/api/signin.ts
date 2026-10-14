// Signing in with Cognito's hosted page, by the OAuth 2.0 authorization code
// flow with PKCE (RFC 7636).
//
// The browser is sent to the hosted page with a hash of a secret it keeps,
// and comes back with a one-time code. Exchanging the code needs the secret
// itself, so a code intercepted on the way back is useless to anyone else.
// No password ever passes through this app.
//
// Tokens are kept in sessionStorage: they last as long as the tab, and a new
// tab signs in again (instantly, while Cognito's own session lasts).

import type { SignInConfig } from "./protocol";

/** The storage this keeps its state in; sessionStorage in the app. */
export interface KeyValueStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

/** What SignIn needs from the browser, replaceable in tests. */
export interface SignInDeps {
  storage: KeyValueStore;
  fetch: typeof fetch;
  /** The time, in milliseconds. */
  now: () => number;
}

/** Tokens as stored. `expiresAt` is when the access token expires, in ms. */
interface Tokens {
  access: string;
  refresh?: string;
  expiresAt: number;
}

const TOKENS = "micro.tokens";
const PENDING = "micro.signIn";

/** An access token this close to expiry is refreshed rather than sent. */
const REFRESH_MARGIN_MS = 60_000;

export class SignIn {
  private readonly deps: SignInDeps;

  /** redirectUri is where the hosted page sends the browser back to, and
   * must be one of the app client's callback URLs exactly. */
  constructor(
    private readonly cfg: SignInConfig,
    private readonly redirectUri: string,
    deps?: Partial<SignInDeps>,
  ) {
    // The browser's own are looked up only when not replaced, so tests can
    // run where there is no sessionStorage.
    this.deps = {
      storage: deps?.storage ?? sessionStorage,
      fetch: deps?.fetch ?? ((...args) => fetch(...args)),
      now: deps?.now ?? Date.now,
    };
  }

  /** The hosted page's address, to send the browser to. It remembers the
   * secret the answer will need. */
  async signInUrl(): Promise<string> {
    const verifier = randomString();
    const state = randomString();
    this.deps.storage.setItem(PENDING, JSON.stringify({ verifier, state }));
    const q = new URLSearchParams({
      response_type: "code",
      client_id: this.cfg.client_id,
      redirect_uri: this.redirectUri,
      scope: "openid email",
      code_challenge_method: "S256",
      code_challenge: await challengeFor(verifier),
      state,
    });
    return `https://${this.cfg.domain}/oauth2/authorize?${q}`;
  }

  /** If url is the hosted page sending the browser back, exchanges its
   * code for tokens. Reports whether it did, so the caller can tidy the
   * code out of the address bar. */
  async finishSignIn(url: URL): Promise<boolean> {
    const code = url.searchParams.get("code");
    const state = url.searchParams.get("state");
    if (!code || !state) return false;

    const raw = this.deps.storage.getItem(PENDING);
    this.deps.storage.removeItem(PENDING);
    const pending = raw ? (JSON.parse(raw) as { verifier: string; state: string }) : null;
    // A state this tab did not send means the answer is not to its own
    // request: someone else's link, or a replay. Ignore it.
    if (!pending || pending.state !== state) return false;

    await this.requestTokens({
      grant_type: "authorization_code",
      code,
      redirect_uri: this.redirectUri,
      code_verifier: pending.verifier,
    });
    return true;
  }

  /** The access token to send, refreshed first if it is about to expire.
   * Null means the user must sign in. */
  token = async (): Promise<string | null> => {
    const t = this.stored();
    if (!t) return null;
    if (this.deps.now() < t.expiresAt - REFRESH_MARGIN_MS) return t.access;
    if (!t.refresh) return this.forget();
    try {
      // Cognito does not send a new refresh token; the old one stays good
      // until it expires.
      await this.requestTokens({ grant_type: "refresh_token", refresh_token: t.refresh }, t.refresh);
      return this.stored()?.access ?? null;
    } catch {
      return this.forget();
    }
  };

  /** Forgets this tab's tokens and returns the hosted page's sign-out
   * address, which ends Cognito's own session too. */
  signOutUrl(): string {
    this.forget();
    const q = new URLSearchParams({ client_id: this.cfg.client_id, logout_uri: this.redirectUri });
    return `https://${this.cfg.domain}/logout?${q}`;
  }

  private async requestTokens(form: Record<string, string>, keepRefresh?: string): Promise<void> {
    const res = await this.deps.fetch(`https://${this.cfg.domain}/oauth2/token`, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ client_id: this.cfg.client_id, ...form }),
    });
    if (!res.ok) throw new Error(`sign-in failed: ${res.status} ${res.statusText}`);
    const body = (await res.json()) as {
      access_token?: unknown;
      refresh_token?: unknown;
      expires_in?: unknown;
    };
    if (typeof body.access_token !== "string" || typeof body.expires_in !== "number") {
      throw new Error("sign-in failed: the token response has no access token");
    }
    const refresh = typeof body.refresh_token === "string" ? body.refresh_token : keepRefresh;
    const tokens: Tokens = {
      access: body.access_token,
      expiresAt: this.deps.now() + body.expires_in * 1000,
      ...(refresh ? { refresh } : {}),
    };
    this.deps.storage.setItem(TOKENS, JSON.stringify(tokens));
  }

  private stored(): Tokens | null {
    const raw = this.deps.storage.getItem(TOKENS);
    return raw ? (JSON.parse(raw) as Tokens) : null;
  }

  private forget(): null {
    this.deps.storage.removeItem(TOKENS);
    return null;
  }
}

/** 32 random bytes, base64url-encoded: a PKCE verifier, or a state. */
function randomString(): string {
  return base64url(crypto.getRandomValues(new Uint8Array(32)));
}

/** PKCE's S256 challenge: the verifier's SHA-256, base64url-encoded. */
async function challengeFor(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier));
  return base64url(new Uint8Array(digest));
}

function base64url(bytes: Uint8Array): string {
  return btoa(String.fromCharCode(...bytes))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}
