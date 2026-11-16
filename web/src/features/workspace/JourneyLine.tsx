"use client";
// Kora's progress as a train line: each pipeline phase is a station and the
// orb rides the track to the phase Kora is in.
import { motion } from "motion/react";
import { useT } from "@/i18n";
import { Orb } from "@/features/orb/Orb";

export function JourneyLine({ phase, history, running, label }: { phase: string; history: string[]; running: boolean; label?: string }) {
  const t = useT();
  const steps = t.canvas.steps;
  const seen = new Set(history);
  const cur = steps.findIndex(([id]) => id === phase);
  let reached = -1;
  steps.forEach(([id], i) => {
    if (seen.has(id) || i === cur) reached = i;
  });
  if (!running && reached < 0) reached = steps.length - 1;
  const at = running && cur >= 0 ? cur : reached;
  const pct = steps.length > 1 ? (Math.max(at, 0) / (steps.length - 1)) * 100 : 0;
  return (
    <div aria-label={t.stage.progress} role="group">
      <div className="mb-3 flex items-center justify-between text-xs">
        <span className="font-semibold text-white/85">{running ? (label ?? steps[at]?.[1]) : t.orb.done}</span>
        <span className="font-mono text-white/50">
          {Math.max(at, 0) + 1}/{steps.length}
        </span>
      </div>
      <div className="relative mx-2 h-6">
        <div className="absolute inset-x-0 top-1/2 h-1 -translate-y-1/2 rounded-full bg-white/15" />
        <motion.div className="absolute left-0 top-1/2 h-1 -translate-y-1/2 rounded-full bg-gradient-to-r from-gold via-brand to-sakura" animate={{ width: `${pct}%` }} transition={{ type: "spring", damping: 26, stiffness: 90 }} />
        {steps.map(([id, name], i) => {
          const done = (seen.has(id) && i !== cur) || (!running && i <= reached);
          const active = running && i === cur;
          return (
            <div key={id} className="absolute top-1/2 -translate-x-1/2 -translate-y-1/2" style={{ left: `${(i / (steps.length - 1)) * 100}%` }}>
              <span className={`block rounded-full border-2 transition-all duration-500 ${active ? "h-3.5 w-3.5 border-white bg-brand" : done ? "h-2.5 w-2.5 border-gold bg-gold" : "h-2.5 w-2.5 border-white/30 bg-night"}`} />
              <span className={`absolute left-1/2 top-5 hidden -translate-x-1/2 whitespace-nowrap text-[10px] font-semibold md:block ${active ? "text-white" : done ? "text-white/60" : "text-white/30"}`}>{name}</span>
            </div>
          );
        })}
        <motion.div className="absolute top-1/2 -translate-x-1/2 -translate-y-1/2" animate={{ left: `${pct}%` }} transition={{ type: "spring", damping: 26, stiffness: 90 }}>
          <Orb size={26} mode={running ? "thinking" : "done"} />
        </motion.div>
      </div>
      <div className="h-4 md:h-5" />
    </div>
  );
}
