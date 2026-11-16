"use client";
import { QueryClient, QueryClientProvider, useQuery, useQueryClient } from "@tanstack/react-query";
import { MotionConfig, motion } from "motion/react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { api, bootstrap } from "@/lib/api/client";
import { I18nProvider, useLocale, useT, type Locale } from "@/i18n";
import { Orb } from "@/features/orb/Orb";

function Session({ children }: { children: ReactNode }) {
  const t = useT();
  const me = useQuery({ queryKey: ["me"], queryFn: bootstrap, staleTime: 60_000, retry: 2 });
  if (me.isPending) {
    return (
      <div className="grid min-h-screen place-items-center bg-night">
        <motion.div className="flex flex-col items-center gap-4" initial={{ opacity: 0, scale: 0.8 }} animate={{ opacity: 1, scale: 1 }}>
          <Orb mode="thinking" size={72} />
          <span className="font-display text-2xl font-semibold text-white">Kora</span>
        </motion.div>
      </div>
    );
  }
  if (me.isError) {
    return (
      <div className="grid min-h-screen place-items-center bg-night p-6 text-center text-white">
        <div className="flex flex-col items-center">
          <Orb size={64} />
          <p className="mt-5 font-display text-2xl font-semibold">{t.server.unreachable}</p>
          <p className="mt-2 text-sm text-white/60">{t.server.check(process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080")}</p>
          <button onClick={() => me.refetch()} className="mt-6 rounded-full bg-brand px-5 py-2 text-sm font-semibold">
            {t.retry}
          </button>
        </div>
      </div>
    );
  }
  return (
    <>
      <SyncProfileLocale profileLocale={me.data.locale} />
      {children}
    </>
  );
}

/** Once per session, aligns the profile with the UI language (English by
 * default), so server-written text such as action messages matches it. */
function SyncProfileLocale({ profileLocale }: { profileLocale: string }) {
  const qc = useQueryClient();
  const { locale } = useLocale();
  const done = useRef(false);
  useEffect(() => {
    if (done.current || profileLocale === locale) return;
    done.current = true;
    api
      .patchMe({ locale })
      .then((u) => qc.setQueryData(["me"], u))
      .catch(() => {
        /* best effort */
      });
  }, [profileLocale, locale, qc]);
  return null;
}

/** Keeps the profile's locale in step with the UI language (best effort). */
function Language({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const onChange = useCallback(
    (locale: Locale) => {
      api
        .patchMe({ locale })
        .then((u) => qc.setQueryData(["me"], u))
        .catch(() => {
          /* the UI language does not depend on the profile */
        });
    },
    [qc],
  );
  return <I18nProvider onChange={onChange}>{children}</I18nProvider>;
}

export function Providers({ children }: { children: ReactNode }) {
  const [client] = useState(() => new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: false } } }));
  return (
    <QueryClientProvider client={client}>
      <MotionConfig reducedMotion="user">
        <Language>
          <Session>{children}</Session>
        </Language>
      </MotionConfig>
    </QueryClientProvider>
  );
}
