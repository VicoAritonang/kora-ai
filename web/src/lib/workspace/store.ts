"use client";
// Workspace store: the live trip canvas, driven by the run's NDJSON stream.
// state.reset replaces it, state.patch applies RFC 6902 ops, ui.action moves
// the user's view (unless they pinned a view themselves). Narration text and
// run status sit beside the canvas for the chat column.
import { create } from "zustand";
import type { StreamLine, UIAction, Workspace, PatchOp } from "@/lib/api/client";
import { applyPatch } from "./patch";

export type View = "brief" | "flights" | "stays" | "itinerary" | "packages" | "destinations";

type ToolStep = { tool: string; label: string; done: boolean; ok?: boolean; summary?: string };

type State = {
  conversationId: string | null;
  runId: string | null;
  live: boolean; // false while replaying a finished run to rebuild the canvas
  ws: Workspace | null;
  view: View;
  follow: boolean; // "Ikuti Kora": accept the agent's focus actions
  focusReason: string | null;
  highlight: { kind: string; id: string; tier?: string } | null;
  pulse: number; // bumps on every agent focus, for a brief animation
  nav: number; // bumps on every user navigation, so the stage scrolls even to the same view
  narration: string;
  tools: ToolStep[];
  status: "idle" | "running" | "completed" | "failed";
  error: string | null;
  reconnecting: boolean;
  bindConversation: (id: string) => void;
  startRun: (runId: string, live: boolean) => void;
  apply: (line: StreamLine) => void;
  setView: (v: View) => void;
  setFollow: (f: boolean) => void;
  setReconnecting: (r: boolean) => void;
};

export const emptyWorkspace = (): Workspace => ({
  run: {}, phase: { current: "idle", history: [] }, effort: {},
  trip: { version: 0, pax: { adults: 0, children: 0 }, interests: [], special_requests: [], assumptions: [] },
  flights: { status: "idle", items: [] }, stays: { status: "idle", items: [] }, activities: [], itinerary: [],
  packages: [], destinations: [], recommendation: null, suggestions: [], notices: [], clarification: null,
});

export const useWorkspace = create<State>((set, get) => ({
  conversationId: null,
  runId: null,
  live: false,
  ws: null,
  view: "brief",
  follow: true,
  focusReason: null,
  highlight: null,
  pulse: 0,
  nav: 0,
  narration: "",
  tools: [],
  status: "idle",
  error: null,
  reconnecting: false,

  bindConversation: (id) => {
    if (get().conversationId === id) return;
    set({ conversationId: id, runId: null, ws: null, view: "brief", narration: "", tools: [], status: "idle", error: null, highlight: null, follow: true });
  },

  startRun: (runId, live) =>
    set({ runId, live, narration: "", tools: [], status: live ? "running" : "idle", error: null, ...(live ? { follow: true } : {}) }),

  apply: (line) => {
    const d = (line.data ?? {}) as Record<string, unknown>;
    const s = get();
    switch (line.type) {
      case "state.reset":
        set({ ws: { ...emptyWorkspace(), ...(d.state as Workspace) } });
        return;
      case "state.patch":
        set({ ws: applyPatch(s.ws ?? emptyWorkspace(), (d.ops as PatchOp[]) ?? []) });
        return;
      case "ui.action": {
        const a = d as unknown as UIAction;
        if (a.action === "focus" && a.view) {
          if (s.follow) set({ view: a.view as View, focusReason: a.reason ?? null, pulse: s.pulse + 1 });
        } else if (a.action === "highlight" && a.target?.id) {
          set({ highlight: { kind: a.target.kind ?? "", id: a.target.id, tier: a.target.tier } });
        }
        return;
      }
      case "message.delta":
        set({ narration: s.narration + String(d.text ?? "") });
        return;
      case "tool.started":
        set({ tools: [...s.tools, { tool: String(d.tool), label: String(d.label), done: false }] });
        return;
      case "tool.finished": {
        const tools = [...s.tools];
        const i = tools.findIndex((t) => t.tool === d.tool && !t.done);
        if (i >= 0) tools[i] = { ...tools[i]!, done: true, ok: Boolean(d.ok), summary: String(d.summary ?? "") };
        set({ tools });
        return;
      }
      case "run.started":
        if (s.live) set({ status: "running" });
        return;
      case "run.completed":
        set({ status: "completed" });
        return;
      case "run.failed":
        set({ status: "failed", error: String(d.message ?? "") });
        return;
    }
  },

  setView: (v) => set({ view: v, follow: false, focusReason: null, nav: get().nav + 1 }),
  setFollow: (f) => set({ follow: f }),
  setReconnecting: (r) => set({ reconnecting: r }),
}));
