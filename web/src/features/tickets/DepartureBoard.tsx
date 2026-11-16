"use client";
// Split-flap departure board: every character flips in when the board
// scrolls into view, like the boards at Soekarno-Hatta.
import { motion } from "motion/react";
import { sampleFlights } from "@/lib/tickets";
import { rupiah } from "@/lib/format";
import { useT } from "@/i18n";
import { useLaunchKora } from "@/features/orb/useLaunch";

export function FlapText({ text, delay = 0, className = "" }: { text: string; delay?: number; className?: string }) {
  return (
    <span className={`inline-flex ${className}`} aria-label={text}>
      {[...text].map((ch, i) => (
        <motion.span
          key={i}
          aria-hidden
          className="relative mx-[0.5px] inline-block min-w-[0.62em] rounded-[3px] bg-white/[.06] text-center"
          initial={{ rotateX: -90, opacity: 0 }}
          whileInView={{ rotateX: 0, opacity: 1 }}
          viewport={{ once: true }}
          transition={{ delay: delay + i * 0.028, duration: 0.42, ease: [0.3, 1.4, 0.5, 1] }}
          style={{ transformOrigin: "50% 50%", transformStyle: "preserve-3d" }}
        >
          {ch === " " ? " " : ch}
          <span className="pointer-events-none absolute inset-x-0 top-1/2 h-px bg-black/50" />
        </motion.span>
      ))}
    </span>
  );
}

export function DepartureBoard({ limit = 7 }: { limit?: number }) {
  const t = useT();
  const launch = useLaunchKora();
  const rows = sampleFlights.slice(0, limit);
  return (
    <div className="overflow-hidden rounded-[28px] border border-white/10 bg-[#0a0d1c] p-2 shadow-[0_40px_80px_-30px_rgba(0,0,0,.7)]">
      <div className="flex items-center justify-between px-4 pb-3 pt-3">
        <p className="flex items-center gap-2 font-mono text-xs font-bold uppercase tracking-[.3em] text-gold">
          <span className="h-2 w-2 animate-pulse rounded-full bg-gold" />
          {t.board.title}
        </p>
        <span className="rounded-full bg-est/20 px-2.5 py-0.5 text-[10px] font-bold uppercase tracking-widest text-gold">{t.tickets.simulated}</span>
      </div>
      <div className="overflow-x-auto no-scrollbar">
        <table className="w-full min-w-[640px] font-mono text-[13px] text-[#f6e7c1]">
          <thead>
            <tr className="text-left text-[10px] uppercase tracking-[.22em] text-white/40">
              {t.landing.boardCols.map((c, i) => (
                <th key={c} className={`px-4 py-2 font-semibold ${i === 4 ? "text-right" : ""}`}>
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((f, r) => (
              <tr
                key={f.code}
                onClick={() => launch(t.tickets.askFlight(f.carrier))}
                className="cursor-pointer border-t border-white/[.06] transition hover:bg-white/[.04]"
                title={t.tickets.ask}
              >
                <td className="px-4 py-3">
                  <span className="mr-2 inline-grid h-6 w-8 place-items-center rounded bg-brand text-[11px] font-bold text-white">{f.code}</span>
                  <FlapText text={f.carrier.toUpperCase()} delay={r * 0.08} />
                </td>
                <td className="px-4 py-3">
                  <FlapText text={`${f.from}→${f.to}`} delay={0.2 + r * 0.08} />
                  <span className="ml-2 text-[10px] text-white/40">{f.via ? t.tickets.via(f.via) : t.tickets.direct}</span>
                </td>
                <td className="px-4 py-3">
                  <FlapText text={f.depart} delay={0.35 + r * 0.08} />
                </td>
                <td className="px-4 py-3">
                  <FlapText text={f.arrive} delay={0.45 + r * 0.08} />
                  {f.nextDay && <sup className="ml-1 text-[9px] text-gold">+1</sup>}
                </td>
                <td className="px-4 py-3 text-right text-gold">
                  <FlapText text={rupiah(f.baseIdr).replace(/\s/g, " ")} delay={0.55 + r * 0.08} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="px-4 pb-3 pt-3 text-[11px] text-white/40">{t.tickets.note}</p>
    </div>
  );
}
