// Typed fetch client for the Go API. Types come only from the generated
// schema (ENGINEERING_STANDARDS §1.2). The CSRF token from GET /me is sent on
// every mutating request; a missing session is bootstrapped as a guest.
import type { components } from "./schema";

export type Schemas = components["schemas"];
export type Me = Schemas["Me"];
export type Conversation = Schemas["Conversation"];
export type ConversationSummary = Schemas["ConversationSummary"];
export type Message = Schemas["Message"];
export type Run = Schemas["Run"];
export type Package = Schemas["Package"];
export type PackageCard = Schemas["PackageCard"];
export type PackageItem = Schemas["PackageItem"];
export type TripState = Schemas["TripState"];
export type Assumption = Schemas["Assumption"];
export type Money = Schemas["Money"];
export type Meta = Schemas["Meta"];
export type RepriceResult = Schemas["RepriceResult"];
export type DestinationEstimate = Schemas["DestinationEstimate"];
export type Problem = Schemas["Problem"];
export type Workspace = Schemas["Workspace"];
export type WorkspacePackage = Schemas["WorkspacePackage"];
export type FlightCard = Schemas["FlightCard"];
export type StayCard = Schemas["StayCard"];
export type ItineraryDay = Schemas["ItineraryDay"];
export type Action = Schemas["Action"];
export type Suggestion = Schemas["Suggestion"];
export type StreamLine = Schemas["StreamLine"];
export type PatchOp = Schemas["PatchOp"];
export type UIAction = Schemas["UIAction"];
export type EffortDecision = Schemas["EffortDecision"];

export const API_URL = (process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080").replace(/\/$/, "");
const BASE = `${API_URL}/api/v1`;

let csrf: string | null = null;

export function setCsrf(token: string | null) {
  csrf = token;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly detail?: string;
  readonly retryAfter?: number;
  constructor(status: number, problem: Partial<Problem>, retryAfter?: number) {
    super(problem.title ?? `HTTP ${status}`);
    this.status = status;
    this.code = problem.code ?? "internal_error";
    this.detail = problem.detail;
    this.retryAfter = retryAfter;
  }
}

type Method = "GET" | "POST" | "PATCH" | "DELETE";

async function request<T>(method: Method, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<T> {
  const h: Record<string, string> = { Accept: "application/json", ...headers };
  if (body !== undefined) h["Content-Type"] = "application/json";
  if (method !== "GET" && csrf) h["X-CSRF-Token"] = csrf;
  const res = await fetch(`${BASE}${path}`, {
    method,
    headers: h,
    credentials: "include",
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204 || res.status === 202) {
    const text = await res.text();
    return (text ? JSON.parse(text) : undefined) as T;
  }
  const text = await res.text();
  const data: unknown = text ? JSON.parse(text) : undefined;
  if (!res.ok) {
    const ra = Number(res.headers.get("Retry-After") ?? "");
    throw new ApiError(res.status, (data ?? {}) as Partial<Problem>, Number.isFinite(ra) && ra > 0 ? ra : undefined);
  }
  return data as T;
}

/** Ensures a session exists (guest if needed) and returns the profile. */
export async function bootstrap(): Promise<Me> {
  try {
    const me = await request<Me>("GET", "/me");
    setCsrf(me.csrf_token);
    return me;
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      const me = await request<Me>("POST", "/auth/guest");
      setCsrf(me.csrf_token);
      return me;
    }
    throw e;
  }
}

function idem(): string {
  return typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`;
}

export const api = {
  me: () => request<Me>("GET", "/me"),
  meta: () => request<Meta>("GET", "/meta"),
  patchMe: (b: { display_name?: string; home_city_code?: string; locale?: string; memory_enabled?: boolean }) => request<Me>("PATCH", "/me", b),
  deleteMe: () => request<void>("DELETE", "/me"),
  exportUrl: () => `${BASE}/me/export`,
  magicLink: (email: string) => request<void>("POST", "/auth/magic-link", { email }),
  verify: async (token: string) => {
    const me = await request<Me>("POST", "/auth/magic-link/verify", { token });
    setCsrf(me.csrf_token);
    return me;
  },
  logout: () => request<void>("POST", "/auth/logout"),

  conversations: () => request<{ items: ConversationSummary[]; next_cursor: string | null }>("GET", "/conversations?limit=50"),
  createConversation: () => request<Conversation>("POST", "/conversations"),
  conversation: (id: string) => request<Conversation>("GET", `/conversations/${id}`),
  renameConversation: (id: string, title: string) => request<Conversation>("PATCH", `/conversations/${id}`, { title }),
  deleteConversation: (id: string) => request<void>("DELETE", `/conversations/${id}`),
  messages: (id: string) => request<{ items: Message[]; next_cursor: string | null }>("GET", `/conversations/${id}/messages?limit=100`),
  send: (id: string, content: string, key = idem()) =>
    request<{ message: Message; run: Run }>("POST", `/conversations/${id}/messages`, { content }, { "Idempotency-Key": key }),
  patchTripState: (id: string, version: number, set: { nights?: number; pax?: { adults?: number; children?: number }; budget?: Money }) =>
    request<TripState>("PATCH", `/conversations/${id}/trip-state`, { version, set }),

  run: (id: string) => request<Run>("GET", `/runs/${id}`),
  cancelRun: (id: string) => request<void>("POST", `/runs/${id}/cancel`),
  streamUrl: (id: string) => `${BASE}/runs/${id}/stream`,
  eventsUrl: (id: string, after = 0) => `${BASE}/runs/${id}/events?after=${after}`,
  action: (id: string, action: Action, key = idem()) =>
    request<{ message: Message; run: Run }>("POST", `/conversations/${id}/actions`, action, { "Idempotency-Key": key }),

  pkg: (id: string) => request<Package>("GET", `/packages/${id}`),
  selectPkg: (id: string) => request<Package>("POST", `/packages/${id}/select`),
  reprice: (id: string) => request<RepriceResult>("POST", `/packages/${id}/reprice`),
  outbound: (pkg: string, item: string) => request<{ url: string }>("POST", `/packages/${pkg}/items/${item}/outbound`),

  feedback: (b: { message_id?: string; package_id?: string; rating: 1 | -1; reason?: string; comment?: string }) => request<void>("POST", "/feedback", b),
  createReminder: (conversation_id: string, kind: "booking_window_open" | "price_recheck") =>
    request<Schemas["Reminder"]>("POST", "/reminders", { conversation_id, kind }),
};
