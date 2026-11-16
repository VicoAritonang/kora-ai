"use client";
// Package detail sheet: per-item breakdown (live vs estimate), the daily plan,
// assumptions, and the actions (select, re-check prices, reminder, feedback).
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { motion } from "motion/react";
import { useState } from "react";
import { api, ApiError, type Package, type PackageItem } from "@/lib/api/client";
import { dateTime, isExpired, money } from "@/lib/format";
import { useT } from "@/i18n";
import { Badge, Button, Sheet, Spinner, Tabs } from "@/components/ui";
import { Timeline } from "@/features/itinerary/Timeline";

const feasTone = { fits: "ok", tight: "est", over_budget: "bad", unbounded: "neutral" } as const;
const tierStyle: Record<string, string> = { hemat: "from-teal-600 to-emerald-800", seimbang: "from-brand to-[#8f2a1a]", nyaman: "from-indigo to-night" };
const catIcon: Record<string, string> = { flight: "✈", stay: "🏨", activity: "⛩", meal: "🍜", local_transport: "🚃", visa: "🛂", insurance: "🛡", other: "•" };

function Row({ label, value, tone = "" }: { label: string; value: string; tone?: string }) {
  return (
    <div className="flex justify-between gap-3">
      <dt className="text-muted">{label}</dt>
      <dd className={`tabular-nums ${tone}`}>{value}</dd>
    </div>
  );
}

type Tab = "breakdown" | "itinerary" | "assumptions";

export function PackageSheet({ id, conversationId, onClose, onSwitch }: { id: string; conversationId?: string; onClose: () => void; onSwitch: (id: string) => void }) {
  const t = useT();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["package", id], queryFn: () => api.pkg(id) });
  const [tab, setTab] = useState<Tab>("breakdown");
  const [note, setNote] = useState<string | null>(null);
  const select = useMutation({
    mutationFn: () => api.selectPkg(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["package"] }),
  });
  const reprice = useMutation({
    mutationFn: () => api.reprice(id),
    onSuccess: (r) => {
      if (r.changes.some((c) => c.status === "unavailable")) setNote(t.priceUnavailable);
      else if (r.total_current.amount !== r.total_previous.amount) setNote(t.priceChanged(money(r.total_previous), money(r.total_current)));
      else setNote(t.priceSame);
      qc.invalidateQueries({ queryKey: ["package"] });
      if (r.new_package_id) onSwitch(r.new_package_id);
    },
    onError: (e) => setNote(e instanceof ApiError ? e.detail ?? e.message : t.errors.generic!),
  });
  const p = q.data;
  return (
    <Sheet open onClose={onClose} title={p?.title ?? "…"}>
      {!p ? (
        <Spinner />
      ) : (
        <>
          <motion.div initial={{ opacity: 0, y: 16 }} animate={{ opacity: 1, y: 0 }} className={`-mx-1 mb-5 rounded-[28px] bg-gradient-to-br ${tierStyle[p.tier] ?? "from-ink to-night"} p-6 text-white`}>
            <p className="text-xs font-bold uppercase tracking-[.2em] text-white/75">{t.tiers[p.tier]}</p>
            <p className="mt-2 font-display text-5xl font-semibold tabular-nums">{money(p.total)}</p>
            <div className="mt-3 flex flex-wrap gap-1.5">
              <Badge tone={feasTone[p.feasibility]}>{t.feasibility[p.feasibility]}</Badge>
              {p.status === "stale" && <Badge tone="est">{t.stale}</Badge>}
              {p.status === "selected" && <Badge tone="ok">✓ {t.selected}</Badge>}
            </div>
          </motion.div>
          {p.items?.some((i) => i.source.label.startsWith("Sandbox")) && <p className="mb-3 rounded-2xl bg-est-soft px-4 py-2.5 text-xs text-est">{t.sandboxBanner}</p>}
          {p.items?.some((i) => i.source.label.startsWith("DRAF")) && <p className="mb-3 rounded-2xl bg-ink/5 px-4 py-2.5 text-xs text-muted">{t.draftNorms}</p>}
          <div className="mb-5 flex flex-wrap items-center gap-2">
            <Button onClick={() => select.mutate()} disabled={p.status !== "proposed" || select.isPending}>
              {p.status === "selected" ? `✓ ${t.selected}` : t.select}
            </Button>
            <Button variant="outline" onClick={() => reprice.mutate()} disabled={reprice.isPending}>
              {reprice.isPending ? t.repricing : t.reprice}
            </Button>
            {conversationId && <RemindButton conversationId={conversationId} />}
            <PackageFeedback id={p.id} />
          </div>
          {note && (
            <motion.p initial={{ opacity: 0, y: -6 }} animate={{ opacity: 1, y: 0 }} className="mb-4 rounded-2xl bg-ink/5 px-4 py-2.5 text-sm">
              {note}
            </motion.p>
          )}
          <Tabs<Tab>
            value={tab}
            onChange={setTab}
            tabs={[
              { id: "breakdown", label: t.breakdown },
              { id: "itinerary", label: t.itinerary },
              { id: "assumptions", label: t.assumptions },
            ]}
          />
          <motion.div key={tab} initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }}>
            {tab === "breakdown" && <Breakdown p={p} />}
            {tab === "itinerary" && <Timeline days={p.days ?? []} items={p.items ?? []} />}
            {tab === "assumptions" && <Assumptions p={p} />}
          </motion.div>
          <p className="mt-8 text-xs text-muted">{t.disclaimer}</p>
        </>
      )}
    </Sheet>
  );
}

function Breakdown({ p }: { p: Package }) {
  const t = useT();
  const items = p.items ?? [];
  const groups = new Map<string, PackageItem[]>();
  for (const it of items) {
    const g = groups.get(it.category) ?? [];
    g.push(it);
    groups.set(it.category, g);
  }
  return (
    <div className="space-y-5">
      <dl className="grid grid-cols-2 gap-x-6 gap-y-2 rounded-3xl border border-line bg-card p-4 text-sm">
        <Row label={t.live} value={money(p.breakdown.live)} tone="text-live font-semibold" />
        <Row label={t.estimate} value={money(p.breakdown.estimate)} tone="text-est font-semibold" />
        <Row label={t.buffer} value={money(p.breakdown.buffer)} />
        {p.budget ? <Row label={t.accountExtra.budget} value={money(p.budget)} /> : <span />}
        {p.budget && p.feasibility !== "over_budget" && <Row label={t.remaining} value={money(p.breakdown.remaining)} />}
        {p.breakdown.shortfall.amount > 0 && <Row label={t.shortfall} value={money(p.breakdown.shortfall)} tone="text-bad font-semibold" />}
      </dl>
      {[...groups.entries()].map(([cat, list], gi) => (
        // No money arithmetic in the client: totals come from the API only.
        <motion.section key={cat} initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: gi * 0.05 }}>
          <div className="mb-2 flex items-baseline justify-between">
            <h3 className="flex items-center gap-2 font-display text-lg font-semibold">
              <span className="grid h-7 w-7 place-items-center rounded-full bg-paper-deep text-sm">{catIcon[cat] ?? "•"}</span>
              {t.categories[cat] ?? cat}
            </h3>
            <span className="text-xs text-muted">{t.accountExtra.items(list.length)}</span>
          </div>
          <ul className="divide-y divide-line overflow-hidden rounded-3xl border border-line bg-card">
            {list.map((it) => (
              <ItemRow key={it.id} it={it} pkgId={p.id} />
            ))}
          </ul>
        </motion.section>
      ))}
    </div>
  );
}

function ItemRow({ it, pkgId }: { it: PackageItem; pkgId: string }) {
  const t = useT();
  const expired = it.price_kind === "live" && isExpired(it.expires_at);
  const out = useMutation({
    mutationFn: () => api.outbound(pkgId, it.id),
    onSuccess: (r) => window.open(r.url, "_blank", "noopener,noreferrer"),
  });
  return (
    <li className="px-4 py-3 text-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="font-medium">{it.title}</p>
          <p className="mt-0.5 text-xs text-muted">
            {it.day_index !== null && it.day_index !== undefined ? `${t.day(it.day_index + 1)} · ` : ""}
            {it.unit_price.currency !== "IDR" && `${money(it.unit_price)} · `}
            {t.source}: {it.source.label} · {t.pricedAt} {dateTime(it.priced_at)}
          </p>
        </div>
        <div className="shrink-0 text-right">
          <p className="font-semibold tabular-nums">{money(it.amount_idr)}</p>
          <div className="mt-1 flex justify-end gap-1">
            <Badge tone={it.price_kind === "live" ? "live" : "est"}>{it.price_kind === "live" ? t.live : t.estimate}</Badge>
            {expired && <Badge tone="bad">{t.expired}</Badge>}
          </div>
        </div>
      </div>
      {it.has_outbound && !expired && (
        <button className="mt-1.5 text-xs font-semibold text-brand underline underline-offset-2" onClick={() => out.mutate()}>
          {t.openOffer} ↗
        </button>
      )}
    </li>
  );
}

function Assumptions({ p }: { p: Package }) {
  const t = useT();
  return (
    <div className="space-y-5 text-sm">
      {p.alternatives.length > 0 && (
        <section>
          <h3 className="mb-2 font-display text-lg font-semibold">{t.alternatives}</h3>
          <ol className="space-y-2">
            {p.alternatives.map((a, i) => (
              <li key={a.kind} className="flex gap-3 rounded-2xl bg-card p-3">
                <span className="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-brand text-xs font-bold text-white">{i + 1}</span>
                {a.label}
              </li>
            ))}
          </ol>
        </section>
      )}
      <section>
        <h3 className="mb-2 font-display text-lg font-semibold">{t.assumptions}</h3>
        <ul className="space-y-2">
          {p.assumptions.map((a, i) => (
            <li key={a.slot + i} className="rounded-2xl border border-dashed border-gold/60 bg-est-soft/50 px-4 py-2.5">
              {a.reason}
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}

function RemindButton({ conversationId }: { conversationId: string }) {
  const t = useT();
  const [msg, setMsg] = useState<string | null>(null);
  const m = useMutation({
    mutationFn: () => api.createReminder(conversationId, "booking_window_open"),
    onSuccess: () => setMsg(t.reminded),
    onError: (e) => setMsg(e instanceof ApiError && e.status === 403 ? t.needLogin : t.errors.generic!),
  });
  return msg ? (
    <span className="self-center text-xs text-muted">{msg}</span>
  ) : (
    <Button variant="ghost" onClick={() => m.mutate()} disabled={m.isPending}>
      🔔 {t.remind}
    </Button>
  );
}

function PackageFeedback({ id }: { id: string }) {
  const t = useT();
  const [done, setDone] = useState(false);
  const m = useMutation({ mutationFn: (r: 1 | -1) => api.feedback({ package_id: id, rating: r, reason: r === -1 ? "price_wrong" : undefined }), onSuccess: () => setDone(true) });
  if (done) return <span className="self-center text-xs text-muted">{t.thanks}</span>;
  return (
    <span className="flex gap-1 self-center">
      <button className="rounded-full px-1.5 text-muted hover:bg-ink/5" aria-label={t.helpful} onClick={() => m.mutate(1)}>
        👍
      </button>
      <button className="rounded-full px-1.5 text-muted hover:bg-ink/5" aria-label={t.notHelpful} onClick={() => m.mutate(-1)}>
        👎
      </button>
    </span>
  );
}
