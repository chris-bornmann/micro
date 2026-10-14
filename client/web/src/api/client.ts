// A thin, typed fetch wrapper over the HTTP API. It knows the endpoints and
// nothing about the game: every response is checked by protocol.ts.

import {
  parseCommandResponse,
  parseConfig,
  parseMe,
  parseFrameResponse,
  parseNewGame,
  parseView,
  type CommandResponse,
  type Frame,
  type Me,
  type NewGameResponse,
  type ServerConfig,
  type View,
  type ViewName,
} from "./protocol";

/** A non-2xx response. `status` lets callers tell "no such game" (404) apart,
 * and `body` is the response's JSON, for the few errors that carry more than
 * a message (a 409 names the game already in progress). */
export class ApiError extends Error {
  override name = "ApiError";
  constructor(
    readonly status: number,
    message: string,
    readonly body?: unknown,
  ) {
    super(message);
  }
}

/** Supplies the signed-in user's access token for each request, or null
 * when there is none to send. */
export type TokenSource = () => Promise<string | null>;

/** Everything the app asks of the server. The controller depends on this,
 * not on fetch, so tests can hand it a fake. */
export interface Api {
  /** Whether and where to sign in. Needs no sign-in itself. */
  config(): Promise<ServerConfig>;
  me(): Promise<Me>;
  newGame(seed?: number): Promise<NewGameResponse>;
  frame(gameId: string): Promise<Frame>;
  command(gameId: string, key: string): Promise<CommandResponse>;
  view(gameId: string, name: ViewName): Promise<View>;
}

export class HttpApi implements Api {
  /** token, if given, is asked for an access token before every request. */
  constructor(
    private readonly base: string,
    private readonly token?: TokenSource,
  ) {}

  config(): Promise<ServerConfig> {
    return this.call("GET", "/v1/config").then(parseConfig);
  }

  me(): Promise<Me> {
    return this.call("GET", "/v1/me").then(parseMe);
  }

  newGame(seed?: number): Promise<NewGameResponse> {
    return this.call("POST", "/v1/games", seed === undefined ? {} : { seed }).then(parseNewGame);
  }

  frame(gameId: string): Promise<Frame> {
    return this.call("GET", `/v1/games/${encodeURIComponent(gameId)}/frame`).then(parseFrameResponse);
  }

  command(gameId: string, key: string): Promise<CommandResponse> {
    return this.call("POST", `/v1/games/${encodeURIComponent(gameId)}/command`, { key }).then(
      parseCommandResponse,
    );
  }

  view(gameId: string, name: ViewName): Promise<View> {
    return this.call("GET", `/v1/games/${encodeURIComponent(gameId)}/views/${name}`).then(parseView);
  }

  private async call(method: "GET" | "POST", path: string, body?: unknown): Promise<unknown> {
    const headers: Record<string, string> = {};
    const init: RequestInit = { method, headers };
    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(body);
    }
    const token = await this.token?.();
    if (token) headers.Authorization = `Bearer ${token}`;
    let res: Response;
    try {
      res = await fetch(this.base + path, init);
    } catch (e) {
      throw new ApiError(0, `cannot reach the server at ${this.base}: ${String(e)}`);
    }
    const data: unknown = await res.json().catch(() => undefined);
    if (!res.ok) {
      const msg =
        typeof data === "object" && data !== null && "error" in data ? String(data.error) : res.statusText;
      throw new ApiError(res.status, msg, data);
    }
    return data;
  }
}
