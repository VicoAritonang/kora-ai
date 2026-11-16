"use client";
import { useQuery } from "@tanstack/react-query";
import { AnimatePresence, motion } from "motion/react";
import Link from "next/link";
import { useState } from "react";
import { api } from "@/lib/api/client";
import { cityLabel, sampleFlights, sampleStays } from "@/lib/tickets";
import { money, rupiah } from "@/lib/format";
import { useReplayWorkspace } from "@/lib/workspace/replay";
import { useT } from "@/i18n";
import { Badge, Kicker, Reveal, Tabs } from "@/components/ui";
import { Scene } from "@/components/scene/Scene";
import { useLaunchKora } from "@/features/orb/useLaunch";
import { useTimedNudge } from "@/features/orb/useNudge";
import { FlightTicket, StayTicket } from "@/features/tickets/Tickets";
import { FlightCardTicket, StayCardTicket } from "@/features/workspace/Stage";

type Tab = "flights" | "stays";

export default function TicketsPage() {
  const t = useT();
  const launch = useLaunchKora();
  const [tab, setTab] = useState<Tab>("flights");
  const [city, setCity] = useState<"all" | "OSA" | "UKY" | "TYO">("all");
  useTimedNudge(
    {
      id: "tickets:page",
      text: t.orb.nudges.tickets,
      chips: [
        { label: t.orb.chips.checkTickets, onPick: () => launch(t.prompts.march) },
        { label: t.orb.chips.family, onPick: () => launch(t.prompts.family) },
      ],
    },
    5000,
  );
  const stays = sampleStays.filter((s) => city === "all" || s.city === city);

  return (
    <>
      <section className="relative h-[56vh] min-h-[420px] overflow-hidden bg-night text-white">
        <Scene variant="tokyo" time="night" />
        <div className="absolute inset-0 bg-gradient-to-b from-night/30 via-night/20 to-paper" />
        <div className="relative mx-auto flex h-full max-w-7xl flex-col justify-center px-4 pt-16 md:px-8">
          <Reveal>
            <Kicker light>{t.tickets.kicker}</Kicker>
            <h1 className="font-display text-[clamp(2.6rem,6.5vw,5.5rem)] font-semibold leading-[.95]">{t.tickets.title}</h1>
            <p className="mt-5 max-w-2xl text-white/80">{t.tickets.body}</p>
          </Reveal>
        </div>
      </section>

      <section className="grain bg-paper pb-28">
        <div className="mx-auto max-w-6xl px-4 md:px-8">
          <FromLatestTrip />
          <div className="mx-auto mb-8 max-w-sm">
            <Tabs<Tab> value={tab} onChange={setTab} tabs={[{ id: "flights", label: `✈ ${t.tickets.flights}` }, { id: "stays", label: `🏨 ${t.tickets.stays}` }]} />
          </div>
          <AnimatePresence mode="wait">
            {tab === "flights" ? (
              <motion.div key="f" initial={{ opacity: 0, y: 16 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -16 }}>
                <p className="mb-5 text-center text-sm text-muted">{t.tickets.note}</p>
                <div className="grid gap-4 lg:grid-cols-2">
                  {sampleFlights.map((f, i) => (
                    <FlightTicket
                      key={f.code}
                      index={i}
                      code={f.code}
                      carrier={f.carrier}
                      from={f.from}
                      to={f.to}
                      depart={f.depart}
                      arrive={f.arrive}
                      nextDay={f.nextDay}
                      stops={f.via ? t.tickets.via(f.via) : t.tickets.direct}
                      duration={t.board.duration(f.minutes)}
                      baggage={t.canvas.baggage(f.baggageKg)}
                      price={rupiah(f.baseIdr)}
                      priceNote={t.tickets.baseFare}
                      badges={<Badge tone="est">{t.tickets.simulated}</Badge>}
                      action={{ label: t.tickets.ask, onClick: () => launch(t.tickets.askFlight(f.carrier)) }}
                    />
                  ))}
                </div>
              </motion.div>
            ) : (
              <motion.div key="s" initial={{ opacity: 0, y: 16 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -16 }}>
                <div className="mb-6 flex flex-wrap justify-center gap-2">
                  {(["all", "OSA", "UKY", "TYO"] as const).map((c) => (
                    <button key={c} onClick={() => setCity(c)} className={`rounded-full px-4 py-1.5 text-sm font-semibold transition ${city === c ? "bg-ink text-paper" : "bg-card text-ink shadow-sm hover:bg-paper-deep"}`}>
                      {c === "all" ? t.tickets.all : cityLabel[c]}
                    </button>
                  ))}
                </div>
                <motion.div layout className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                  {stays.map((s, i) => (
                    <motion.div key={s.name} layout>
                      <StayTicket
                        index={i}
                        name={s.name}
                        area={`${cityLabel[s.city]} · ${s.area}`}
                        stars={s.stars}
                        rating={s.rating}
                        station={t.tickets.station(s.stationM)}
                        price={money({ amount: s.nightlyJpy, currency: "JPY" })}
                        priceNote={t.tickets.perNight}
                        badges={<Badge tone="est">{t.tickets.simulated}</Badge>}
                        action={{ label: t.tickets.ask, onClick: () => launch(t.tickets.askStay(s.name, cityLabel[s.city]!, s.area)) }}
                      />
                    </motion.div>
                  ))}
                </motion.div>
              </motion.div>
            )}
          </AnimatePresence>
        </div>
      </section>
    </>
  );
}

/** Real cards (sandbox or live) from the traveller's most recent run. */
function FromLatestTrip() {
  const t = useT();
  const list = useQuery({ queryKey: ["conversations"], queryFn: api.conversations });
  const first = list.data?.items[0];
  const conv = useQuery({ queryKey: ["conversation", first?.id], queryFn: () => api.conversation(first!.id), enabled: !!first });
  const ws = useReplayWorkspace(conv.data?.last_run_id);
  const flights = ws?.flights.items ?? [];
  const stays = ws?.stays.items ?? [];
  if (!first || (flights.length === 0 && stays.length === 0)) return null;
  return (
    <Reveal className="-mt-20 mb-16">
      <div className="relative rounded-[32px] border border-line bg-card p-5 shadow-[0_40px_80px_-40px_rgba(40,25,10,.45)] md:p-8">
        <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
          <div>
            <Kicker>{t.tickets.fromYourTrip}</Kicker>
            <h2 className="font-display text-3xl font-semibold">{conv.data?.title ?? first.title ?? t.untitled}</h2>
            <p className="mt-1 text-sm text-muted">{t.tickets.fromYourTripBody}</p>
          </div>
          <Link href={`/c/${first.id}`} className="rounded-full bg-ink px-5 py-2.5 text-sm font-semibold text-paper transition hover:bg-brand">
            {t.tickets.openTrip} →
          </Link>
        </div>
        <div className="grid gap-4 lg:grid-cols-2">
          {flights.slice(0, 4).map((c, i) => (
            <FlightCardTicket key={c.id} c={c} index={i} />
          ))}
          {stays.slice(0, 2).map((c, i) => (
            <StayCardTicket key={c.id} c={c} index={i} />
          ))}
        </div>
      </div>
    </Reveal>
  );
}
