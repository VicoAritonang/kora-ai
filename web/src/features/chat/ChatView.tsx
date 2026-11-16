"use client";
// Chat inside the orb panel. Text and canvas actions go through the shared
// conversation context; live narration and tool progress come from the
// workspace store (NDJSON stream).
import { useMutation, useQuery } from "@tanstack/react-query";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import { api, type Message } from "@/lib/api/client";
import { useShallow } from "zustand/react/shallow";
import { useWorkspace, type View } from "@/lib/workspace/store";
import { useT } from "@/i18n";
import { Orb } from "@/features/orb/Orb";
import { useOrb } from "@/features/orb/store";
import { useConversation } from "@/features/workspace/ConversationContext";
import { Composer } from "./Composer";

/** Moves the stage to a chapter; on phones also closes the panel so it shows. */
function useJump() {
  const setView = useWorkspace((s) => s.setView);
  return (v: View) => {
    setView(v);
    if (typeof window !== "undefined" && window.matchMedia("(max-width: 767px)").matches) useOrb.getState().setOpen(false);
  };
}

export function ChatView() {
  const t = useT();
  const { conversationId, busy, error, sendText, cancel } = useConversation();
  const msgs = useQuery({ queryKey: ["messages", conversationId], queryFn: () => api.messages(conversationId) });
  const { live, status, narration, tools, phase, reconnecting, runError, pulse, focusReason, view } = useWorkspace(
    useShallow((s) => ({
      live: s.live,
      status: s.status,
      narration: s.narration,
      tools: s.tools,
      phase: s.ws?.phase?.label ?? null,
      reconnecting: s.reconnecting,
      runError: s.error,
      pulse: s.pulse,
      focusReason: s.focusReason,
      view: s.view,
    })),
  );
  const jump = useJump();
  const bottom = useRef<HTMLDivElement>(null);
  const items = msgs.data?.items ?? [];
  const running = live && status === "running";
  useEffect(() => {
    bottom.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [items.length, narration.length, tools.length]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <AnimatePresence>
        {running && pulse > 0 && focusReason && (
          <motion.button
            key={pulse}
            onClick={() => jump(view)}
            className="mx-3 mt-2 flex items-center gap-2 rounded-full bg-night px-3 py-1.5 text-left text-xs font-semibold text-white md:hidden"
            initial={{ opacity: 0, y: -8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
          >
            <Orb size={16} mode="acting" />
            <span className="flex-1 truncate">{focusReason}</span>
            <span className="text-gold">{t.stage.see} →</span>
          </motion.button>
        )}
      </AnimatePresence>
      <div className="flex-1 overflow-y-auto px-4 py-4">
        {items.length === 0 && !busy && <Welcome onPick={sendText} />}
        {items.map((m, i) => (
          <MessageRow key={m.id} m={m} latest={i === items.length - 1} onOption={sendText} />
        ))}
        {running && (
          <motion.div className="my-3 flex gap-2.5" aria-live="polite" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }}>
            <Orb size={28} mode="thinking" className="mt-1" />
            <div className="min-w-0 flex-1 rounded-3xl rounded-tl-md border border-line bg-card px-4 py-3">
              {phase && <p className="text-xs font-bold uppercase tracking-[.16em] text-brand">{phase}</p>}
              {reconnecting && <p className="mt-1 text-xs text-est">{t.errors.network}</p>}
              {tools.length > 0 && (
                <ul className="mt-2 space-y-1.5 text-sm">
                  <AnimatePresence initial={false}>
                    {tools.map((s, i) => (
                      <motion.li key={i} className="flex items-start gap-2" initial={{ opacity: 0, x: -8 }} animate={{ opacity: 1, x: 0 }}>
                        <span className={`mt-0.5 grid h-4 w-4 shrink-0 place-items-center rounded-full text-[9px] font-bold ${s.done ? (s.ok ? "bg-live text-white" : "bg-ink/15 text-muted") : "bg-transparent"}`}>
                          {s.done ? (s.ok ? "✓" : "•") : <span className="h-3 w-3 animate-spin rounded-full border-2 border-brand border-t-transparent" />}
                        </span>
                        <span className="min-w-0">
                          <span className={s.done ? "text-muted" : "font-medium"}>{s.label}</span>
                          {s.summary && <span className="block text-xs text-muted">{s.summary}</span>}
                        </span>
                      </motion.li>
                    ))}
                  </AnimatePresence>
                </ul>
              )}
              {narration && (
                <div className="mt-3 border-t border-line pt-3">
                  <Markdown text={narration} />
                  <span className="ml-0.5 inline-block h-4 w-1.5 translate-y-0.5 animate-pulse rounded-sm bg-brand" />
                </div>
              )}
            </div>
          </motion.div>
        )}
        {live && status === "failed" && runError && <p className="my-3 rounded-2xl bg-bad-soft px-4 py-3 text-sm text-bad">{runError}</p>}
        {error && <p className="my-3 rounded-2xl bg-bad-soft px-4 py-3 text-sm text-bad">{error}</p>}
        <div ref={bottom} />
      </div>
      <Composer busy={busy} onSend={sendText} onCancel={running ? cancel : undefined} autoFocus />
    </div>
  );
}

function Welcome({ onPick }: { onPick: (s: string) => void }) {
  const t = useT();
  return (
    <div className="py-6 text-center">
      <Orb size={64} className="mx-auto" />
      <h2 className="mt-4 font-display text-2xl font-semibold">{t.welcomeTitle}</h2>
      <p className="mx-auto mb-5 mt-2 max-w-xs text-sm text-muted">{t.welcomeBody}</p>
      <div className="flex flex-col gap-2">
        {t.examples.map((e) => (
          <button key={e} onClick={() => onPick(e)} className="rounded-2xl border border-line bg-card px-4 py-2.5 text-left text-sm transition hover:border-brand/40 hover:shadow">
            {e}
          </button>
        ))}
      </div>
    </div>
  );
}

export function Markdown({ text }: { text: string }) {
  return (
    <div className="prose-kora text-[14.5px]">
      {/* No raw HTML, no images (exfiltration), links only to http(s). */}
      <ReactMarkdown
        disallowedElements={["img", "script", "iframe"]}
        components={{
          a: ({ href, children }) =>
            href && /^https?:\/\//.test(href) ? (
              <a href={href} target="_blank" rel="noopener noreferrer nofollow" className="font-medium text-brand underline underline-offset-2">
                {children}
              </a>
            ) : (
              <span>{children}</span>
            ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}

function MessageRow({ m, latest, onOption }: { m: Message; latest: boolean; onOption: (s: string) => void }) {
  const t = useT();
  const jump = useJump();
  const b = m.blocks ?? {};
  if (m.role === "user") {
    if (b.action) {
      return (
        <motion.div className="my-3 flex justify-end" initial={{ opacity: 0, x: 12 }} animate={{ opacity: 1, x: 0 }}>
          <span className="inline-flex max-w-[90%] items-center gap-1.5 rounded-full border border-brand/40 bg-brand-soft px-3.5 py-1.5 text-sm font-semibold text-brand-deep">
            <span aria-hidden>⚡</span>
            {b.action.label ?? m.content}
          </span>
        </motion.div>
      );
    }
    return (
      <motion.div className="my-3 flex justify-end" initial={{ opacity: 0, x: 12 }} animate={{ opacity: 1, x: 0 }}>
        <div className="max-w-[88%] whitespace-pre-wrap rounded-3xl rounded-br-md bg-ink px-4 py-2.5 text-[14.5px] leading-relaxed text-paper">{m.content}</div>
      </motion.div>
    );
  }
  return (
    <motion.div className="my-3 flex gap-2.5" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }}>
      <Orb size={28} className="mt-1" mode={m.role === "notice" ? "idle" : "done"} />
      <div className={`min-w-0 flex-1 rounded-3xl rounded-tl-md border px-4 py-3 ${m.role === "notice" ? "border-est/30 bg-est-soft" : "border-line bg-card"}`}>
        <Markdown text={m.content} />
        {b.notices?.map((n) => (
          <p key={n} className="mt-2 rounded-xl bg-est-soft px-3 py-2 text-xs text-est">
            {n}
          </p>
        ))}
        {latest && b.clarification?.options && (
          <div className="mt-3 flex flex-wrap gap-1.5">
            {b.clarification.options.map((o) => (
              <button key={o} onClick={() => onOption(o)} className="rounded-full border border-brand/40 px-3 py-1 text-sm font-medium text-brand-deep transition hover:bg-brand hover:text-white">
                {o}
              </button>
            ))}
          </div>
        )}
        {((b.packages?.length ?? 0) > 0 || (b.destinations?.length ?? 0) > 0) && (
          <div className="mt-3 flex flex-wrap gap-1.5">
            {(b.packages?.length ?? 0) > 0 && (
              <button className="rounded-full bg-ink px-3 py-1.5 text-xs font-semibold text-paper transition hover:bg-brand" onClick={() => jump("packages")}>
                {t.canvas.views.packages} →
              </button>
            )}
            {(b.destinations?.length ?? 0) > 0 && (
              <button className="rounded-full bg-ink px-3 py-1.5 text-xs font-semibold text-paper transition hover:bg-brand" onClick={() => jump("destinations")}>
                {t.destinations} →
              </button>
            )}
          </div>
        )}
        <Feedback messageId={m.id} />
      </div>
    </motion.div>
  );
}

function Feedback({ messageId }: { messageId: string }) {
  const t = useT();
  const [done, setDone] = useState(false);
  const fb = useMutation({ mutationFn: (rating: 1 | -1) => api.feedback({ message_id: messageId, rating }), onSuccess: () => setDone(true) });
  if (done) return <p className="mt-2 text-[11px] text-muted">{t.thanks}</p>;
  return (
    <div className="mt-2 flex gap-0.5 opacity-60 transition hover:opacity-100">
      <button className="rounded-full px-1.5 text-xs hover:bg-ink/5" aria-label={t.helpful} onClick={() => fb.mutate(1)}>
        👍
      </button>
      <button className="rounded-full px-1.5 text-xs hover:bg-ink/5" aria-label={t.notHelpful} onClick={() => fb.mutate(-1)}>
        👎
      </button>
    </div>
  );
}
