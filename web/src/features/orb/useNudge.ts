"use client";
// Proactive nudges: a page (or a section scrolling into view) asks the orb
// to say something. Each nudge shows at most once per browser session.
import { useEffect, useRef, type RefObject } from "react";
import { useOrb, type Nudge } from "./store";

/** Shows `nudge` after `delay` ms on the page. */
export function useTimedNudge(nudge: Nudge | null, delay = 5000) {
  const n = useRef(nudge);
  n.current = nudge;
  const id = nudge?.id;
  const text = nudge?.text; // a language switch re-shows the nudge, translated
  useEffect(() => {
    if (!id) return;
    const h = setTimeout(() => n.current && useOrb.getState().showNudge(n.current), delay);
    return () => clearTimeout(h);
  }, [id, delay, text]);
}

/** Shows `nudge` once `ref` has been at least 40% visible for 1.2 s. */
export function useSectionNudge(ref: RefObject<HTMLElement | null>, nudge: Nudge) {
  const n = useRef(nudge);
  n.current = nudge;
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof IntersectionObserver === "undefined") return;
    let h: ReturnType<typeof setTimeout> | undefined;
    const io = new IntersectionObserver(
      ([e]) => {
        if (e?.isIntersecting) h = setTimeout(() => useOrb.getState().showNudge(n.current), 1200);
        else if (h) clearTimeout(h);
      },
      { threshold: 0.4 },
    );
    io.observe(el);
    return () => {
      io.disconnect();
      if (h) clearTimeout(h);
    };
  }, [ref]);
}
