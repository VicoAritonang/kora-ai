"use client";
// The landing page is told in chapters (scrollytelling): a hero that travels
// between three cities, curated journeys, a pinned "how Kora works" sequence,
// the departure board, the honesty chapter, and the final ask. The orb
// speaks up as each chapter comes into view.
import { AnimatePresence, motion, useScroll, useTransform } from "motion/react";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { useT } from "@/i18n";
import { Kicker, Reveal } from "@/components/ui";
import { Scene, type SceneVariant } from "@/components/scene/Scene";
import { Orb } from "@/features/orb/Orb";
import { useOrb } from "@/features/orb/store";
import { useLaunchKora } from "@/features/orb/useLaunch";
import { useSectionNudge, useTimedNudge } from "@/features/orb/useNudge";
import { JourneyCard } from "@/features/journeys/JourneyCard";
import { useJourneys } from "@/features/journeys/useJourneys";
import { DepartureBoard } from "@/features/tickets/DepartureBoard";

export function Landing() {
  return (
    <>
      <Hero />
      <JourneysChapter />
      <HowItWorks />
      <BoardChapter />
      <HonestyChapter />
      <FinalChapter />
    </>
  );
}

// ------------------------------------------------------------------- hero --
const cities: { v: SceneVariant; name: string; jp: string }[] = [
  { v: "osaka", name: "Osaka", jp: "大阪" },
  { v: "kyoto", name: "Kyoto", jp: "京都" },
  { v: "tokyo", name: "Tokyo", jp: "東京" },
];

function Hero() {
  const t = useT();
  const launch = useLaunchKora();
  const ref = useRef<HTMLElement>(null);
  const [i, setI] = useState(0);
  useEffect(() => {
    const h = setInterval(() => setI((x) => (x + 1) % cities.length), 7000);
    return () => clearInterval(h);
  }, []);
  const { scrollYProgress } = useScroll({ target: ref, offset: ["start start", "end start"] });
  const fade = useTransform(scrollYProgress, [0, 0.7], [1, 0]);
  const lift = useTransform(scrollYProgress, [0, 1], [0, -120]);
  const city = cities[i]!;

  useTimedNudge(
    {
      id: "landing:hero",
      text: t.orb.nudges.hero,
      chips: [
        { label: t.orb.chips.budget15, onPick: () => launch(t.examples[0]!) },
        { label: t.orb.chips.budget25, onPick: () => launch(t.prompts.budget25) },
      ],
    },
    4500,
  );

  return (
    <section ref={ref} className="relative h-[100svh] min-h-[640px] overflow-hidden bg-night text-white">
      <AnimatePresence initial={false}>
        <motion.div key={city.v} className="absolute inset-0" initial={{ opacity: 0, scale: 1.06 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0 }} transition={{ duration: 1.6, ease: "easeInOut" }}>
          <Scene variant={city.v} time={i === 1 ? "dawn" : i === 2 ? "night" : "dusk"} petals />
        </motion.div>
      </AnimatePresence>
      <div className="absolute inset-0 bg-gradient-to-b from-night/50 via-transparent to-night/70" />

      <motion.div style={{ opacity: fade, y: lift }} className="relative mx-auto flex h-full max-w-7xl flex-col justify-center px-4 pt-16 md:px-8">
        <Kicker light>{t.landing.heroKicker}</Kicker>
        <h1 className="font-display text-[clamp(3rem,8.5vw,7.5rem)] font-semibold leading-[.92]">
          {t.landing.heroTitle.map((line, k) => (
            <span key={k} className="block overflow-hidden pb-[.08em]">
              <motion.span className={`block ${k === 2 ? "italic text-gold" : ""}`} initial={{ y: "110%" }} animate={{ y: 0 }} transition={{ delay: 0.2 + k * 0.12, duration: 1, ease: [0.22, 1, 0.36, 1] }}>
                {line}
              </motion.span>
            </span>
          ))}
        </h1>
        <motion.p className="mt-6 max-w-xl text-base leading-relaxed text-white/80 md:text-lg" initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.7, duration: 0.8 }}>
          {t.landing.heroBody}
        </motion.p>
        <motion.div className="mt-8 flex flex-wrap items-center gap-3" initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.9, duration: 0.8 }}>
          <button
            onClick={() => useOrb.getState().setOpen(true)}
            className="group inline-flex items-center gap-3 rounded-full bg-brand py-2 pl-2 pr-6 text-base font-semibold shadow-[0_18px_40px_-12px_rgba(224,67,42,.9)] transition hover:bg-brand-deep"
          >
            <Orb size={40} />
            {t.landing.start}
          </button>
          <a href="#journeys" className="rounded-full border border-white/30 px-6 py-3 text-base font-semibold backdrop-blur transition hover:bg-white/10">
            {t.landing.explore}
          </a>
        </motion.div>
      </motion.div>

      <div className="absolute inset-x-0 bottom-0">
        <div className="mx-auto flex max-w-7xl items-end justify-between gap-6 px-4 pb-8 md:px-8">
          <div className="hidden gap-10 md:flex">
            {t.landing.stats.map(([n, l], k) => (
              <motion.div key={l} initial={{ opacity: 0, y: 16 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 1.1 + k * 0.1 }}>
                <p className="font-display text-4xl font-semibold">{n}</p>
                <p className="text-xs uppercase tracking-[.18em] text-white/60">{l}</p>
              </motion.div>
            ))}
          </div>
          <div className="flex flex-col items-center gap-2 text-[11px] uppercase tracking-[.25em] text-white/60">
            <span className="flex h-9 w-5 justify-center rounded-full border border-white/40 pt-1.5">
              <span className="h-2 w-1 rounded-full bg-white" style={{ animation: "scroll-cue 1.8s ease-in-out infinite" }} />
            </span>
            {t.landing.scroll}
          </div>
          <div className="text-right">
            <AnimatePresence mode="wait">
              <motion.div key={city.v} initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -12 }} transition={{ duration: 0.6 }}>
                <p className="font-display text-5xl font-semibold text-white/90 md:text-6xl">{city.jp}</p>
                <p className="text-xs font-bold uppercase tracking-[.3em] text-white/60">{city.name}</p>
              </motion.div>
            </AnimatePresence>
            <div className="mt-3 flex justify-end gap-1.5">
              {cities.map((c, k) => (
                <button key={c.v} onClick={() => setI(k)} aria-label={c.name} className={`h-1.5 rounded-full transition-all ${k === i ? "w-8 bg-white" : "w-3 bg-white/40"}`} />
              ))}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

// --------------------------------------------------------------- journeys --
function JourneysChapter() {
  const t = useT();
  const journeys = useJourneys();
  const launch = useLaunchKora();
  const ref = useRef<HTMLElement>(null);
  useSectionNudge(ref, {
    id: "landing:journeys",
    text: t.orb.nudges.journeys,
    chips: [
      { label: t.prompts.osaka5, onPick: () => launch(journeys[0]!.prompt) },
      { label: t.prompts.kyotoAutumn, onPick: () => launch(journeys[1]!.prompt) },
    ],
  });
  return (
    <section id="journeys" ref={ref} className="grain scroll-mt-16 bg-paper py-24 md:py-32">
      <div className="mx-auto max-w-7xl px-4 md:px-8">
        <div className="mb-12 flex flex-col justify-between gap-6 md:flex-row md:items-end">
          <Reveal className="max-w-2xl">
            <Kicker>{t.landing.ch1}</Kicker>
            <h2 className="font-display text-[clamp(2.2rem,5vw,4rem)] font-semibold leading-[1]">{t.landing.ch1Title}</h2>
            <p className="mt-4 max-w-xl text-muted">{t.landing.ch1Body}</p>
          </Reveal>
          <Reveal delay={0.15}>
            <Link href="/journeys" className="inline-flex items-center gap-2 rounded-full border border-ink/15 px-5 py-2.5 text-sm font-semibold transition hover:bg-ink hover:text-paper">
              {t.landing.seeAll} →
            </Link>
          </Reveal>
        </div>
        <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
          {journeys.slice(0, 3).map((j, i) => (
            <JourneyCard key={j.slug} j={j} index={i} />
          ))}
        </div>
      </div>
    </section>
  );
}

// ----------------------------------------------------------- how it works --
function HowItWorks() {
  const t = useT();
  const [step, setStep] = useState(0);
  return (
    <section className="relative bg-night text-white">
      <div className="mx-auto max-w-7xl px-4 pt-24 md:px-8 md:pt-32">
        <Reveal>
          <Kicker light>{t.landing.ch2}</Kicker>
          <h2 className="max-w-3xl font-display text-[clamp(2.2rem,5vw,4rem)] font-semibold leading-[1]">{t.landing.ch2Title}</h2>
        </Reveal>
      </div>
      <div className="mx-auto grid max-w-7xl gap-8 px-4 md:grid-cols-2 md:px-8">
        <div className="sticky top-16 z-10 flex h-[52vh] items-center bg-night py-4 md:top-0 md:h-screen md:bg-transparent">
          <StepVisual step={step} />
        </div>
        <div className="relative z-0 pb-[20vh]">
          {t.landing.steps.map((s, i) => (
            <motion.div key={s.title} className="flex min-h-[70vh] items-center md:min-h-screen" onViewportEnter={() => setStep(i)} viewport={{ amount: 0.6 }}>
              <div className={`transition duration-500 ${step === i ? "opacity-100" : "opacity-30"}`}>
                <p className="font-display text-7xl font-semibold text-white/15">0{i + 1}</p>
                <h3 className="-mt-6 font-display text-4xl font-semibold md:text-5xl">{s.title}</h3>
                <p className="mt-4 max-w-md text-lg leading-relaxed text-white/70">{s.body}</p>
              </div>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
}

const mockTiers = [
  { id: "hemat", bar: [46, 34, 10], color: "from-teal-500/30" },
  { id: "seimbang", bar: [52, 30, 10], color: "from-brand/40" },
  { id: "nyaman", bar: [58, 28, 10], color: "from-indigo-500/40" },
];

function StepVisual({ step }: { step: number }) {
  const t = useT();
  return (
    <div className="relative w-full overflow-hidden rounded-[32px] border border-white/10 bg-gradient-to-br from-night-soft to-[#0a0d1c] p-5 shadow-[0_40px_80px_-30px_rgba(0,0,0,.8)] md:p-7">
      <div className="mb-5 flex items-center justify-between">
        <div className="flex gap-1.5">
          <span className="h-2.5 w-2.5 rounded-full bg-white/20" />
          <span className="h-2.5 w-2.5 rounded-full bg-white/20" />
          <span className="h-2.5 w-2.5 rounded-full bg-white/20" />
        </div>
        <span className="text-[10px] font-bold uppercase tracking-[.22em] text-white/35">{t.landing.illustration}</span>
      </div>
      <div className="relative h-[34vh] md:h-[380px]">
        <AnimatePresence mode="wait">
          <motion.div key={step} className="absolute inset-0" initial={{ opacity: 0, y: 24 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -24 }} transition={{ duration: 0.5, ease: [0.22, 1, 0.36, 1] }}>
            {step === 0 && <MockChat />}
            {step === 1 && <MockSearch />}
            {step === 2 && <MockPackages highlight={false} />}
            {step === 3 && <MockPackages highlight />}
          </motion.div>
        </AnimatePresence>
      </div>
    </div>
  );
}

function MockChat() {
  const t = useT();
  const chips = t.mock.chips;
  return (
    <div className="flex h-full flex-col justify-center gap-5">
      <motion.div className="ml-auto max-w-[85%] rounded-3xl rounded-br-md bg-white px-5 py-3.5 text-[15px] text-ink" initial={{ opacity: 0, scale: 0.9 }} animate={{ opacity: 1, scale: 1 }}>
        {t.mock.chat}
      </motion.div>
      <div className="flex items-start gap-3">
        <Orb mode="thinking" size={40} />
        <div className="flex flex-wrap gap-2 pt-1">
          {chips.map((c, i) => (
            <motion.span
              key={c}
              initial={{ opacity: 0, y: 10, scale: 0.8 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              transition={{ delay: 0.5 + i * 0.25, type: "spring", damping: 14 }}
              className={`rounded-full px-3 py-1.5 text-xs font-semibold ${i === 3 ? "border border-dashed border-gold/60 text-gold" : "bg-white/10 text-white"}`}
            >
              {c}
            </motion.span>
          ))}
        </div>
      </div>
    </div>
  );
}

function MockSearch() {
  const t = useT();
  const rows = t.mock.flights;
  return (
    <div className="flex h-full flex-col justify-center gap-3">
      <div className="mb-2 flex items-center gap-2 text-xs font-semibold text-white/60">
        <Orb mode="thinking" size={22} /> {t.mock.searching}
      </div>
      {rows.map(([code, name, info], i) => (
        <motion.div
          key={code}
          initial={{ opacity: 0, x: 60 }}
          animate={{ opacity: 1, x: 0 }}
          transition={{ delay: 0.3 + i * 0.35, type: "spring", damping: 18 }}
          className="flex items-center gap-4 rounded-2xl bg-white/[.07] px-4 py-3"
        >
          <span className="grid h-9 w-10 place-items-center rounded-lg bg-brand font-mono text-xs font-bold">{code}</span>
          <div className="flex-1">
            <p className="text-sm font-semibold">{name}</p>
            <p className="text-xs text-white/50">CGK → KIX · {info}</p>
          </div>
          <span className="rounded-full bg-teal-400/15 px-2 py-0.5 text-[10px] font-bold text-teal-300">LIVE</span>
        </motion.div>
      ))}
      <motion.div initial={{ opacity: 0, x: 60 }} animate={{ opacity: 1, x: 0 }} transition={{ delay: 1.4, type: "spring", damping: 18 }} className="flex items-center gap-4 rounded-2xl border border-white/10 px-4 py-3">
        <span className="grid h-9 w-10 place-items-center rounded-lg bg-gold/90 text-sm">🏨</span>
        <div className="flex-1">
          <p className="text-sm font-semibold">Dotonbori Riverside Inn</p>
          <p className="text-xs text-white/50">{t.mock.hotel}</p>
        </div>
      </motion.div>
    </div>
  );
}

function MockPackages({ highlight }: { highlight: boolean }) {
  const t = useT();
  return (
    <div className="relative grid h-full grid-cols-3 items-end gap-3">
      {mockTiers.map((p, i) => {
        const on = highlight && p.id === "seimbang";
        return (
          <motion.div
            key={p.id}
            initial={{ opacity: 0, y: 50 }}
            animate={{ opacity: highlight && !on ? 0.45 : 1, y: 0, scale: on ? 1.06 : 1 }}
            transition={{ delay: highlight ? 0.9 : i * 0.15, type: "spring", damping: 16 }}
            className={`relative flex h-[85%] flex-col rounded-3xl bg-gradient-to-b ${p.color} to-white/[.03] p-4 ${on ? "ring-2 ring-gold" : ""}`}
          >
            {on && (
              <motion.span initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 1.3 }} className="absolute -top-3 left-1/2 -translate-x-1/2 whitespace-nowrap rounded-full bg-gold px-2.5 py-0.5 text-[10px] font-bold text-night">
                {t.mock.pick}
              </motion.span>
            )}
            <p className="text-xs font-bold uppercase tracking-widest text-white/60">{t.tiers[p.id]}</p>
            <p className="mt-2 font-display text-xl font-semibold md:text-2xl">{t.mock.totals[i]}</p>
            <div className="mt-auto flex h-2 overflow-hidden rounded-full bg-white/10">
              <motion.span className="bg-teal-400" initial={{ width: 0 }} animate={{ width: `${p.bar[0]}%` }} transition={{ delay: 0.4 + i * 0.15, duration: 0.8 }} />
              <motion.span className="bg-amber-400" initial={{ width: 0 }} animate={{ width: `${p.bar[1]}%` }} transition={{ delay: 0.6 + i * 0.15, duration: 0.8 }} />
              <motion.span className="bg-amber-200" initial={{ width: 0 }} animate={{ width: `${p.bar[2]}%` }} transition={{ delay: 0.8 + i * 0.15, duration: 0.8 }} />
            </div>
          </motion.div>
        );
      })}
      {highlight && (
        <motion.div
          className="pointer-events-none absolute"
          initial={{ left: "88%", top: "-6%", opacity: 0 }}
          animate={{ left: ["88%", "70%", "47%"], top: ["-6%", "-16%", "18%"], opacity: [0, 1, 0] }}
          transition={{ duration: 1, ease: "easeInOut" }}
        >
          <Orb mode="acting" size={26} />
        </motion.div>
      )}
    </div>
  );
}

// ------------------------------------------------------------------ board --
function BoardChapter() {
  const t = useT();
  const launch = useLaunchKora();
  const ref = useRef<HTMLElement>(null);
  useSectionNudge(ref, {
    id: "landing:board",
    text: t.orb.nudges.board,
    chips: [{ label: t.orb.chips.checkTickets, onPick: () => launch(t.prompts.march) }],
  });
  return (
    <section ref={ref} className="relative overflow-hidden bg-night pb-28 pt-8 text-white md:pb-36">
      <div className="absolute inset-x-0 bottom-0 h-[60%] opacity-60">
        <Scene variant="fuji" time="night" parallax={false} />
      </div>
      <div className="relative mx-auto max-w-7xl px-4 md:px-8">
        <Reveal className="mb-10 max-w-2xl">
          <Kicker light>{t.landing.ch3}</Kicker>
          <h2 className="font-display text-[clamp(2.2rem,5vw,4rem)] font-semibold leading-[1]">{t.landing.ch3Title}</h2>
          <p className="mt-4 text-white/70">{t.landing.ch3Body}</p>
        </Reveal>
        <Reveal delay={0.1}>
          <DepartureBoard limit={6} />
        </Reveal>
        <Reveal delay={0.2} className="mt-8">
          <Link href="/tickets" className="inline-flex items-center gap-2 rounded-full bg-white px-6 py-3 text-sm font-semibold text-ink transition hover:bg-gold">
            {t.landing.seeTickets} →
          </Link>
        </Reveal>
      </div>
    </section>
  );
}

// --------------------------------------------------------------- honesty --
function HonestyChapter() {
  const t = useT();
  const ref = useRef<HTMLElement>(null);
  useSectionNudge(ref, { id: "landing:honest", text: t.orb.nudges.honest });
  const bars = [
    { k: "live", w: 52, c: "bg-live" },
    { k: "est", w: 30, c: "bg-est" },
    { k: "buf", w: 10, c: "bg-gold" },
  ];
  return (
    <section ref={ref} className="grain bg-paper py-24 md:py-32">
      <div className="mx-auto grid max-w-7xl gap-12 px-4 md:grid-cols-[1.1fr_1fr] md:px-8">
        <Reveal>
          <Kicker>{t.landing.ch4}</Kicker>
          <h2 className="font-display text-[clamp(2.2rem,5vw,4rem)] font-semibold leading-[1]">{t.landing.ch4Title}</h2>
          <p className="mt-5 max-w-lg text-lg leading-relaxed text-muted">{t.landing.ch4Body}</p>
        </Reveal>
        <Reveal delay={0.15} className="self-center">
          <div className="rounded-[28px] border border-line bg-card p-6 shadow-[0_30px_60px_-35px_rgba(40,25,10,.4)] md:p-8">
            <div className="flex items-baseline justify-between">
              <p className="text-xs font-bold uppercase tracking-[.2em] text-muted">{t.mock.packageLabel} · {t.landing.illustration}</p>
              <p className="font-display text-3xl font-semibold">100%</p>
            </div>
            <div className="mt-5 flex h-4 overflow-hidden rounded-full bg-ink/[.06]">
              {bars.map((b, i) => (
                <motion.span key={b.k} className={b.c} initial={{ width: 0 }} whileInView={{ width: `${b.w}%` }} viewport={{ once: true }} transition={{ delay: 0.3 + i * 0.25, duration: 1, ease: [0.22, 1, 0.36, 1] }} />
              ))}
            </div>
            <ul className="mt-6 space-y-4">
              {t.landing.legend.map(([k, label, body], i) => (
                <motion.li key={k} className="flex gap-4" initial={{ opacity: 0, x: 20 }} whileInView={{ opacity: 1, x: 0 }} viewport={{ once: true }} transition={{ delay: 0.6 + i * 0.15 }}>
                  <span className={`mt-1 h-3 w-3 shrink-0 rounded-full ${bars[i]!.c}`} />
                  <div>
                    <p className="font-semibold">{label}</p>
                    <p className="text-sm text-muted">{body}</p>
                  </div>
                </motion.li>
              ))}
            </ul>
          </div>
        </Reveal>
      </div>
    </section>
  );
}

// ------------------------------------------------------------------ final --
function FinalChapter() {
  const t = useT();
  const launch = useLaunchKora();
  const [text, setText] = useState("");
  const launching = useOrb((s) => s.launching);
  return (
    <section className="relative overflow-hidden bg-night py-32 text-white md:py-44">
      <Scene variant="kyoto" time="night" petals />
      <div className="absolute inset-0 bg-night/40" />
      <div className="relative mx-auto flex max-w-3xl flex-col items-center px-4 text-center">
        <Reveal>
          <Orb size={84} mode={launching ? "thinking" : "idle"} />
        </Reveal>
        <Reveal delay={0.1}>
          <h2 className="mt-8 font-display text-[clamp(3rem,8vw,6.5rem)] font-semibold italic leading-none">{t.landing.finalTitle}</h2>
        </Reveal>
        <Reveal delay={0.2}>
          <p className="mt-5 text-lg text-white/75">{t.landing.finalBody}</p>
        </Reveal>
        <Reveal delay={0.3} className="mt-8 w-full">
          <form
            onSubmit={(e) => {
              e.preventDefault();
              if (text.trim()) launch(text);
            }}
            className="glass-dark flex items-center gap-2 rounded-full border border-white/20 p-2 pl-6"
          >
            <label htmlFor="final-ask" className="sr-only">
              {t.placeholder}
            </label>
            <input id="final-ask" value={text} onChange={(e) => setText(e.target.value)} placeholder={t.placeholder} maxLength={4000} className="min-w-0 flex-1 bg-transparent py-2 text-base text-white outline-none placeholder:text-white/45" />
            <button type="submit" disabled={launching || !text.trim()} className="shrink-0 rounded-full bg-brand px-5 py-3 text-sm font-semibold transition hover:bg-brand-deep disabled:opacity-50">
              {t.send}
            </button>
          </form>
          <div className="mt-5 flex flex-wrap justify-center gap-2">
            {t.examples.map((e) => (
              <button key={e} onClick={() => launch(e)} className="rounded-full border border-white/20 px-4 py-2 text-sm text-white/85 transition hover:border-gold hover:text-gold">
                {e}
              </button>
            ))}
          </div>
        </Reveal>
      </div>
    </section>
  );
}
