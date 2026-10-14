import { describe, expect, it, vi } from "vitest";
import { SignIn, type KeyValueStore } from "./signin";

const cfg = { domain: "auth.example.com", client_id: "the-client" };
const redirect = "https://game.example/";

function memoryStore(): KeyValueStore {
  const m = new Map<string, string>();
  return {
    getItem: (k) => m.get(k) ?? null,
    setItem: (k, v) => void m.set(k, v),
    removeItem: (k) => void m.delete(k),
  };
}

/** A fake token endpoint: it records each form it is sent and answers with
 * the next of answers. */
function tokenEndpoint(...answers: { status?: number; body?: unknown }[]) {
  const forms: URLSearchParams[] = [];
  const fetch = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
    expect(String(url)).toBe("https://auth.example.com/oauth2/token");
    forms.push(new URLSearchParams(String(init?.body)));
    const a = answers.shift() ?? { status: 500 };
    return new Response(JSON.stringify(a.body ?? {}), { status: a.status ?? 200 });
  });
  return { fetch, forms };
}

/** Goes to the hosted page and comes back with code, as the browser would. */
async function signInWith(s: SignIn, code = "the-code"): Promise<URLSearchParams> {
  const sent = new URL(await s.signInUrl()).searchParams;
  await s.finishSignIn(new URL(`${redirect}?code=${code}&state=${sent.get("state")}`));
  return sent;
}

describe("SignIn", () => {
  it("sends the browser to the hosted page with a PKCE challenge", async () => {
    const s = new SignIn(cfg, redirect, { storage: memoryStore() });
    const url = new URL(await s.signInUrl());
    expect(url.origin + url.pathname).toBe("https://auth.example.com/oauth2/authorize");
    const q = url.searchParams;
    expect(q.get("client_id")).toBe("the-client");
    expect(q.get("redirect_uri")).toBe(redirect);
    expect(q.get("response_type")).toBe("code");
    expect(q.get("code_challenge_method")).toBe("S256");
    expect(q.get("code_challenge")).toMatch(/^[A-Za-z0-9_-]{43}$/);
  });

  it("exchanges the code, with the secret behind the challenge, for tokens", async () => {
    const { fetch, forms } = tokenEndpoint({
      body: { access_token: "A1", refresh_token: "R1", expires_in: 3600 },
    });
    const s = new SignIn(cfg, redirect, { storage: memoryStore(), fetch, now: () => 0 });
    const sent = await signInWith(s);

    const form = forms[0];
    expect(form?.get("grant_type")).toBe("authorization_code");
    expect(form?.get("code")).toBe("the-code");
    expect(form?.get("redirect_uri")).toBe(redirect);
    const digest = await crypto.subtle.digest(
      "SHA-256",
      new TextEncoder().encode(form?.get("code_verifier") ?? ""),
    );
    const challenge = btoa(String.fromCharCode(...new Uint8Array(digest)))
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "");
    expect(challenge).toBe(sent.get("code_challenge"));
    expect(await s.token()).toBe("A1");
  });

  it("ignores an answer to a request this tab did not make", async () => {
    const { fetch } = tokenEndpoint({ body: { access_token: "A1", expires_in: 3600 } });
    const s = new SignIn(cfg, redirect, { storage: memoryStore(), fetch });
    await s.signInUrl();
    expect(await s.finishSignIn(new URL(`${redirect}?code=c&state=forged`))).toBe(false);
    expect(fetch).not.toHaveBeenCalled();
    expect(await s.token()).toBeNull();
  });

  it("leaves an ordinary address alone", async () => {
    const s = new SignIn(cfg, redirect, { storage: memoryStore() });
    expect(await s.finishSignIn(new URL(`${redirect}?seed=4`))).toBe(false);
  });

  it("refreshes a token about to expire, keeping the refresh token", async () => {
    let now = 0;
    const { fetch, forms } = tokenEndpoint(
      { body: { access_token: "A1", refresh_token: "R1", expires_in: 3600 } },
      { body: { access_token: "A2", expires_in: 3600 } },
      { body: { access_token: "A3", expires_in: 3600 } },
    );
    const s = new SignIn(cfg, redirect, { storage: memoryStore(), fetch, now: () => now });
    await signInWith(s);

    now = 3_590_000; // ten seconds before expiry
    expect(await s.token()).toBe("A2");
    expect(forms[1]?.get("grant_type")).toBe("refresh_token");
    expect(forms[1]?.get("refresh_token")).toBe("R1");

    now += 3_590_000;
    expect(await s.token()).toBe("A3");
    expect(forms[2]?.get("refresh_token")).toBe("R1");
  });

  it("forgets the tokens when they can no longer be refreshed", async () => {
    let now = 0;
    const { fetch } = tokenEndpoint(
      { body: { access_token: "A1", refresh_token: "R1", expires_in: 3600 } },
      { status: 400, body: { error: "invalid_grant" } },
    );
    const s = new SignIn(cfg, redirect, { storage: memoryStore(), fetch, now: () => now });
    await signInWith(s);
    now = 3_600_000;
    expect(await s.token()).toBeNull();
    expect(await s.token()).toBeNull();
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it("signing out forgets the tokens and ends the hosted page's session", async () => {
    const { fetch } = tokenEndpoint({ body: { access_token: "A1", expires_in: 3600 } });
    const s = new SignIn(cfg, redirect, { storage: memoryStore(), fetch, now: () => 0 });
    await signInWith(s);
    const url = new URL(s.signOutUrl());
    expect(url.origin + url.pathname).toBe("https://auth.example.com/logout");
    expect(url.searchParams.get("logout_uri")).toBe(redirect);
    expect(await s.token()).toBeNull();
  });
});
