"use client";
import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { AnimatePresence, motion } from "motion/react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, type ConversationSummary } from "@/lib/api/client";
import { date, money } from "@/lib/format";
import { sceneFor } from "@/lib/journeys";
import { useT } from "@/i18n";
import { Kicker, Reveal, Spinner } from "@/components/ui";
import { Scene } from "@/components/scene/Scene";
import { Orb } from "@/features/orb/Orb";
import { useOrb } from "@/features/orb/store";
import { useTimedNudge } from "@/features/orb/useNudge";

export default function TripsPage() {
  const t = useT();
  const router = useRouter();
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["conversations"], queryFn: api.conversations });
  const items = (list.data?.items ?? []).slice(0, 24);
  const details = useQueries({ queries: items.map((c) => ({ queryKey: ["conversation", c.id], queryFn: () => api.conversation(c.id), staleTime: 30_000 })) });
  const create = useMutation({
    mutationFn: api.createConversation,
    onSuccess: (c) => {
      qc.invalidateQueries({ queryKey: ["conversations"] });
      useOrb.getState().setOpen(true);
      router.push(`/c/${c.id}`);
    },
  });
  const del = useMutation({ mutationFn: (id: string) => api.deleteConversation(id), onSuccess: () => qc.invalidateQueries({ queryKey: ["conversations"] }) });
  useTimedNudge({ id: "trips:page", text: t.orb.nudges.trips }, 6000);

  return (
    <>
      <section className="relative h-[46vh] min-h-[360px] overflow-hidden bg-night text-white">
        <Scene variant="osaka" time="night" />
        <div className="absolute inset-0 bg-gradient-to-b from-night/30 to-paper" />
        <div className="relative mx-auto flex h-full max-w-7xl flex-col justify-center px-4 pt-16 md:px-8">
          <Reveal>
            <Kicker light>{t.trips.kicker}</Kicker>
            <h1 className="max-w-3xl font-display text-[clamp(2.4rem,6vw,4.8rem)] font-semibold leading-[.95]">{t.trips.title}</h1>
          </Reveal>
        </div>
      </section>
      <section className="grain min-h-[50vh] bg-paper pb-28">
        <div className="mx-auto max-w-7xl px-4 md:px-8">
          {list.isPending ? (
            <div className="py-16 text-center">
              <Spinner />
            </div>
          ) : (
            <div className="-mt-10 grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
              <motion.button
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                onClick={() => create.mutate()}
                disabled={create.isPending}
                className="group flex min-h-[260px] flex-col items-center justify-center gap-4 rounded-[28px] border-2 border-dashed border-ink/15 bg-card/60 p-6 text-center transition hover:border-brand hover:bg-card"
              >
                <motion.span whileHover={{ rotate: 8, scale: 1.08 }}>
                  <Orb size={64} mode={create.isPending ? "thinking" : "idle"} />
                </motion.span>
                <span className="font-display text-2xl font-semibold">{t.trips.new}</span>
                {items.length === 0 && <span className="max-w-xs text-sm text-muted">{t.trips.empty}</span>}
              </motion.button>
              <AnimatePresence>
                {items.map((c, i) => (
                  <TripCard key={c.id} c={c} i={i} detail={details[i]?.data} onDelete={() => window.confirm(t.trips.deleteConfirm) && del.mutate(c.id)} />
                ))}
              </AnimatePresence>
            </div>
          )}
        </div>
      </section>
    </>
  );
}

function TripCard({ c, i, detail, onDelete }: { c: ConversationSummary; i: number; detail?: Awaited<ReturnType<typeof api.conversation>>; onDelete: () => void }) {
  const t = useT();
  const s = detail?.trip_state;
  return (
    <motion.article layout initial={{ opacity: 0, y: 30 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, scale: 0.9 }} transition={{ delay: Math.min(i, 8) * 0.05 }} className="group relative overflow-hidden rounded-[28px] border border-line bg-card shadow-[0_20px_40px_-30px_rgba(40,25,10,.5)]">
      <Link href={`/c/${c.id}`} className="block">
        <div className="relative h-36 overflow-hidden bg-night">
          <div className="absolute inset-0 transition duration-700 group-hover:scale-105">
            <Scene variant={sceneFor(s?.destination?.code)} time="dusk" parallax={false} compact />
          </div>
          <p className="absolute bottom-3 left-5 font-display text-3xl font-semibold text-white drop-shadow">{s?.destination?.label ?? t.stage.newTrip}</p>
        </div>
        <div className="p-5">
          <h3 className="line-clamp-1 font-semibold">{c.title ?? t.untitled}</h3>
          <div className="mt-2 flex flex-wrap gap-1.5 text-xs">
            {s?.origin && <span className="rounded-full bg-paper-deep px-2.5 py-1">{s.origin.label}</span>}
            {s?.nights && <span className="rounded-full bg-paper-deep px-2.5 py-1">{s.nights} {t.canvas.nights}</span>}
            {s?.budget && <span className="rounded-full bg-paper-deep px-2.5 py-1">{money(s.budget)}</span>}
          </div>
          <div className="mt-4 flex items-center justify-between text-xs text-muted">
            <span>
              {t.trips.updated} {date(c.last_message_at ?? c.created_at)}
            </span>
            <span className="font-semibold text-brand transition group-hover:translate-x-1">{t.trips.open} →</span>
          </div>
        </div>
      </Link>
      <button onClick={onDelete} className="glass-dark absolute right-3 top-3 rounded-full px-2.5 py-1 text-[11px] font-semibold text-white opacity-0 transition hover:bg-bad group-hover:opacity-100 focus:opacity-100">
        {t.trips.delete}
      </button>
    </motion.article>
  );
}
