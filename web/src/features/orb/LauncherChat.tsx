"use client";
// Orb panel on story pages: no conversation yet. The first message creates
// one and moves the traveller into its live workspace.
import { motion } from "motion/react";
import Link from "next/link";
import { useT } from "@/i18n";
import { Composer } from "@/features/chat/Composer";
import { Orb } from "./Orb";
import { useOrb } from "./store";
import { useLaunchKora } from "./useLaunch";

export function LauncherChat() {
  const t = useT();
  const launch = useLaunchKora();
  const { launching, launchError } = useOrb();
  return (
    <>
      <div className="flex-1 overflow-y-auto px-5 py-6">
        <div className="flex flex-col items-center text-center">
          <motion.div initial={{ scale: 0.6, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} transition={{ type: "spring", damping: 14 }}>
            <Orb mode={launching ? "thinking" : "idle"} size={88} />
          </motion.div>
          <h2 className="mt-5 font-display text-2xl font-semibold">{launching ? t.orb.launching : t.welcomeTitle}</h2>
          <p className="mt-2 max-w-xs text-sm leading-relaxed text-muted">{t.welcomeBody}</p>
        </div>
        {!launching && (
          <div className="mt-6 space-y-2">
            {t.examples.map((e, i) => (
              <motion.button
                key={e}
                initial={{ opacity: 0, x: -10 }}
                animate={{ opacity: 1, x: 0 }}
                transition={{ delay: 0.15 + i * 0.07 }}
                onClick={() => launch(e)}
                className="group flex w-full items-center justify-between gap-3 rounded-2xl border border-line bg-card px-4 py-3 text-left text-sm transition hover:border-brand/40 hover:shadow-md"
              >
                <span>{e}</span>
                <span className="text-brand transition group-hover:translate-x-1">→</span>
              </motion.button>
            ))}
          </div>
        )}
        {launchError && <p className="mt-4 rounded-2xl bg-bad-soft px-4 py-3 text-sm text-bad">{launchError}</p>}
        <Link href="/trips" onClick={() => useOrb.getState().setOpen(false)} className="mt-6 block text-center text-xs font-semibold text-muted underline-offset-4 hover:text-ink hover:underline">
          {t.orb.myTrips} →
        </Link>
      </div>
      <Composer busy={launching} onSend={launch} autoFocus />
    </>
  );
}
