"use client";
// Rebuilds a finished run's workspace off to the side (without touching the
// live store), e.g. to show the tickets Kora found on the tickets page.
import { useEffect, useState } from "react";
import type { PatchOp, Workspace } from "@/lib/api/client";
import { followRun } from "@/lib/stream/ndjson";
import { applyPatch } from "./patch";
import { emptyWorkspace } from "./store";

export function useReplayWorkspace(runId: string | null | undefined): Workspace | null {
  const [ws, setWs] = useState<Workspace | null>(null);
  useEffect(() => {
    if (!runId) return;
    let cur: Workspace | null = null;
    const f = followRun(
      runId,
      (line) => {
        const d = (line.data ?? {}) as Record<string, unknown>;
        if (line.type === "state.reset") cur = { ...emptyWorkspace(), ...(d.state as Workspace) };
        else if (line.type === "state.patch") cur = applyPatch(cur ?? emptyWorkspace(), (d.ops as PatchOp[]) ?? []);
      },
      { onEnd: () => setWs(cur) },
    );
    return () => f.stop();
  }, [runId]);
  return ws;
}
