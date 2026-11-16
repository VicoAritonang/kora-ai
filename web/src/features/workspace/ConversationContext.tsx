"use client";
// One conversation's runtime: loads it, rebuilds the canvas by replaying
// its last run, follows live runs over NDJSON, and sends text or structured
// actions. Chat column and canvas share it.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { ApiError, api, type Action, type Conversation, type Message } from "@/lib/api/client";
import { followRun } from "@/lib/stream/ndjson";
import { useWorkspace } from "@/lib/workspace/store";
import { useT } from "@/i18n";

type Ctx = {
  conversationId: string;
  conversation: Conversation | undefined;
  busy: boolean;
  error: string | null;
  sendText: (text: string) => void;
  sendAction: (a: Action) => void;
  cancel: () => void;
};

const C = createContext<Ctx | null>(null);

export function useConversation(): Ctx {
  const c = useContext(C);
  if (!c) throw new Error("useConversation outside ConversationProvider");
  return c;
}

export function ConversationProvider({ id, children }: { id: string; children: ReactNode }) {
  const t = useT();
  const qc = useQueryClient();
  const store = useWorkspace();
  const conv = useQuery({ queryKey: ["conversation", id], queryFn: () => api.conversation(id) });
  const [error, setError] = useState<string | null>(null);
  const booted = useRef(false);

  useEffect(() => {
    store.bindConversation(id);
    booted.current = false;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  // On load: follow an active run live, or replay the last one so the
  // canvas shows the current plan.
  useEffect(() => {
    if (!conv.data || booted.current) return;
    booted.current = true;
    if (conv.data.active_run_id) store.startRun(conv.data.active_run_id, true);
    else if (conv.data.last_run_id) store.startRun(conv.data.last_run_id, false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conv.data]);

  const runId = store.runId;
  const live = store.live;
  useEffect(() => {
    if (!runId) return;
    const f = followRun(runId, (line) => {
      useWorkspace.getState().setReconnecting(false);
      useWorkspace.getState().apply(line);
    }, {
      onReconnect: () => useWorkspace.getState().setReconnecting(true),
      onEnd: () => {
        if (!live) return;
        qc.invalidateQueries({ queryKey: ["messages", id] });
        qc.invalidateQueries({ queryKey: ["conversation", id] });
        qc.invalidateQueries({ queryKey: ["conversations"] });
        qc.invalidateQueries({ queryKey: ["me"] });
        qc.invalidateQueries({ queryKey: ["package"] });
      },
    });
    return () => f.stop();
  }, [runId, live, id, qc]);

  const onStarted = (r: { message: Message; run: { id: string } }) => {
    qc.setQueryData<{ items: Message[]; next_cursor: string | null }>(["messages", id], (old) => ({
      items: [...(old?.items ?? []), r.message],
      next_cursor: null,
    }));
    store.startRun(r.run.id, true);
  };
  const onError = (e: unknown) =>
    setError(e instanceof ApiError ? t.errors[e.code] ?? e.detail ?? t.errors.generic! : t.errors.generic!);

  const text = useMutation({ mutationFn: (s: string) => api.send(id, s), onMutate: () => setError(null), onSuccess: onStarted, onError });
  const action = useMutation({ mutationFn: (a: Action) => api.action(id, a), onMutate: () => setError(null), onSuccess: onStarted, onError });

  const busy = text.isPending || action.isPending || (live && store.status === "running");
  return (
    <C.Provider
      value={{
        conversationId: id,
        conversation: conv.data,
        busy,
        error,
        sendText: (s) => !busy && text.mutate(s),
        sendAction: (a) => !busy && action.mutate(a),
        cancel: () => runId && live && void api.cancelRun(runId),
      }}
    >
      {children}
    </C.Provider>
  );
}
