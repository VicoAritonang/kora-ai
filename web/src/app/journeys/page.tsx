"use client";
import { AnimatePresence, motion } from "motion/react";
import { useState } from "react";
import { useT } from "@/i18n";
import { Kicker, Reveal } from "@/components/ui";
import { Scene } from "@/components/scene/Scene";
import { JourneyCard } from "@/features/journeys/JourneyCard";
import { useJourneys } from "@/features/journeys/useJourneys";
import { useLaunchKora } from "@/features/orb/useLaunch";
import { useTimedNudge } from "@/features/orb/useNudge";

const filters = ["all", "OSA", "UKY", "TYO"] as const;
const names: Record<string, string> = { OSA: "Osaka", UKY: "Kyoto", TYO: "Tokyo" };

export default function JourneysPage() {
  const t = useT();
  const launch = useLaunchKora();
  const journeys = useJourneys();
  const [f, setF] = useState<(typeof filters)[number]>("all");
  const list = journeys.filter((j) => f === "all" || j.code === f);
  useTimedNudge(
    {
      id: "journeys:list",
      text: t.orb.nudges.journeys,
      chips: [
        { label: t.orb.chips.budget15, onPick: () => launch(t.examples[0]!) },
        { label: t.orb.chips.family, onPick: () => launch(journeys[3]!.prompt) },
      ],
    },
    6000,
  );
  return (
    <>
      <section className="relative h-[62vh] min-h-[460px] overflow-hidden bg-night text-white">
        <Scene variant="fuji" time="dawn" petals />
        <div className="absolute inset-0 bg-gradient-to-b from-night/40 via-transparent to-paper" />
        <div className="relative mx-auto flex h-full max-w-7xl flex-col justify-center px-4 pt-16 md:px-8">
          <Reveal>
            <Kicker light>{t.journey.listKicker}</Kicker>
            <h1 className="max-w-3xl font-display text-[clamp(2.6rem,6.5vw,5.5rem)] font-semibold leading-[.95]">{t.journey.listTitle}</h1>
            <p className="mt-5 max-w-xl text-white/80">{t.journey.listBody}</p>
          </Reveal>
        </div>
      </section>
      <section className="grain bg-paper pb-28">
        <div className="mx-auto max-w-7xl px-4 md:px-8">
          <div className="sticky top-16 z-20 -mx-4 mb-8 flex gap-2 overflow-x-auto px-4 py-4 no-scrollbar md:mx-0 md:px-0">
            {filters.map((x) => (
              <button key={x} onClick={() => setF(x)} className={`relative shrink-0 rounded-full px-5 py-2 text-sm font-semibold transition ${f === x ? "text-white" : "bg-card text-ink shadow-sm hover:bg-paper-deep"}`}>
                {f === x && <motion.span layoutId="journey-filter" className="absolute inset-0 rounded-full bg-ink" transition={{ type: "spring", damping: 28, stiffness: 380 }} />}
                <span className="relative">{x === "all" ? t.journey.all : names[x]}</span>
              </button>
            ))}
          </div>
          <motion.div layout className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
            <AnimatePresence mode="popLayout">
              {list.map((j, i) => (
                <motion.div key={j.slug} layout initial={{ opacity: 0, scale: 0.94 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0, scale: 0.94 }} transition={{ duration: 0.35 }}>
                  <JourneyCard j={j} index={i} />
                </motion.div>
              ))}
            </AnimatePresence>
          </motion.div>
        </div>
      </section>
    </>
  );
}
