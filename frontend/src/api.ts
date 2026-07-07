// Typed client for the cyberkube backend. Auth is a JWT set as an HttpOnly
// cookie (cyberkube_token); every call uses credentials: "include" so the
// cookie rides along. The base URL is configurable for dev/staging/prod.

const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? "http://localhost:8080";

export type ChallengeMode = "static" | "dynamic";

export interface Challenge {
  name: string;
  displayName: string;
  category: string;
  description: string;
  mode: ChallengeMode;
  value: number;
  solves: number;
  solvedByTeam: boolean;
  attachments?: string[];
}

export interface Me {
  id: string;
  username: string;
  email: string;
  teamId: string;
}

export interface Team {
  id: string;
  name: string;
  inviteCode?: string;
}

export interface SubmitResult {
  correct: boolean;
  points?: number;
}

export type InstanceStatus = "none" | "pending" | "ready";

export interface Instance {
  status: InstanceStatus;
  connectionInfo?: string;
  expiresAt?: string;
}

export interface ScoreboardEntry {
  teamId: string;
  teamName: string;
  points: number;
}

/**
 * World descriptor (GET /api/v1/world): everything a client needs to render
 * the procedural world deterministically. `challenges` may be absent on
 * older/partial backends — callers fall back to GET /api/challenges.
 */
export interface WorldInfo {
  seed: string;
  generatorVersion: number;
  teamMode: boolean;
  challenges?: Challenge[];
}

/** ApiError carries the HTTP status so callers can branch on 401/403/404/409. */
export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${API_BASE}${path}`, {
      method,
      credentials: "include",
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0, "Cannot reach the server. Is the backend running?");
  }

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  const data: unknown = text ? JSON.parse(text) : undefined;

  if (!res.ok) {
    const msg =
      data && typeof data === "object" && "error" in data
        ? String((data as { error: unknown }).error)
        : `Request failed (${res.status})`;
    throw new ApiError(res.status, msg);
  }
  return data as T;
}

export const api = {
  register: (username: string, email: string, password: string) =>
    request<{ id: string; username: string }>("POST", "/api/register", { username, email, password }),

  login: (login: string, password: string) =>
    request<{ token: string }>("POST", "/api/login", { login, password }),

  me: () => request<Me>("GET", "/api/me"),

  createTeam: (name: string) => request<Team>("POST", "/api/teams", { name }),

  joinTeam: (inviteCode: string) => request<Team>("POST", "/api/teams/join", { inviteCode }),

  myTeam: () => request<Team>("GET", "/api/teams/mine"),

  challenges: () => request<Challenge[]>("GET", "/api/challenges"),

  world: () => request<WorldInfo>("GET", "/api/v1/world"),

  submit: (name: string, flag: string) =>
    request<SubmitResult>("POST", `/api/challenges/${encodeURIComponent(name)}/submit`, { flag }),

  launch: (name: string) =>
    request<Instance>("POST", `/api/challenges/${encodeURIComponent(name)}/launch`),

  instance: (name: string) =>
    request<Instance>("GET", `/api/challenges/${encodeURIComponent(name)}/instance`),

  attachmentUrl: (name: string, filename: string) =>
    `${API_BASE}/api/challenges/${encodeURIComponent(name)}/attachments/${encodeURIComponent(filename)}`,

  scoreboard: () => request<ScoreboardEntry[]>("GET", "/api/scoreboard"),
};
