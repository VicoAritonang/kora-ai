"use client";
import { motion } from "motion/react";
import type { PackageItem, Schemas } from "@/lib/api/client";
import { dayDate, money } from "@/lib/format";
import { useT } from "@/i18n";
import { Badge } from "@/components/ui";

type Day = Schemas["PackageDay"];

export function Timeline({ days, items }: { days: Day[]; items: PackageItem[] }) {
  const t = useT();
  return (
    <ol className="relative space-y-4 before:absolute before:bottom-3 before:left-[19px] before:top-3 before:w-0.5 before:bg-gradient-to-b before:from-brand before:via-gold before:to-indigo">
      {days.map((d, i) => {
        const dayItems = items.filter((x) => x.day_index === d.day_index);
        return (
          <motion.li key={d.day_index} className="relative flex gap-4" initial={{ opacity: 0, x: -12 }} animate={{ opacity: 1, x: 0 }} transition={{ delay: i * 0.05 }}>
            <span className="relative z-10 grid h-10 w-10 shrink-0 place-items-center rounded-xl bg-night font-display font-semibold text-white">{d.day_index + 1}</span>
            <div className="min-w-0 flex-1 rounded-2xl border border-line bg-card p-4">
              <p className="text-[11px] font-bold uppercase tracking-[.16em] text-muted">
                {t.day(d.day_index + 1)}
                {d.date ? ` · ${dayDate(d.date)}` : ""}
                {d.area ? ` · ${d.area}` : ""}
              </p>
              <h3 className="mt-0.5 font-display text-lg font-semibold">{d.title}</h3>
              {d.summary && <p className="text-sm text-muted">{d.summary}</p>}
              {dayItems.length > 0 && (
                <ul className="mt-2 space-y-1 border-t border-dashed border-line pt-2 text-sm">
                  {dayItems.map((x) => (
                    <li key={x.id} className="flex items-center justify-between gap-2">
                      <span className="min-w-0 truncate">{x.title}</span>
                      <span className="flex shrink-0 items-center gap-1.5 tabular-nums text-muted">
                        {money(x.amount_idr)}
                        <Badge tone={x.price_kind === "live" ? "live" : "est"}>{x.price_kind === "live" ? "LIVE" : "EST"}</Badge>
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </motion.li>
        );
      })}
    </ol>
  );
}
