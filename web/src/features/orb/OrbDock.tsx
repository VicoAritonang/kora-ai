"use client";
// The floating orb, its proactive speech bubble, and the chat panel it opens.
// `panel` is the chat body: a launcher on story pages, the live ChatView in a
// conversation workspace. The element marked data-orb-anchor is where focus
// "comets" start when Kora moves the page.
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useRef, type ReactNode } from "react";
import { useLocale, useT } from "@/i18n";
import { Orb } from "./Orb";
import { useOrb, type OrbMode } from "./store";

export function OrbDock({ mode, caption, panel }: { mode: OrbMode; caption?: string | null; panel: ReactNode }) {
  const t = useT();
  const { open, setOpen, toggle, nudge, dismissNudge } = useOrb();
  const shown: OrbMode = mode === "idle" && nudge ? "nudge" : mode;
  const { locale } = useLocale();
  // A nudge is written in the language it was shown in; drop it on a switch
  // (without marking it seen) so the page can show it again, translated.
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    useOrb.setState({ nudge: null });
  }, [locale]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && useOrb.getState().open) setOpen(false);
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        toggle();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [setOpen, toggle]);

  return (
    <>
      <AnimatePresence>
        {open && (
          <motion.aside
            key="panel"
            aria-label={t.orb.panel}
            className="glass fixed inset-x-2 bottom-2 top-16 z-50 flex flex-col overflow-hidden rounded-[28px] border border-white/60 shadow-[0_30px_80px_-20px_rgba(13,18,38,.45)] md:inset-x-auto md:bottom-4 md:right-4 md:top-20 md:w-[410px]"
            initial={{ opacity: 0, y: 40, scale: 0.92, filter: "blur(8px)" }}
            animate={{ opacity: 1, y: 0, scale: 1, filter: "blur(0px)" }}
            exit={{ opacity: 0, y: 30, scale: 0.94, filter: "blur(6px)" }}
            transition={{ type: "spring", damping: 26, stiffness: 260 }}
            style={{ transformOrigin: "bottom right" }}
          >
            <header className="flex items-center gap-3 border-b border-line/70 px-4 py-3">
              <span data-orb-anchor>
                <Orb mode={shown} size={40} />
              </span>
              <div className="min-w-0 flex-1">
                <p className="font-display text-lg font-semibold leading-tight">Kora</p>
                <AnimatePresence mode="wait">
                  <motion.p
                    key={caption ?? "ready"}
                    className="truncate text-xs text-muted"
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: -4 }}
                  >
                    {caption ?? t.orb.ready}
                  </motion.p>
                </AnimatePresence>
              </div>
              <button onClick={() => setOpen(false)} className="grid h-9 w-9 place-items-center rounded-full text-muted transition hover:bg-ink/5 hover:text-ink" aria-label={t.close}>
                <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden>
                  <path d="M2 2l10 10M12 2L2 12" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
                </svg>
              </button>
            </header>
            <div className="flex min-h-0 flex-1 flex-col">{panel}</div>
          </motion.aside>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {!open && nudge && (
          <motion.div
            key={nudge.id}
            role="status"
            className="fixed bottom-24 right-4 z-50 w-[min(330px,calc(100vw-32px))] md:right-6"
            initial={{ opacity: 0, y: 16, scale: 0.9 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 10, scale: 0.95 }}
            transition={{ type: "spring", damping: 22, stiffness: 260 }}
            style={{ transformOrigin: "bottom right" }}
          >
            <div className="relative rounded-3xl rounded-br-md border border-line bg-card p-4 shadow-[0_24px_60px_-20px_rgba(13,18,38,.45)]">
              <button onClick={dismissNudge} className="absolute right-2.5 top-2.5 grid h-7 w-7 place-items-center rounded-full text-muted hover:bg-ink/5" aria-label={t.close}>
                <svg width="10" height="10" viewBox="0 0 14 14" aria-hidden>
                  <path d="M2 2l10 10M12 2L2 12" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
                </svg>
              </button>
              <p className="mb-1 text-[11px] font-bold uppercase tracking-[.18em] text-brand">Kora</p>
              <Typed text={nudge.text} />
              {nudge.chips && nudge.chips.length > 0 && (
                <div className="mt-3 flex flex-wrap gap-1.5">
                  {nudge.chips.map((c, i) => (
                    <motion.button
                      key={c.label}
                      initial={{ opacity: 0, y: 6 }}
                      animate={{ opacity: 1, y: 0 }}
                      transition={{ delay: 0.5 + i * 0.08 }}
                      onClick={() => {
                        dismissNudge();
                        c.onPick();
                      }}
                      className="rounded-full border border-brand/30 bg-brand-soft px-3 py-1.5 text-left text-xs font-semibold text-brand-deep transition hover:border-brand hover:bg-brand hover:text-white"
                    >
                      {c.label}
                    </motion.button>
                  ))}
                </div>
              )}
              <button onClick={() => setOpen(true)} className="mt-3 text-xs font-semibold text-muted underline-offset-4 hover:text-ink hover:underline">
                {t.orb.reply} →
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {!open && (
          <motion.div
            key="fab"
            className="fixed bottom-4 right-4 z-50 flex items-center gap-2 md:bottom-6 md:right-6"
            initial={{ opacity: 0, scale: 0.6 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.6 }}
          >
            <AnimatePresence>
              {caption && (mode === "thinking" || mode === "acting" || mode === "done") && (
                <motion.span
                  key={caption}
                  className="glass hidden max-w-[260px] truncate rounded-full border border-white/60 px-3.5 py-2 text-xs font-semibold text-ink shadow-lg sm:block"
                  initial={{ opacity: 0, x: 12 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={{ opacity: 0, x: 12 }}
                >
                  {caption}
                </motion.span>
              )}
            </AnimatePresence>
            <motion.button
              data-orb-anchor
              onClick={() => setOpen(true)}
              aria-label={t.orb.open}
              className="rounded-full"
              whileHover={{ scale: 1.08 }}
              whileTap={{ scale: 0.94 }}
              animate={shown === "nudge" ? { y: [0, -8, 0] } : { y: 0 }}
              transition={shown === "nudge" ? { duration: 1.4, repeat: Infinity, ease: "easeInOut" } : undefined}
            >
              <Orb mode={shown} size={64} />
            </motion.button>
          </motion.div>
        )}
      </AnimatePresence>
    </>
  );
}

/** Reveals the nudge text word by word, like Kora is speaking. */
function Typed({ text }: { text: string }) {
  const words = text.split(" ");
  return (
    <p className="pr-5 text-[15px] leading-relaxed text-ink">
      {words.map((w, i) => (
        <motion.span key={i} initial={{ opacity: 0, filter: "blur(4px)" }} animate={{ opacity: 1, filter: "blur(0px)" }} transition={{ delay: i * 0.035, duration: 0.3 }}>
          {w}{" "}
        </motion.span>
      ))}
    </p>
  );
}
