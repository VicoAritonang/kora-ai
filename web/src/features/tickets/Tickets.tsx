"use client";
// Boarding-pass and hotel key-card visuals, shared by the trip stage (real
// cards from a Kora run) and the tickets page (sandbox reference fares).
import { motion } from "motion/react";
import type { ReactNode } from "react";

type Action = { label: string; onClick: () => void; disabled?: boolean };

export type FlightTicketProps = {
  code?: string;
  carrier: string;
  from: string;
  to: string;
  depart?: string;
  arrive?: string;
  date?: string;
  nextDay?: boolean;
  stops: string;
  duration?: string;
  baggage: string;
  price: string;
  priceNote: string;
  badges?: ReactNode;
  stamp?: string | null;
  glow?: boolean;
  action?: Action;
  index?: number;
};

export function FlightTicket(p: FlightTicketProps) {
  return (
    <motion.article
      initial={{ opacity: 0, y: 24, rotate: -0.6 }}
      whileInView={{ opacity: 1, y: 0, rotate: 0 }}
      viewport={{ once: true, margin: "-5% 0px" }}
      transition={{ delay: (p.index ?? 0) * 0.06, duration: 0.6, ease: [0.22, 1, 0.36, 1] }}
      whileHover={{ y: -4 }}
      className={`group relative ${p.glow ? "drop-shadow-[0_0_24px_rgba(15,118,110,.35)]" : "drop-shadow-[0_14px_24px_rgba(40,25,10,.14)]"}`}
    >
      <div className="ticket flex overflow-hidden rounded-[22px] bg-card" style={{ ["--cut" as string]: "calc(100% - 150px)" }}>
        <div className="relative min-w-0 flex-1 p-4 sm:p-5">
          <div className="flex items-center gap-2">
            {p.code && <span className="grid h-6 min-w-8 place-items-center rounded-md bg-ink px-1.5 font-mono text-[11px] font-bold text-paper">{p.code}</span>}
            <p className="truncate text-sm font-semibold">{p.carrier}</p>
            {p.date && <span className="ml-auto hidden shrink-0 text-xs text-muted sm:inline">{p.date}</span>}
          </div>
          <div className="mt-3 grid grid-cols-[auto_1fr_auto] items-center gap-3">
            <div>
              <p className="font-display text-3xl font-semibold leading-none sm:text-4xl">{p.from}</p>
              {p.depart && <p className="mt-1 font-mono text-sm tabular-nums text-muted">{p.depart}</p>}
            </div>
            <div className="relative h-6">
              <span className="absolute inset-x-0 top-1/2 border-t-2 border-dashed border-line" />
              <motion.span
                className="absolute top-1/2 -translate-y-1/2 text-brand"
                initial={{ left: "0%" }}
                whileInView={{ left: "calc(100% - 18px)" }}
                viewport={{ once: true }}
                transition={{ delay: 0.3 + (p.index ?? 0) * 0.06, duration: 1.4, ease: [0.65, 0, 0.35, 1] }}
              >
                <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor" aria-hidden>
                  <path d="M21 16v-2l-8-5V3.5a1.5 1.5 0 0 0-3 0V9l-8 5v2l8-2.5V19l-2 1.5V22l3.5-1 3.5 1v-1.5L13 19v-5.5z" transform="rotate(90 12 12)" />
                </svg>
              </motion.span>
              <span className="absolute inset-x-0 top-full mt-0.5 text-center text-[10px] font-semibold uppercase tracking-wider text-muted">{p.stops}</span>
            </div>
            <div className="text-right">
              <p className="font-display text-3xl font-semibold leading-none sm:text-4xl">{p.to}</p>
              {p.arrive && (
                <p className="mt-1 font-mono text-sm tabular-nums text-muted">
                  {p.arrive}
                  {p.nextDay && <sup className="ml-0.5 text-brand">+1</sup>}
                </p>
              )}
            </div>
          </div>
          <div className="mt-4 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted">
            {p.duration && <span>⏱ {p.duration}</span>}
            <span>🧳 {p.baggage}</span>
            {p.stamp && (
              <motion.span
                initial={{ scale: 2, opacity: 0, rotate: -18 }}
                whileInView={{ scale: 1, opacity: 1, rotate: -4 }}
                viewport={{ once: true }}
                transition={{ delay: 0.6, type: "spring", damping: 10 }}
                className="ml-auto rounded-md border-2 border-live px-2 py-0.5 text-[10px] font-black uppercase tracking-widest text-live"
              >
                {p.stamp}
              </motion.span>
            )}
          </div>
        </div>
        <div className="flex w-[150px] shrink-0 flex-col justify-between border-l-2 border-dashed border-line bg-paper-deep/60 p-4">
          <div>
            <p className="text-[10px] font-bold uppercase tracking-[.18em] text-muted">{p.priceNote}</p>
            <p className="mt-1 font-display text-xl font-semibold leading-tight tabular-nums">{p.price}</p>
          </div>
          <div className="mt-2 flex flex-wrap gap-1">{p.badges}</div>
          {p.action && (
            <button onClick={p.action.onClick} disabled={p.action.disabled} className="mt-3 rounded-full bg-ink px-3 py-1.5 text-[11px] font-semibold text-paper transition hover:bg-brand disabled:opacity-40">
              {p.action.label}
            </button>
          )}
        </div>
      </div>
    </motion.article>
  );
}

export type StayTicketProps = {
  name: string;
  area?: string;
  stars?: number;
  rating?: number;
  station?: string;
  price: string;
  priceNote: string;
  total?: string;
  badges?: ReactNode;
  stamp?: string | null;
  glow?: boolean;
  action?: Action;
  index?: number;
};

export function StayTicket(p: StayTicketProps) {
  return (
    <motion.article
      initial={{ opacity: 0, y: 24 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, margin: "-5% 0px" }}
      transition={{ delay: (p.index ?? 0) * 0.06, duration: 0.6, ease: [0.22, 1, 0.36, 1] }}
      whileHover={{ y: -4 }}
      className={`relative overflow-hidden rounded-[22px] border bg-card p-5 ${p.glow ? "border-live shadow-[0_0_0_4px_rgba(15,118,110,.12)]" : "border-line shadow-[0_14px_24px_-16px_rgba(40,25,10,.3)]"}`}
    >
      <div className="absolute -right-10 -top-10 h-32 w-32 rounded-full bg-gradient-to-br from-gold/30 to-brand/10 blur-xl" aria-hidden />
      <div className="relative flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[10px] font-bold uppercase tracking-[.2em] text-muted">{p.area}</p>
          <h4 className="mt-1 font-display text-xl font-semibold leading-tight">{p.name}</h4>
          <p className="mt-1 text-sm text-gold">{p.stars ? "★".repeat(p.stars) : ""}</p>
        </div>
        {p.rating ? <span className="shrink-0 rounded-xl bg-night px-2.5 py-1.5 font-mono text-sm font-bold text-white">{p.rating.toFixed(1)}</span> : null}
      </div>
      <div className="relative mt-4 flex items-end justify-between gap-3 border-t border-dashed border-line pt-3">
        <div>
          <p className="font-display text-xl font-semibold tabular-nums">
            {p.price}
            <span className="ml-1 text-xs font-normal text-muted">{p.priceNote}</span>
          </p>
          {p.total && <p className="text-xs tabular-nums text-muted">{p.total}</p>}
          {p.station && <p className="mt-0.5 text-xs text-muted">🚉 {p.station}</p>}
        </div>
        <div className="flex flex-col items-end gap-1.5">
          <div className="flex flex-wrap justify-end gap-1">{p.badges}</div>
          {p.action && (
            <button onClick={p.action.onClick} disabled={p.action.disabled} className="rounded-full border border-ink/15 px-3 py-1 text-[11px] font-semibold transition hover:border-brand hover:bg-brand hover:text-white disabled:opacity-40">
              {p.action.label}
            </button>
          )}
        </div>
      </div>
      {p.stamp && <span className="absolute left-5 top-0 rounded-b-lg bg-live px-2 py-0.5 text-[10px] font-bold uppercase tracking-widest text-white">{p.stamp}</span>}
    </motion.article>
  );
}

export function TicketSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div className="space-y-3">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="skeleton h-[132px] rounded-[22px]" style={{ animationDelay: `${i * 0.15}s` }} />
      ))}
    </div>
  );
}
