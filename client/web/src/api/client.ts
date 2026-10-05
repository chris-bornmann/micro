// A thin, typed fetch wrapper over the HTTP API. It knows the endpoints and
// nothing about the game: every response is checked by protocol.ts.

import {
  parseCommandResponse,
  parseFrameResponse,
  parseNewGame,
  parseView,
  type CommandResponse,
  type Frame,
  type NewGameResponse,
  type View,
  type ViewName,
} from "./protocol";

/** A non-2xx response. `status` lets callers tell "no such game" (404) apart. */
export class ApiError extends Error {
  override name = "ApiError";
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

/** Everything the app asks of the server. The controller depends on this,
 * not on fetch, so tests can hand it a fake. */
export interface Api {
  newGame(seed?: number): Promise<NewGameResponse>;
  frame(gameId: string): Promise<Frame>;
  command(gameId: string, key: string): Promise<CommandResponse>;
  view(gameId: string, name: ViewName): Promise<View>;
}

export class HttpApi implements Api {
  constructor(private readonly base: string) {}

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
    const init: RequestInit = { method };
    if (body !== undefined) {
      init.headers = { "Content-Type": "application/json" };
      init.body = JSON.stringify(body);
    }
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
      throw new ApiError(res.status, msg);
    }
    return data;
  }
}
