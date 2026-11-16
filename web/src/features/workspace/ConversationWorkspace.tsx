"use client";
// A conversation: the trip stage fills the page and Kora lives in the orb.
// The orb mirrors the run — thinking while it works, acting when it moves the
// page, done when the plan is ready — and speaks up proactively when it has
// something to offer (finished packages, a clarifying question).
import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useShallow } from "zustand/react/shallow";
import { api } from "@/lib/api/client";
import { useWorkspace } from "@/lib/workspace/store";
import { useT } from "@/i18n";
import { ChatView } from "@/features/chat/ChatView";
import { OrbDock } from "@/features/orb/OrbDock";
import { useOrb, type OrbMode } from "@/features/orb/store";
import { ConversationProvider, useConversation } from "./ConversationContext";
import { Stage } from "./Stage";

export function ConversationWorkspace({ id }: { id: string }) {
  return (
    <ConversationProvider id={id}>
      <Workspace />
    </ConversationProvider>
  );
}

function Workspace() {
  const t = useT();
  const open = useOrb((s) => s.open);
  const { conversationId, sendText, sendAction } = useConversation();
  const msgs = useQuery({ queryKey: ["messages", conversationId], queryFn: () => api.messages(conversationId) });
  const { live, status, pulse, focusReason, phaseLabel, tools, runId } = useWorkspace(
    useShallow((s) => ({
      live: s.live,
      status: s.status,
      pulse: s.pulse,
      focusReason: s.focusReason,
      phaseLabel: s.ws?.phase?.label ?? null,
      tools: s.tools,
      runId: s.runId,
    })),
  );
  const running = live && status === "running";

  // "acting" for a moment after every focus action Kora takes.
  const [acting, setActing] = useState(false);
  useEffect(() => {
    if (!pulse || !live) return;
    setActing(true);
    const h = setTimeout(() => setActing(false), 1800);
    return () => clearTimeout(h);
  }, [pulse, live]);

  // "done" for a while after a live run completes, plus a proactive nudge.
  const [done, setDone] = useState(false);
  const prev = useRef(status);
  useEffect(() => {
    const was = prev.current;
    prev.current = status;
    if (!live || was !== "running" || status !== "completed") return;
    setDone(true);
    const h = setTimeout(() => setDone(false), 6000);
    const ws = useWorkspace.getState().ws;
    const orb = useOrb.getState();
    if (ws?.clarification?.question) {
      orb.showNudge({
        id: `live:clarify:${runId}`,
        text: `${t.orb.nudges.clarify} ${ws.clarification.question}`,
        chips: (ws.clarification.options ?? []).slice(0, 4).map((o) => ({ label: o, onPick: () => sendText(o) })),
      });
    } else if (ws && ws.packages.length > 0) {
      orb.showNudge({
        id: `live:done:${runId}`,
        text: ws.recommendation?.reason ? `${t.orb.nudges.done} ${ws.recommendation.reason}` : t.orb.nudges.done,
        chips: ws.suggestions.slice(0, 3).map((s) => ({ label: s.label, onPick: () => sendAction({ ...s.action, label: s.label }) })),
      });
    }
    return () => clearTimeout(h);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, live]);

  // A brand-new conversation opens the panel so the traveller can start typing.
  const opened = useRef(false);
  useEffect(() => {
    if (opened.current || !msgs.data) return;
    opened.current = true;
    if (msgs.data.items.length === 0) useOrb.getState().setOpen(true);
  }, [msgs.data]);

  const mode: OrbMode = acting ? "acting" : running ? "thinking" : done ? "done" : "idle";
  const lastTool = [...tools].reverse().find((x) => !x.done)?.label;
  const caption = acting ? (focusReason ?? t.orb.acting) : running ? (lastTool ?? phaseLabel ?? null) : done ? t.orb.done : null;

  return (
    <>
      <div className={`transition-[padding] duration-500 ease-[cubic-bezier(.22,1,.36,1)] ${open ? "md:pr-[430px]" : ""}`}>
        <Stage />
      </div>
      <OrbDock mode={mode} caption={caption} panel={<ChatView />} />
    </>
  );
}
