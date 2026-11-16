"use client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, ApiError, bootstrap, type Me } from "@/lib/api/client";
import { useLocale, useT, type Locale } from "@/i18n";
import { Badge, Button, Card } from "@/components/ui";

export function AccountView() {
  const t = useT();
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ["me"], queryFn: bootstrap });
  const meta = useQuery({ queryKey: ["meta"], queryFn: api.meta, staleTime: 3_600_000 });
  if (!me.data) return null;
  const u = me.data;
  return (
    <div className="mx-auto w-full max-w-2xl space-y-5 px-4 pb-24 pt-28">
      <h1 className="font-display text-5xl font-semibold">{t.account}</h1>
      {u.is_guest ? <Login /> : <Signed me={u} onLogout={() => qc.invalidateQueries()} />}
      <Profile me={u} origins={meta.data?.origins.map((o) => ({ code: o.code, label: o.label })) ?? []} />
      <Card className="space-y-4 p-6">
        <h2 className="font-display text-xl font-semibold">{t.accountExtra.data}</h2>
        <div className="flex flex-wrap gap-2">
          <a className="inline-flex items-center rounded-full border border-line bg-paper px-4 py-2 text-sm font-semibold hover:bg-ink/5" href={api.exportUrl()} target="_blank" rel="noopener">
            {t.exportData}
          </a>
          <DeleteAccount />
        </div>
      </Card>
    </div>
  );
}

function Login() {
  const t = useT();
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const m = useMutation({
    mutationFn: () => api.magicLink(email),
    onSuccess: () => setSent(true),
    onError: (e) => setErr(e instanceof ApiError ? e.message : t.errors.generic!),
  });
  return (
    <Card className="space-y-4 p-6">
      <h2 className="font-display text-xl font-semibold">{t.loginTitle}</h2>
      <p className="text-sm text-muted">{t.loginBody}</p>
      {sent ? (
        <p className="rounded-lg bg-live-soft px-3 py-2 text-sm text-live">{t.emailSent}</p>
      ) : (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            m.mutate();
          }}
        >
          <label htmlFor="email" className="sr-only">
            Email
          </label>
          <input id="email" type="email" required maxLength={254} value={email} onChange={(e) => setEmail(e.target.value)} placeholder={t.accountExtra.emailPlaceholder}
            className="flex-1 rounded-xl border border-line bg-paper px-3 py-2 text-sm" />
          <Button type="submit" disabled={m.isPending}>
            {t.login}
          </Button>
        </form>
      )}
      {err && <p className="text-sm text-bad">{err}</p>}
    </Card>
  );
}

function Signed({ me, onLogout }: { me: Me; onLogout: () => void }) {
  const t = useT();
  const m = useMutation({ mutationFn: api.logout, onSuccess: onLogout });
  return (
    <Card className="flex items-center justify-between p-6">
      <div>
        <p className="font-medium">{me.email}</p>
        <Badge tone="ok">{t.quota(me.quota.runs_remaining)}</Badge>
      </div>
      <Button variant="outline" onClick={() => m.mutate()}>
        {t.logout}
      </Button>
    </Card>
  );
}

function Profile({ me, origins }: { me: Me; origins: { code: string; label: string }[] }) {
  const t = useT();
  const qc = useQueryClient();
  const [name, setName] = useState(me.display_name ?? "");
  const [city, setCity] = useState(me.home_city_code ?? "");
  const ui = useLocale();
  const [locale, setLocale] = useState<Locale>(ui.locale);
  const [memory, setMemory] = useState(me.memory_enabled);
  const [ok, setOk] = useState(false);
  const m = useMutation({
    mutationFn: () => api.patchMe({ display_name: name || undefined, home_city_code: city || undefined, locale, memory_enabled: memory }),
    onSuccess: (u) => {
      qc.setQueryData(["me"], u);
      if (locale !== ui.locale) ui.setLocale(locale);
      setOk(true);
    },
  });
  return (
    <Card className="space-y-4 p-6">
      <h2 className="font-display text-xl font-semibold">{t.profile}</h2>
      <label className="flex items-center justify-between gap-3 text-sm">
        <span>{t.accountExtra.name}</span>
        <input className="w-44 sm:w-56 rounded-xl border border-line bg-paper px-2 py-1.5" value={name} maxLength={80} onChange={(e) => setName(e.target.value)} />
      </label>
      <label className="flex items-center justify-between gap-3 text-sm">
        <span>{t.homeCity}</span>
        <select className="w-44 sm:w-56 rounded-xl border border-line bg-paper px-2 py-1.5" value={city} onChange={(e) => setCity(e.target.value)}>
          <option value="">—</option>
          {origins.map((o) => (
            <option key={o.code} value={o.code}>
              {o.label}
            </option>
          ))}
        </select>
      </label>
      <label className="flex items-center justify-between gap-3 text-sm">
        <span>{t.language}</span>
        <select className="w-44 sm:w-56 rounded-xl border border-line bg-paper px-2 py-1.5" value={locale} onChange={(e) => setLocale(e.target.value as Locale)}>
          <option value="id">Bahasa Indonesia</option>
          <option value="en">English</option>
        </select>
      </label>
      <label className="flex items-start justify-between gap-3 text-sm">
        <span>
          {t.memory}
          {!me.memory_available && <span className="block text-xs text-muted">{t.memoryUnavailable}</span>}
        </span>
        <input type="checkbox" checked={memory} disabled={!me.memory_available || me.is_guest} onChange={(e) => setMemory(e.target.checked)} />
      </label>
      <div className="flex items-center gap-3">
        <Button onClick={() => m.mutate()} disabled={m.isPending}>
          {t.save}
        </Button>
        {ok && <span className="text-sm text-live">{t.saved}</span>}
      </div>
    </Card>
  );
}

function DeleteAccount() {
  const t = useT();
  const qc = useQueryClient();
  const m = useMutation({ mutationFn: api.deleteMe, onSuccess: () => qc.invalidateQueries() });
  return (
    <Button
      variant="danger"
      onClick={() => {
        if (window.confirm(t.deleteConfirm)) m.mutate();
      }}
    >
      {t.deleteAccount}
    </Button>
  );
}
