"use client";
// A journey told as one day passing: the landscape stays pinned while the
// chapters scroll over it, and the sky moves from dawn to night with the
// reader's progress. The last chapter hands the journey to Kora.
import { motion, useScroll, useSpring, useTransform } from "motion/react";
import Link from "next/link";
import { Fragment, useRef, useState } from "react";
import { useJourneys } from "./useJourneys";
import { useT } from "@/i18n";
import { Kicker } from "@/components/ui";
import { Scene } from "@/components/scene/Scene";
import { Orb } from "@/features/orb/Orb";
import { useOrb } from "@/features/orb/store";
import { useLaunchKora } from "@/features/orb/useLaunch";
import { useTimedNudge } from "@/features/orb/useNudge";
import { JourneyCard } from "./JourneyCard";

export function JourneyStory({ slug }: { slug: string }) {
  const t = useT();
  const journeys = useJourneys();
  const j = journeys.find((x) => x.slug === slug);
  const launch = useLaunchKora();
  const launching = useOrb((s) => s.launching);
  const ref = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState(-1);
  const { scrollYProgress } = useScroll({ target: ref, offset: ["start start", "end end"] });
  const time = useSpring(scrollYProgress, { stiffness: 60, damping: 20 });
  const bar = useTransform(scrollYProgress, [0, 1], ["0%", "100%"]);

  useTimedNudge(
    j
      ? {
          id: `journey:${j.slug}`,
          text: t.orb.nudges.journey(j.city),
          chips: [
            { label: t.orb.chips.plan, onPick: () => launch(j.prompt) },
            { label: t.orb.chips.cheaper, onPick: () => launch(j.prompt + t.prompts.cheaper) },
          ],
        }
      : null,
    7000,
  );

  if (!j) {
    return (
      <div className="grid min-h-screen place-items-center bg-night text-white">
        <div className="text-center">
          <p className="font-display text-3xl">{t.journey.notFound}</p>
          <Link href="/journeys" className="mt-4 inline-block text-gold underline">
            {t.journey.back}
          </Link>
        </div>
      </div>
    );
  }
  const others = journeys.filter((x) => x.slug !== j.slug).slice(0, 3);

  return (
    <>
      <div ref={ref} className="relative bg-night text-white">
        <div className="sticky top-0 h-screen overflow-hidden">
          <Scene variant={j.scene} progress={time} petals parallax={false} />
          <div className="absolute inset-0 bg-gradient-to-r from-night/70 via-night/20 to-transparent" />
          <motion.div className="absolute left-0 top-16 h-0.5 bg-gold" style={{ width: bar }} />
        </div>

        <nav className="fixed right-4 top-1/2 z-30 hidden -translate-y-1/2 flex-col gap-3 md:flex" aria-label={t.journeyChapters}>
          {j.chapters.map((c, i) => (
            <a key={c.day} href={`#ch-${i}`} className="group flex items-center justify-end gap-3" aria-label={c.title}>
              <span className={`text-xs font-semibold transition ${active === i ? "text-white opacity-100" : "text-white/0 group-hover:text-white/70"}`}>{c.day}</span>
              <span className={`block rounded-full transition-all ${active === i ? "h-3 w-3 bg-gold" : "h-2 w-2 bg-white/40"}`} />
            </a>
          ))}
        </nav>

        <div className="relative -mt-[100vh]">
          <section className="flex min-h-screen items-end pb-24">
            <div className="mx-auto w-full max-w-7xl px-4 md:px-8">
              <Link href="/journeys" className="mb-8 flex w-fit text-sm font-semibold text-white/70 hover:text-white">
                ← {t.journey.back}
              </Link>
              <Kicker light>{j.kicker}</Kicker>
              <h1 className="max-w-4xl font-display text-[clamp(2.8rem,7.5vw,6.5rem)] font-semibold leading-[.92]">
                {j.title.split(" ").map((w, i) => (
                  <Fragment key={`${j.slug}-${i}`}>
                    {i > 0 && " "}
                    <motion.span className="inline-block" initial={{ opacity: 0, y: 40, rotate: 3 }} animate={{ opacity: 1, y: 0, rotate: 0 }} transition={{ delay: 0.1 + i * 0.07, duration: 0.8, ease: [0.22, 1, 0.36, 1] }}>
                      {w}
                    </motion.span>
                  </Fragment>
                ))}
              </h1>
              <motion.p className="mt-6 max-w-xl text-lg text-white/80" initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ delay: 0.7 }}>
                {j.tagline}
              </motion.p>
              <motion.dl className="mt-8 flex flex-wrap gap-3" initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.9 }}>
                {[
                  [t.journey.season, j.season],
                  [t.canvas.trip.nights, t.journey.nights(j.nights)],
                  [t.journey.bestFor, j.bestFor],
                ].map(([k, v]) => (
                  <div key={k} className="glass-dark rounded-2xl px-4 py-2.5">
                    <dt className="text-[10px] font-bold uppercase tracking-[.2em] text-white/55">{k}</dt>
                    <dd className="text-sm font-semibold">{v}</dd>
                  </div>
                ))}
              </motion.dl>
            </div>
          </section>

          {j.chapters.map((c, i) => (
            <motion.section
              key={c.day}
              id={`ch-${i}`}
              className="flex min-h-screen items-center py-24"
              onViewportEnter={() => setActive(i)}
              viewport={{ amount: 0.5 }}
            >
              <div className={`mx-auto flex w-full max-w-7xl px-4 md:px-8 ${i % 2 ? "md:justify-end" : ""}`}>
                <motion.article
                  className="glass-dark relative max-w-lg rounded-[32px] border border-white/15 p-7 md:p-9"
                  initial={{ opacity: 0, y: 60, x: i % 2 ? 30 : -30 }}
                  whileInView={{ opacity: 1, y: 0, x: 0 }}
                  viewport={{ once: true, amount: 0.4 }}
                  transition={{ duration: 0.9, ease: [0.22, 1, 0.36, 1] }}
                >
                  <span className="absolute -top-10 right-6 font-display text-[110px] font-semibold leading-none text-white/10">{String(i + 1).padStart(2, "0")}</span>
                  <p className="text-xs font-bold uppercase tracking-[.24em] text-gold">{c.day}</p>
                  <h2 className="mt-2 font-display text-4xl font-semibold leading-tight">{c.title}</h2>
                  <p className="mt-4 leading-relaxed text-white/80">{c.body}</p>
                  <p className="mt-6 text-[10px] font-bold uppercase tracking-[.22em] text-white/50">{t.journey.places}</p>
                  <ul className="mt-2 space-y-2">
                    {c.places.map((p, k) => (
                      <motion.li key={p} className="flex items-center gap-3 text-sm" initial={{ opacity: 0, x: -10 }} whileInView={{ opacity: 1, x: 0 }} viewport={{ once: true }} transition={{ delay: 0.3 + k * 0.1 }}>
                        <span className="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-brand/90 text-[10px] font-bold">{k + 1}</span>
                        {p}
                      </motion.li>
                    ))}
                  </ul>
                </motion.article>
              </div>
            </motion.section>
          ))}

          <section className="flex min-h-screen items-center py-24" onPointerEnter={() => setActive(-1)}>
            <div className="mx-auto flex w-full max-w-3xl flex-col items-center px-4 text-center">
              <motion.div initial={{ scale: 0.5, opacity: 0 }} whileInView={{ scale: 1, opacity: 1 }} viewport={{ once: true }} transition={{ type: "spring", damping: 12 }}>
                <Orb size={96} mode={launching ? "thinking" : "idle"} />
              </motion.div>
              <h2 className="mt-8 font-display text-[clamp(2.6rem,6vw,4.8rem)] font-semibold leading-none">{t.journey.plan}</h2>
              <p className="mt-5 max-w-lg text-lg text-white/75">{t.journey.planBody}</p>
              <div className="mt-8 flex flex-wrap justify-center gap-3">
                <button onClick={() => launch(j.prompt)} disabled={launching} className="inline-flex items-center gap-3 rounded-full bg-brand py-2 pl-2 pr-6 font-semibold shadow-[0_18px_40px_-12px_rgba(224,67,42,.9)] transition hover:bg-brand-deep disabled:opacity-60">
                  <Orb size={36} mode={launching ? "thinking" : "idle"} />
                  {launching ? t.orb.launching : t.orb.chips.plan}
                </button>
                <button onClick={() => launch(j.prompt + t.prompts.cheaper)} disabled={launching} className="rounded-full border border-white/30 px-6 py-3 font-semibold transition hover:bg-white/10">
                  {t.orb.chips.cheaper}
                </button>
              </div>
              <p className="mx-auto mt-6 max-w-md rounded-2xl bg-white/5 px-4 py-3 text-left text-xs text-white/60">“{j.prompt}”</p>
            </div>
          </section>
        </div>
      </div>

      <section className="grain bg-paper py-24">
        <div className="mx-auto max-w-7xl px-4 md:px-8">
          <h2 className="mb-10 font-display text-4xl font-semibold">{t.journey.next}</h2>
          <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
            {others.map((o, i) => (
              <JourneyCard key={o.slug} j={o} index={i} />
            ))}
          </div>
        </div>
      </section>
    </>
  );
}
