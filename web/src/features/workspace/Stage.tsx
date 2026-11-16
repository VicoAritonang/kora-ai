"use client";
// The trip stage: a conversation's live plan told as chapters. Chapters appear
// as Kora reaches them (state.patch), Kora's focus actions (ui.action) scroll
// the page to a chapter, spotlight it and send a "comet" from the orb, and
// highlight actions make a package glow. Buttons send structured actions back.
import { AnimatePresence, animate, motion, useMotionValue } from "motion/react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { DestinationEstimate, FlightCard, ItineraryDay, StayCard, Suggestion, Workspace, WorkspacePackage } from "@/lib/api/client";
import { date, dayDate, money, rupiah } from "@/lib/format";
import { sceneFor } from "@/lib/journeys";
import { useWorkspace, type View } from "@/lib/workspace/store";
import { useT } from "@/i18n";
import { Badge, Button, Switch } from "@/components/ui";
import { Scene, type SceneTime } from "@/components/scene/Scene";
import { Orb } from "@/features/orb/Orb";
import { PackageSheet } from "@/features/packages/PackageCards";
import { FlightTicket, StayTicket, TicketSkeleton } from "@/features/tickets/Tickets";
import { useConversation } from "./ConversationContext";
import { JourneyLine } from "./JourneyLine";

const feasTone = { fits: "ok", tight: "est", over_budget: "bad", unbounded: "neutral" } as const;
const ORDER: View[] = ["brief", "destinations", "flights", "stays", "itinerary", "packages"];

type Spot = { view: View; reason: string | null; key: number };
type Comet = { key: number; from: { x: number; y: number }; to: { x: number; y: number } };

function visibleChapters(w: Workspace, view: View): View[] {
  const hist = new Set(w.phase?.history ?? []);
  const seen = (p: string) => hist.has(p) || w.phase?.current === p;
  const on: Record<View, boolean> = {
    brief: true,
    destinations: w.destinations.length > 0,
    flights: w.flights.items.length > 0 || w.flights.status !== "idle" || seen("searching_flights"),
    stays: w.stays.items.length > 0 || w.stays.status !== "idle" || seen("searching_stays"),
    itinerary: w.itinerary.length > 0 || seen("planning"),
    packages: w.packages.length > 0 || seen("building_packages"),
  };
  return ORDER.filter((v) => on[v] || v === view);
}

export function Stage() {
  const t = useT();
  const { ws, view, pulse, nav, follow, live, status } = useWorkspace();
  const [spot, setSpot] = useState<Spot | null>(null);
  const [comet, setComet] = useState<Comet | null>(null);
  const [visible, setVisible] = useState<View>("brief");
  const chapters = useMemo(() => (ws ? visibleChapters(ws, view) : []), [ws, view]);

  // User navigation: scroll to the chapter.
  useEffect(() => {
    if (nav === 0) return;
    const h = setTimeout(() => document.getElementById(`ch-${view}`)?.scrollIntoView({ behavior: "smooth", block: "start" }), 40);
    return () => clearTimeout(h);
  }, [nav, view]);

  // Kora's focus: scroll, spotlight, and fly a comet from the orb.
  useEffect(() => {
    if (pulse === 0 || !live || !follow) return;
    const v = useWorkspace.getState().view;
    const reason = useWorkspace.getState().focusReason;
    const h1 = setTimeout(() => document.getElementById(`ch-${v}`)?.scrollIntoView({ behavior: "smooth", block: "start" }), 60);
    const h2 = setTimeout(() => {
      setSpot({ view: v, reason, key: pulse });
      const a = document.querySelector("[data-orb-anchor]")?.getBoundingClientRect();
      const b = document.getElementById(`ch-head-${v}`)?.getBoundingClientRect();
      if (a && b) setComet({ key: pulse, from: { x: a.left + a.width / 2, y: a.top + a.height / 2 }, to: { x: b.left + 22, y: b.top + 22 } });
    }, 620);
    const h3 = setTimeout(() => setSpot((s) => (s?.key === pulse ? null : s)), 4800);
    return () => [h1, h2, h3].forEach(clearTimeout);
  }, [pulse, live, follow]);

  // Which chapter is on screen, for the chapter bar.
  useEffect(() => {
    if (typeof IntersectionObserver === "undefined") return;
    const io = new IntersectionObserver(
      (es) => {
        const top = es.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0];
        if (top) setVisible(top.target.id.slice(3) as View);
      },
      { rootMargin: "-30% 0px -55% 0px" },
    );
    chapters.forEach((c) => {
      const el = document.getElementById(`ch-${c}`);
      if (el) io.observe(el);
    });
    return () => io.disconnect();
  }, [chapters]);

  if (!ws) return <EmptyStage />;
  const running = live && status === "running";

  return (
    <div className="pb-32">
      <TripHero w={ws} running={running} />
      <ChapterBar chapters={chapters} active={visible} w={ws} />
      <div className="mx-auto max-w-5xl space-y-6 px-4 pt-8 md:px-8">
        <AnimatePresence initial={false}>
          {chapters.map((id) => (
            <Chapter key={id} id={id} n={chapters.indexOf(id) + 1} spot={spot?.view === id ? spot : null}>
              {id === "brief" && <BriefPanel w={ws} />}
              {id === "destinations" && <DestinationCards destinations={ws.destinations} />}
              {id === "flights" && <FlightsPanel w={ws} />}
              {id === "stays" && <StaysPanel w={ws} />}
              {id === "itinerary" && <ItineraryPanel days={ws.itinerary} />}
              {id === "packages" && <PackagesPanel w={ws} />}
            </Chapter>
          ))}
        </AnimatePresence>
        {ws.suggestions.length > 0 && <NextSteps items={ws.suggestions} />}
        <p className="px-2 pt-4 text-center text-xs text-muted">{t.disclaimer}</p>
      </div>
      <AnimatePresence>
        {comet && (
          <motion.div
            key={comet.key}
            className="pointer-events-none fixed left-0 top-0 z-[55]"
            initial={{ x: comet.from.x, y: comet.from.y, opacity: 0, scale: 0.6 }}
            animate={{
              x: [comet.from.x, (comet.from.x + comet.to.x) / 2, comet.to.x],
              y: [comet.from.y, Math.min(comet.from.y, comet.to.y) - 140, comet.to.y],
              opacity: [0, 1, 1, 0],
              scale: [0.6, 1.2, 0.8],
            }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.95, ease: [0.65, 0, 0.35, 1] }}
            onAnimationComplete={() => setComet(null)}
          >
            <span className="block h-5 w-5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-gradient-to-br from-gold via-brand to-sakura shadow-[0_0_24px_10px_rgba(255,170,80,.55)]" />
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

// ------------------------------------------------------------------ hero --
const phaseTime = (phase: string, running: boolean): SceneTime => {
  if (!running) return "night";
  if (phase === "understanding" || phase === "retrieving_knowledge" || phase === "idle") return "dawn";
  if (phase.startsWith("searching")) return "day";
  return "dusk";
};
const timeVal: Record<SceneTime, number> = { dawn: 0, day: 1 / 3, dusk: 2 / 3, night: 1 };

function TripHero({ w, running }: { w: Workspace; running: boolean }) {
  const t = useT();
  const { follow, setFollow } = useWorkspace();
  const s = w.trip;
  const variant = sceneFor(s.destination?.code);
  // Kora's progress drives the sky: dawn while it listens, day while it
  // searches, dusk while it plans, night when the plan is ready.
  const sky = useMotionValue(timeVal[phaseTime(w.phase?.current ?? "idle", running)]);
  useEffect(() => {
    const c = animate(sky, timeVal[phaseTime(w.phase?.current ?? "idle", running)], { duration: 2.4, ease: "easeInOut" });
    return () => c.stop();
  }, [w.phase?.current, running, sky]);
  const planner = w.effort?.planner ?? w.effort?.narrator;
  return (
    <section className="relative h-[62vh] min-h-[440px] overflow-hidden bg-night text-white">
      <AnimatePresence initial={false}>
        <motion.div key={variant} className="absolute inset-0" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: 1.4 }}>
          <Scene variant={variant} progress={sky} petals={!running} />
        </motion.div>
      </AnimatePresence>
      <div className="absolute inset-0 bg-gradient-to-b from-night/30 via-transparent to-night/80" />
      <div className="relative mx-auto flex h-full max-w-5xl flex-col justify-end px-4 pb-6 pt-20 md:px-8">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs font-bold uppercase tracking-[.22em] text-white/70">{t.stage.plannerKicker}</span>
          {planner && (
            <span className="glass-dark inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px] text-white/85" title={planner.reason}>
              <span className={`h-1.5 w-1.5 rounded-full ${running ? "animate-pulse bg-gold" : "bg-white/50"}`} />
              {t.canvas.thinking}: {t.canvas.effort[planner.effort] ?? planner.effort}
              {planner.mode === "escalated" && <span className="text-gold">· {t.canvas.escalated}</span>}
            </span>
          )}
        </div>
        <AnimatePresence mode="wait">
          <motion.h1
            key={s.destination?.label ?? "new"}
            className="mt-2 font-display text-[clamp(2.6rem,7vw,5.5rem)] font-semibold leading-[.95]"
            initial={{ opacity: 0, y: 30, filter: "blur(8px)" }}
            animate={{ opacity: 1, y: 0, filter: "blur(0px)" }}
            exit={{ opacity: 0, y: -20 }}
            transition={{ duration: 0.8, ease: [0.22, 1, 0.36, 1] }}
          >
            {s.destination?.label ?? t.stage.newTrip}
          </motion.h1>
        </AnimatePresence>
        <div className="mt-3 flex flex-wrap items-center gap-2 text-sm">
          {s.origin && <HeroChip>{`${s.origin.label} → ${s.destination?.label ?? "…"}`}</HeroChip>}
          {s.budget && <HeroChip>{money(s.budget)}</HeroChip>}
          {s.nights && <HeroChip>{`${s.nights} ${t.canvas.nights}`}</HeroChip>}
          {(s.pax.adults || s.pax.children) > 0 && <HeroChip>{`${s.pax.adults || 1} ${t.adults}${s.pax.children ? ` · ${s.pax.children} ${t.children}` : ""}`}</HeroChip>}
          <span className="ml-auto">
            <span className="glass-dark inline-flex rounded-full px-3 py-1.5 text-white">
              <Switch checked={follow} onChange={setFollow} title={t.canvas.followHint} label={<span className="flex items-center gap-1.5"><Orb size={16} /> {t.canvas.follow}</span>} />
            </span>
          </span>
        </div>
        {(running || (w.phase?.history?.length ?? 0) > 0) && (
          <div className="glass-dark mt-5 rounded-3xl border border-white/10 px-4 pb-3 pt-4 md:px-6">
            <JourneyLine phase={w.phase?.current ?? "idle"} history={w.phase?.history ?? []} running={running} label={w.phase?.label} />
          </div>
        )}
      </div>
    </section>
  );
}

function HeroChip({ children }: { children: ReactNode }) {
  return (
    <motion.span key={String(children)} initial={{ opacity: 0, scale: 0.85, backgroundColor: "rgba(233,169,72,.6)" }} animate={{ opacity: 1, scale: 1, backgroundColor: "rgba(255,255,255,.12)" }} transition={{ duration: 1.2 }} className="rounded-full border border-white/15 px-3 py-1 font-semibold backdrop-blur">
      {children}
    </motion.span>
  );
}

// ----------------------------------------------------------- chapter bar --
function ChapterBar({ chapters, active, w }: { chapters: View[]; active: View; w: Workspace }) {
  const t = useT();
  const setView = useWorkspace((s) => s.setView);
  const count: Partial<Record<View, number>> = { flights: w.flights.items.length, stays: w.stays.items.length, itinerary: w.itinerary.length, packages: w.packages.length, destinations: w.destinations.length };
  return (
    <div className="glass sticky top-16 z-30 border-b border-line/70">
      <nav className="no-scrollbar mx-auto flex max-w-5xl gap-1 overflow-x-auto px-4 py-2.5 md:px-8" aria-label={t.canvas.title}>
        {chapters.map((id, i) => (
          <motion.button
            layout
            key={id}
            initial={{ opacity: 0, scale: 0.8 }}
            animate={{ opacity: 1, scale: 1 }}
            onClick={() => setView(id)}
            className={`relative flex shrink-0 items-center gap-2 rounded-full px-3.5 py-1.5 text-sm font-semibold transition ${active === id ? "text-white" : "text-ink-soft hover:bg-ink/5"}`}
          >
            {active === id && <motion.span layoutId="chapter-pill" className="absolute inset-0 rounded-full bg-ink" transition={{ type: "spring", damping: 30, stiffness: 380 }} />}
            <span className={`relative font-display text-xs ${active === id ? "text-gold" : "text-muted"}`}>{String(i + 1).padStart(2, "0")}</span>
            <span className="relative">{t.stage.titles[id]}</span>
            {!!count[id] && <span className={`relative rounded-full px-1.5 text-[11px] ${active === id ? "bg-white/20" : "bg-ink/[.07]"}`}>{count[id]}</span>}
          </motion.button>
        ))}
      </nav>
    </div>
  );
}

// --------------------------------------------------------------- chapter --
function Chapter({ id, n, spot, children }: { id: View; n: number; spot: Spot | null; children: ReactNode }) {
  const t = useT();
  return (
    <motion.section
      id={`ch-${id}`}
      layout="position"
      className="relative scroll-mt-36"
      initial={{ opacity: 0, y: 50, filter: "blur(6px)" }}
      animate={{ opacity: 1, y: 0, filter: "blur(0px)" }}
      exit={{ opacity: 0, y: 20 }}
      transition={{ duration: 0.8, ease: [0.22, 1, 0.36, 1] }}
    >
      <div className={`relative rounded-[32px] border bg-card p-5 transition-colors duration-700 md:p-8 ${spot ? "border-brand/50" : "border-line"} shadow-[0_24px_50px_-36px_rgba(40,25,10,.5)]`}>
        {spot && <span key={spot.key} className="pointer-events-none absolute inset-0 rounded-[32px]" style={{ animation: "spotlight 1.4s ease-out 2" }} />}
        <header id={`ch-head-${id}`} className="mb-6 flex flex-wrap items-start gap-4">
          <span className="font-display text-5xl font-semibold leading-none text-brand/20 md:text-6xl">{String(n).padStart(2, "0")}</span>
          <div className="min-w-0 flex-1">
            <p className="text-[11px] font-bold uppercase tracking-[.22em] text-brand">{t.stage.chapter(n)}</p>
            <h2 className="font-display text-3xl font-semibold leading-tight md:text-4xl">{t.stage.titles[id]}</h2>
            <p className="mt-1 text-sm text-muted">{t.stage.subtitles[id]}</p>
          </div>
          <AnimatePresence>
            {spot && (
              <motion.div
                key={spot.key}
                className="flex items-center gap-2 rounded-full bg-night py-1 pl-1 pr-3.5 text-xs font-semibold text-white shadow-lg"
                initial={{ opacity: 0, x: 30, scale: 0.8 }}
                animate={{ opacity: 1, x: 0, scale: 1 }}
                exit={{ opacity: 0, x: 20 }}
                transition={{ type: "spring", damping: 18 }}
              >
                <Orb size={22} mode="acting" />
                {spot.reason ?? t.stage.openChapter(t.stage.titles[id] ?? id)}
              </motion.div>
            )}
          </AnimatePresence>
        </header>
        {children}
      </div>
    </motion.section>
  );
}

function Waiting({ searching }: { searching: boolean }) {
  const t = useT();
  return searching ? (
    <TicketSkeleton rows={2} />
  ) : (
    <p className="rounded-2xl border border-dashed border-line px-4 py-6 text-center text-sm text-muted">{t.stage.waiting}</p>
  );
}

// ----------------------------------------------------------------- brief --
function BriefPanel({ w }: { w: Workspace }) {
  const t = useT();
  const s = w.trip;
  const assumed = (slot: string) => s.assumptions.some((a) => a.slot === slot);
  const win = s.date_window;
  const when = win?.from
    ? `${t.stage.departing} ${date(win.from)}`
    : win?.month
      ? `${t.stage.months[win.month - 1] ?? ""} ${win.year ?? ""}`.trim()
      : win?.season
        ? `${t.stage.seasons[win.season] ?? win.season} ${win.year ?? ""}`.trim()
        : null;
  const pax = s.pax.adults || s.pax.children ? `${s.pax.adults || 1} ${t.adults.toLowerCase()}${s.pax.children ? `, ${s.pax.children} ${t.children.toLowerCase()}` : ""}` : null;
  return (
    <div className="space-y-6">
      <p className="font-display text-2xl leading-[1.5] text-ink-soft md:text-[28px]">
        {t.stage.story.from} <Token v={s.origin?.label} a={assumed("origin")} /> {t.stage.story.to} <Token v={s.destination?.label} a={assumed("destination")} />,{" "}
        <Token v={s.nights ? `${s.nights} ${t.stage.story.nights}` : null} a={assumed("nights")} />
        {when && (
          <>
            {" "}
            <Token v={when} a={assumed("date_window")} />
          </>
        )}
        , {t.stage.story.for} <Token v={pax} a={assumed("pax")} />, {t.stage.story.with} <Token v={s.budget ? money(s.budget) : null} a={assumed("budget")} />.
      </p>
      <NightsStepper nights={s.nights ?? null} />
      {s.special_requests.length > 0 && (
        <div>
          <p className="mb-2 text-[11px] font-bold uppercase tracking-[.2em] text-muted">{t.canvas.trip.special}</p>
          <div className="flex flex-wrap gap-1.5">
            {s.special_requests.map((r) => (
              <Badge key={r.kind + r.text} tone="brand">
                {r.text}
              </Badge>
            ))}
          </div>
        </div>
      )}
      {s.assumptions.length > 0 && (
        <div>
          <p className="mb-2 text-[11px] font-bold uppercase tracking-[.2em] text-muted">{t.assumptions}</p>
          <ul className="grid gap-2 sm:grid-cols-2">
            {s.assumptions.map((a, i) => (
              <motion.li key={a.slot} initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: i * 0.06 }} className="rounded-2xl border border-dashed border-gold/60 bg-est-soft/50 px-4 py-3 text-sm">
                {a.reason}
              </motion.li>
            ))}
          </ul>
        </div>
      )}
      {w.notices.map((n) => (
        <p key={n} className="rounded-2xl bg-est-soft px-4 py-3 text-sm text-est">
          {n}
        </p>
      ))}
    </div>
  );
}

/** One value in the trip sentence; flashes when Kora changes it. */
function Token({ v, a }: { v: string | null | undefined; a: boolean }) {
  const t = useT();
  return (
    <motion.span
      key={v ?? "_"}
      initial={{ backgroundColor: "rgba(233,169,72,.45)" }}
      animate={{ backgroundColor: "rgba(233,169,72,0)" }}
      transition={{ duration: 1.6 }}
      title={a ? t.stage.assumed : undefined}
      className={`rounded-md px-1 font-semibold text-ink ${a ? "underline decoration-gold decoration-dashed decoration-2 underline-offset-[6px]" : v ? "underline decoration-brand/40 decoration-2 underline-offset-[6px]" : "text-muted"}`}
    >
      {v ?? t.stage.story.unknown}
    </motion.span>
  );
}

function NightsStepper({ nights }: { nights: number | null }) {
  const t = useT();
  const { sendAction, busy } = useConversation();
  const [n, setN] = useState(nights ?? 5);
  useEffect(() => setN(nights ?? 5), [nights]);
  if (nights === null) return null;
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-2xl bg-paper px-4 py-3">
      <span className="text-sm font-semibold text-muted">{t.canvas.trip.nights}</span>
      <div className="flex items-center gap-1">
        <button className="grid h-9 w-9 place-items-center rounded-full border border-line bg-card text-lg disabled:opacity-40" aria-label="-" onClick={() => setN((x) => Math.max(1, x - 1))} disabled={busy}>
          −
        </button>
        <AnimatePresence mode="popLayout">
          <motion.span key={n} className="w-32 whitespace-nowrap text-center font-display text-2xl font-semibold tabular-nums" initial={{ y: -12, opacity: 0 }} animate={{ y: 0, opacity: 1 }} exit={{ y: 12, opacity: 0 }}>
            {n} {t.canvas.nights}
          </motion.span>
        </AnimatePresence>
        <button className="grid h-9 w-9 place-items-center rounded-full border border-line bg-card text-lg disabled:opacity-40" aria-label="+" onClick={() => setN((x) => Math.min(30, x + 1))} disabled={busy}>
          +
        </button>
      </div>
      <AnimatePresence>
        {n !== nights && (
          <motion.div className="ml-auto" initial={{ opacity: 0, x: 10 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0 }}>
            <Button disabled={busy} onClick={() => sendAction({ type: "set_slot", payload: { nights: n }, label: t.stage.setNights(n) })}>
              {t.rebuild}
            </Button>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

// ---------------------------------------------------------- destinations --
const tiers = ["hemat", "seimbang", "nyaman"] as const;

function DestinationCards({ destinations }: { destinations: DestinationEstimate[] }) {
  const t = useT();
  const { sendAction, busy } = useConversation();
  return (
    <div className="grid gap-4 md:grid-cols-3">
      {destinations.map((d, i) => (
        <motion.article key={d.code} initial={{ opacity: 0, y: 30 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: i * 0.1 }} className="overflow-hidden rounded-3xl border border-line bg-paper">
          <div className="relative h-28 overflow-hidden bg-night">
            <Scene variant={sceneFor(d.code)} time="dusk" parallax={false} compact />
            <p className="absolute bottom-3 left-4 font-display text-2xl font-semibold text-white drop-shadow">{d.label}</p>
            <span className="glass-dark absolute right-3 top-3 rounded-full px-2 py-0.5 text-[10px] font-bold text-white">{d.nights} {t.canvas.nights}</span>
          </div>
          <ul className="divide-y divide-line px-4">
            {tiers.map((x) => {
              const v = d.total_idr[x];
              const fits = d.fits?.[x];
              return (
                <li key={x} className="flex items-center justify-between py-2.5 text-sm">
                  <span className="font-semibold">{t.tiers[x]}</span>
                  <span className={`flex items-center gap-2 tabular-nums ${fits === false ? "text-muted line-through" : ""}`}>
                    {v !== undefined ? rupiah(v) : "—"}
                    {fits !== undefined && (
                      <span className={`grid h-5 w-5 place-items-center rounded-full text-[10px] ${fits ? "bg-live text-white" : "bg-ink/10 text-muted"}`} title={fits ? t.stage.destinationsFits : t.stage.destinationsNot}>
                        {fits ? "✓" : "×"}
                      </span>
                    )}
                  </span>
                </li>
              );
            })}
          </ul>
          <div className="px-4 pb-4 pt-1">
            <button disabled={busy} onClick={() => sendAction({ type: "set_slot", payload: { destination: d.code }, label: t.stage.planDestination(d.label) })} className="w-full rounded-full bg-ink py-2 text-sm font-semibold text-paper transition hover:bg-brand disabled:opacity-40">
              {t.orb.chips.plan} →
            </button>
          </div>
        </motion.article>
      ))}
      <p className="text-xs text-muted md:col-span-3">
        <Badge tone="est">{t.estimate}</Badge> {t.stage.costIndex}
      </p>
    </div>
  );
}

// --------------------------------------------------------------- flights --
const hhmm = (s?: string) => (s ? s.slice(11, 16) : undefined);
const place = (s?: string) => (s ? s.slice(17) : "—");
/** "Singapore Airlines" → "SA", "Scoot" → "SC" (cards carry names, not IATA codes). */
const carrierCode = (name?: string) => {
  if (!name) return undefined;
  const w = name.split(/\s+/).filter(Boolean);
  return (w.length > 1 ? w.map((x) => x[0]).join("").slice(0, 2) : name.slice(0, 2)).toUpperCase();
};

export function FlightCardTicket({ c, index = 0 }: { c: FlightCard; index?: number }) {
  const t = useT();
  return (
    <FlightTicket
      index={index}
      code={carrierCode(c.carriers[0])}
      carrier={c.carriers.join(" · ")}
      from={place(c.depart)}
      to={place(c.arrive)}
      depart={hhmm(c.depart)}
      arrive={hhmm(c.arrive)}
      date={c.depart ? dayDate(c.depart.slice(0, 10)) : undefined}
      nextDay={!!c.depart && !!c.arrive && c.depart.slice(0, 10) !== c.arrive.slice(0, 10)}
      stops={t.canvas.stops(c.stops)}
      duration={c.hours ? t.stage.roundTripHours(c.hours) : undefined}
      baggage={t.canvas.baggage(c.baggage_kg ?? 0)}
      price={rupiah(c.price_idr)}
      priceNote={c.kind === "live" ? t.live : t.estimate}
      glow={c.used_by.length > 0}
      stamp={c.used_by.length ? `${t.stage.usedIn} ${c.used_by.map((x) => t.tiers[x]).join(" · ")}` : null}
      badges={
        <>
          <Badge tone={c.kind === "live" ? "live" : "est"}>{c.kind === "live" ? "LIVE" : "EST"}</Badge>
          {c.sandbox && <Badge tone="est">sandbox</Badge>}
        </>
      }
    />
  );
}

function FlightsPanel({ w }: { w: Workspace }) {
  const t = useT();
  const f = w.flights;
  return (
    <div className="space-y-4">
      {f.status === "estimate" && <p className="rounded-2xl bg-est-soft px-4 py-3 text-sm text-est">{t.canvas.estimateOnly}</p>}
      {f.items.length === 0 ? (
        f.status !== "estimate" && <Waiting searching={f.status === "searching" || f.status === "idle"} />
      ) : (
        <div className="grid gap-4 xl:grid-cols-2">
          {[...f.items]
            .sort((a, b) => b.used_by.length - a.used_by.length)
            .map((c, i) => (
              <FlightCardTicket key={c.id} c={c} index={i} />
            ))}
        </div>
      )}
    </div>
  );
}

// ----------------------------------------------------------------- stays --
export function StayCardTicket({ c, index = 0, onStay, busy = false }: { c: StayCard; index?: number; onStay?: (area: string) => void; busy?: boolean }) {
  const t = useT();
  return (
    <StayTicket
      index={index}
      name={c.name}
      area={c.area}
      stars={c.stars}
      rating={c.rating}
      station={c.station_m !== undefined ? t.canvas.station(c.station_m) : undefined}
      price={c.per_night_idr ? rupiah(c.per_night_idr) : rupiah(c.total_idr)}
      priceNote={c.per_night_idr ? t.canvas.perNight : ""}
      total={c.per_night_idr ? `${t.total} ${rupiah(c.total_idr)}` : undefined}
      glow={c.used_by.length > 0}
      stamp={c.used_by.length ? `${t.stage.usedIn} ${c.used_by.map((x) => t.tiers[x]).join(" · ")}` : null}
      badges={c.sandbox ? <Badge tone="est">sandbox</Badge> : undefined}
      action={onStay && c.area ? { label: t.canvas.stayHere, onClick: () => onStay(c.area!), disabled: busy } : undefined}
    />
  );
}

function StaysPanel({ w }: { w: Workspace }) {
  const t = useT();
  const { sendAction, busy } = useConversation();
  const s = w.stays;
  if (s.items.length === 0) return <Waiting searching={s.status === "searching" || s.status === "idle"} />;
  return (
    <div className="grid gap-4 md:grid-cols-2">
      {[...s.items]
        .sort((a, b) => b.used_by.length - a.used_by.length)
        .map((c, i) => (
          <StayCardTicket key={c.id} c={c} index={i} busy={busy} onStay={(area) => sendAction({ type: "prefer_area", payload: { area }, label: t.stage.stayArea(area) })} />
        ))}
    </div>
  );
}

// ------------------------------------------------------------- itinerary --
function ItineraryPanel({ days }: { days: ItineraryDay[] }) {
  const t = useT();
  if (days.length === 0) return <Waiting searching />;
  return (
    <ol className="relative space-y-4 before:absolute before:bottom-4 before:left-[27px] before:top-4 before:w-0.5 before:bg-gradient-to-b before:from-brand before:via-gold before:to-indigo md:before:left-[35px]">
      {days.map((d, i) => (
        <motion.li key={d.day_index} className="relative flex gap-4 md:gap-6" initial={{ opacity: 0, x: -20 }} whileInView={{ opacity: 1, x: 0 }} viewport={{ once: true, margin: "-8% 0px" }} transition={{ delay: (i % 4) * 0.06, duration: 0.6 }}>
          <span className="relative z-10 grid h-14 w-14 shrink-0 place-items-center rounded-2xl bg-night font-display text-xl font-semibold text-white shadow-lg md:h-[72px] md:w-[72px] md:text-2xl">{d.day_index + 1}</span>
          <div className="min-w-0 flex-1 rounded-3xl bg-paper p-4 md:p-5">
            <p className="text-[11px] font-bold uppercase tracking-[.18em] text-muted">
              {t.day(d.day_index + 1)}
              {d.date ? ` · ${dayDate(d.date)}` : ""}
              {d.area ? ` · ${d.area}` : ""}
            </p>
            <h3 className="mt-1 font-display text-xl font-semibold">{d.title}</h3>
            {d.summary && <p className="mt-1 text-sm leading-relaxed text-muted">{d.summary}</p>}
            {d.activities.length > 0 && (
              <div className="mt-3 flex flex-wrap gap-1.5">
                {d.activities.map((a) => (
                  <span key={a} className="rounded-full border border-line bg-card px-3 py-1 text-xs font-medium">
                    {a}
                  </span>
                ))}
              </div>
            )}
          </div>
        </motion.li>
      ))}
    </ol>
  );
}

// -------------------------------------------------------------- packages --
const tierStyle: Record<string, string> = {
  hemat: "from-teal-600 to-emerald-800",
  seimbang: "from-brand to-[#8f2a1a]",
  nyaman: "from-indigo to-night",
};

function PackagesPanel({ w }: { w: Workspace }) {
  const t = useT();
  const highlight = useWorkspace((s) => s.highlight);
  const [open, setOpen] = useState<string | null>(null);
  const { conversationId } = useConversation();
  if (w.packages.length === 0) return <Waiting searching={w.phase?.current === "building_packages"} />;
  return (
    <div>
      {w.recommendation?.reason && (
        <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} className="mb-6 flex items-start gap-3 rounded-3xl bg-night p-4 text-white">
          <Orb size={34} mode="done" />
          <p className="text-sm leading-relaxed">
            <strong className="text-gold">{t.canvas.recommended}:</strong> {w.recommendation.reason}
          </p>
        </motion.div>
      )}
      <div className="grid grid-cols-1 gap-5 md:grid-cols-3">
        {w.packages.map((p, i) => (
          <PackageTier key={p.id} p={p} i={i} glow={highlight?.id === p.id} onOpen={() => !p.draft && setOpen(p.id)} />
        ))}
      </div>
      {open && <PackageSheet id={open} conversationId={conversationId} onClose={() => setOpen(null)} onSwitch={setOpen} />}
    </div>
  );
}

function PackageTier({ p, i, glow, onOpen }: { p: WorkspacePackage; i: number; glow: boolean; onOpen: () => void }) {
  const t = useT();
  return (
    <motion.article
      layout
      initial={{ opacity: 0, y: 50, rotateX: 12 }}
      animate={{ opacity: p.draft ? 0.8 : 1, y: 0, rotateX: 0, scale: glow ? 1.02 : 1 }}
      transition={{ delay: i * 0.12, type: "spring", damping: 20 }}
      whileHover={{ y: -6 }}
      className={`relative flex flex-col overflow-hidden rounded-[28px] border bg-card ${glow ? "border-gold shadow-[0_0_0_4px_rgba(233,169,72,.25),0_30px_60px_-30px_rgba(224,67,42,.6)]" : p.recommended ? "border-brand/40" : "border-line"}`}
    >
      <div className={`relative bg-gradient-to-br ${tierStyle[p.tier] ?? "from-ink to-night"} px-5 pb-5 pt-4 text-white`}>
        <div className="flex items-center justify-between">
          <span className="text-xs font-bold uppercase tracking-[.2em] text-white/80">{t.tiers[p.tier]}</span>
          <div className="flex gap-1">
            {p.draft && <span className="rounded-full bg-white/20 px-2 py-0.5 text-[10px] font-bold">{t.canvas.draft}</span>}
            {(glow || p.recommended) && (
              <span className="inline-flex items-center gap-1 rounded-full bg-gold px-2 py-0.5 text-[10px] font-bold text-night">
                <Orb size={12} /> {glow ? t.stage.highlighted : t.canvas.recommended}
              </span>
            )}
          </div>
        </div>
        <p className="mt-1 text-sm text-white/75">{t.stage.tierLine[p.tier]}</p>
        <motion.p key={p.total.amount} initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} className="mt-3 font-display text-4xl font-semibold tabular-nums">
          {money(p.total)}
        </motion.p>
        <p className="text-xs text-white/70">
          {p.nights} {t.canvas.nights}
          {p.budget ? ` · ${t.canvas.trip.budget} ${money(p.budget)}` : ""}
        </p>
      </div>
      <div className="flex flex-1 flex-col p-5">
        <Badge tone={feasTone[p.feasibility]} className="self-start">
          {t.feasibility[p.feasibility]}
        </Badge>
        {/* Proportions come from the server (no money arithmetic in the client). */}
        <div className="mt-4 flex h-2.5 overflow-hidden rounded-full bg-ink/[.06]" aria-hidden>
          {[
            [p.bar.live_pct, "bg-live"],
            [p.bar.estimate_pct, "bg-est"],
            [p.bar.buffer_pct, "bg-gold"],
            [p.bar.over_pct, "bg-bad"],
          ].map(([pct, c], k) =>
            Number(pct) > 0 ? <motion.span key={k} className={String(c)} initial={{ width: 0 }} animate={{ width: `${pct}%` }} transition={{ delay: 0.3 + i * 0.12 + k * 0.15, duration: 0.9, ease: [0.22, 1, 0.36, 1] }} /> : null,
          )}
        </div>
        <dl className="mt-3 grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
          <dt className="flex items-center gap-1.5 text-muted">
            <i className="h-2 w-2 rounded-full bg-live" />
            {t.live}
          </dt>
          <dd className="text-right tabular-nums text-live">{money(p.live)}</dd>
          <dt className="flex items-center gap-1.5 text-muted">
            <i className="h-2 w-2 rounded-full bg-est" />
            {t.estimate}
          </dt>
          <dd className="text-right tabular-nums text-est">{money(p.estimate)}</dd>
          <dt className="flex items-center gap-1.5 text-muted">
            <i className="h-2 w-2 rounded-full bg-gold" />
            {t.buffer}
          </dt>
          <dd className="text-right tabular-nums">{money(p.buffer)}</dd>
          {p.shortfall.amount > 0 ? (
            <>
              <dt className="text-muted">{t.shortfall}</dt>
              <dd className="text-right font-semibold tabular-nums text-bad">{money(p.shortfall)}</dd>
            </>
          ) : p.budget ? (
            <>
              <dt className="text-muted">{t.remaining}</dt>
              <dd className="text-right font-semibold tabular-nums">{money(p.remaining)}</dd>
            </>
          ) : null}
        </dl>
        <div className="mt-4 space-y-1.5 border-t border-dashed border-line pt-3 text-xs text-ink-soft">
          {p.flight && <p className="truncate">✈ {p.flight}</p>}
          {p.stay && <p className="truncate">🏨 {p.stay}</p>}
        </div>
        {p.sandbox && <p className="mt-2 text-[11px] text-est">{t.sandboxBanner}</p>}
        <div className="mt-auto pt-4">
          <Button variant={p.recommended || glow ? "primary" : "outline"} className="w-full" onClick={onOpen} disabled={p.draft}>
            {t.details}
          </Button>
        </div>
      </div>
    </motion.article>
  );
}

// ------------------------------------------------------------ next steps --
function NextSteps({ items }: { items: Suggestion[] }) {
  const t = useT();
  const { sendAction, busy } = useConversation();
  return (
    <motion.div initial={{ opacity: 0, y: 30 }} animate={{ opacity: 1, y: 0 }} className="rounded-[32px] bg-night p-6 text-white md:p-8">
      <div className="mb-4 flex items-center gap-3">
        <Orb size={34} />
        <p className="font-display text-2xl font-semibold">{t.canvas.nextActions}</p>
      </div>
      <div className="flex flex-wrap gap-2">
        {items.map((s, i) => (
          <motion.button
            key={s.id}
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: i * 0.06 }}
            disabled={busy}
            onClick={() => sendAction({ ...s.action, label: s.label })}
            className="group rounded-full border border-white/20 px-4 py-2 text-sm font-semibold transition hover:border-gold hover:bg-gold hover:text-night disabled:opacity-40"
          >
            {s.label} <span className="inline-block transition group-hover:translate-x-1">→</span>
          </motion.button>
        ))}
      </div>
    </motion.div>
  );
}

// ----------------------------------------------------------------- empty --
function EmptyStage() {
  const t = useT();
  const { sendText, busy } = useConversation();
  return (
    <section className="relative min-h-screen overflow-hidden bg-night text-white">
      <Scene variant="fuji" time="dawn" petals />
      <div className="absolute inset-0 bg-gradient-to-b from-night/40 via-transparent to-night/80" />
      <div className="relative mx-auto flex min-h-screen max-w-3xl flex-col items-center justify-center px-4 pt-16 text-center">
        <Orb size={96} mode={busy ? "thinking" : "idle"} />
        <h1 className="mt-8 font-display text-[clamp(2.6rem,6vw,4.8rem)] font-semibold leading-none">{t.stage.tellKora}</h1>
        <p className="mt-4 max-w-lg text-white/75">{t.stage.tellKoraBody}</p>
        <div className="mt-8 flex flex-wrap justify-center gap-2">
          {t.examples.map((e) => (
            <button key={e} onClick={() => sendText(e)} disabled={busy} className="glass-dark rounded-full border border-white/20 px-4 py-2 text-sm transition hover:border-gold hover:text-gold disabled:opacity-50">
              {e}
            </button>
          ))}
        </div>
      </div>
    </section>
  );
}
