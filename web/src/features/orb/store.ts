"use client";
// Kora orb: the always-present assistant. Holds whether the chat panel is
// open, the current proactive nudge, and launch state for pages that start
// a new conversation (landing, journeys, tickets).
import { create } from "zustand";

export type OrbMode = "idle" | "nudge" | "thinking" | "acting" | "done";

export type NudgeChip = { label: string; onPick: () => void };
export type Nudge = { id: string; text: string; chips?: NudgeChip[] };

type State = {
  open: boolean;
  nudge: Nudge | null;
  launching: boolean;
  launchError: string | null;
  setOpen: (o: boolean) => void;
  toggle: () => void;
  showNudge: (n: Nudge) => void;
  dismissNudge: () => void;
  setLaunch: (p: { launching: boolean; launchError?: string | null }) => void;
};

const SEEN = "kora.nudges.seen";

function seen(): string[] {
  try {
    return JSON.parse(sessionStorage.getItem(SEEN) ?? "[]") as string[];
  } catch {
    return [];
  }
}
function markSeen(id: string) {
  try {
    sessionStorage.setItem(SEEN, JSON.stringify([...new Set([...seen(), id])]));
  } catch {
    /* storage unavailable: nudges simply may repeat */
  }
}

export const useOrb = create<State>((set, get) => ({
  open: false,
  nudge: null,
  launching: false,
  launchError: null,
  setOpen: (open) => set({ open, ...(open ? { nudge: null } : {}) }),
  toggle: () => get().setOpen(!get().open),
  showNudge: (n) => {
    if (get().open) return;
    if (!n.id.startsWith("live:") && seen().includes(n.id)) return;
    set({ nudge: n });
  },
  dismissNudge: () => {
    const n = get().nudge;
    if (n) markSeen(n.id);
    set({ nudge: null });
  },
  setLaunch: ({ launching, launchError = null }) => set({ launching, launchError }),
}));
