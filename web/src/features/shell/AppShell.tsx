"use client";
// App frame: a top bar that floats over each page's scenic hero and turns to
// paper glass on scroll, the footer, and — outside a conversation — the
// launcher orb. Inside /c/[id] the workspace mounts its own connected orb.
import { useQuery } from "@tanstack/react-query";
import { AnimatePresence, motion, useMotionValueEvent, useScroll } from "motion/react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { api, bootstrap } from "@/lib/api/client";
import { useT } from "@/i18n";
import { Orb } from "@/features/orb/Orb";
import { OrbDock } from "@/features/orb/OrbDock";
import { LauncherChat } from "@/features/orb/LauncherChat";
import { useOrb } from "@/features/orb/store";
import { LanguageSwitch } from "./LanguageSwitch";

export function AppShell({ children }: { children: ReactNode }) {
  const path = usePathname();
  const inConversation = path.startsWith("/c/");
  const solid = path.startsWith("/account") || path.startsWith("/auth");
  return (
    <>
      <TopBar solid={solid} />
      <main className="min-h-screen">{children}</main>
      {!inConversation && <Footer />}
      {!inConversation && <OrbDock mode="idle" panel={<LauncherChat />} />}
    </>
  );
}

function TopBar({ solid }: { solid: boolean }) {
  const t = useT();
  const path = usePathname();
  const me = useQuery({ queryKey: ["me"], queryFn: bootstrap, staleTime: 60_000 }).data;
  const meta = useQuery({ queryKey: ["meta"], queryFn: api.meta, staleTime: 3_600_000 });
  const { scrollY } = useScroll();
  const [scrolled, setScrolled] = useState(false);
  const [menu, setMenu] = useState(false);
  useMotionValueEvent(scrollY, "change", (y) => setScrolled(y > 40));
  useEffect(() => setMenu(false), [path]);
  const light = solid || scrolled || menu;
  const links = [
    { href: "/journeys", label: t.nav.journeys },
    { href: "/tickets", label: t.nav.tickets },
    { href: "/trips", label: t.nav.trips },
  ];
  const active = (href: string) => path === href || path.startsWith(`${href}/`) || (href === "/trips" && path.startsWith("/c/"));

  return (
    <header className={`fixed inset-x-0 top-0 z-40 transition-colors duration-500 ${light ? "glass border-b border-line/70 text-ink" : "text-white"}`}>
      <div className="mx-auto flex h-16 max-w-7xl items-center gap-4 px-4 md:px-8">
        <Link href="/" className="flex items-center gap-2.5" aria-label="Kora">
          <Orb size={30} />
          <span className="font-display text-2xl font-semibold tracking-tight">Kora</span>
        </Link>
        <nav className="ml-6 hidden items-center gap-1 md:flex">
          {links.map((l) => (
            <Link key={l.href} href={l.href} className={`relative rounded-full px-4 py-2 text-sm font-semibold transition ${light ? "hover:bg-ink/5" : "hover:bg-white/10"}`}>
              {active(l.href) && <motion.span layoutId="nav-pill" className={`absolute inset-0 rounded-full ${light ? "bg-ink/[.07]" : "bg-white/15"}`} />}
              <span className="relative">{l.label}</span>
            </Link>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-2">
          {meta.data?.sandbox && (
            <span title={t.sandboxBanner} className={`hidden rounded-full px-2.5 py-1 text-[11px] font-bold uppercase tracking-wider lg:inline ${light ? "bg-est-soft text-est" : "bg-white/15 text-white"}`}>
              {t.nav.sandbox}
            </span>
          )}
          {me && (
            <span className={`hidden text-xs lg:inline ${light ? "text-muted" : "text-white/75"}`}>{t.quota(me.quota.runs_remaining)}</span>
          )}
          <span className="hidden sm:block">
            <LanguageSwitch light={light} />
          </span>
          <Link href="/account" className={`hidden rounded-full px-3 py-2 text-sm font-semibold md:inline ${light ? "hover:bg-ink/5" : "hover:bg-white/10"}`}>
            {me?.is_guest === false ? (me.display_name ?? me.email ?? t.nav.account) : t.nav.account}
          </Link>
          <button
            onClick={() => useOrb.getState().setOpen(true)}
            className="hidden items-center gap-2 rounded-full bg-brand py-1.5 pl-1.5 pr-4 text-sm font-semibold text-white shadow-[0_8px_20px_-8px_rgba(224,67,42,.9)] transition hover:bg-brand-deep sm:inline-flex"
          >
            <Orb size={24} />
            {t.nav.ask}
          </button>
          <button onClick={() => setMenu((m) => !m)} className="grid h-10 w-10 place-items-center rounded-full md:hidden" aria-label={t.nav.menu} aria-expanded={menu}>
            <svg width="20" height="20" viewBox="0 0 20 20" aria-hidden>
              <motion.path d="M3 6h14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" animate={menu ? { d: "M4 4l12 12" } : { d: "M3 6h14" }} />
              <motion.path d="M3 14h14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" animate={menu ? { d: "M4 16L16 4" } : { d: "M3 14h14" }} />
            </svg>
          </button>
        </div>
      </div>
      <AnimatePresence>
        {menu && (
          <motion.nav
            className="overflow-hidden border-t border-line/60 md:hidden"
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
          >
            <div className="flex flex-col gap-1 px-4 py-4">
              {[...links, { href: "/account", label: t.nav.account }].map((l, i) => (
                <motion.div key={l.href} initial={{ opacity: 0, x: -12 }} animate={{ opacity: 1, x: 0 }} transition={{ delay: i * 0.05 }}>
                  <Link href={l.href} className="block rounded-2xl px-4 py-3 font-display text-2xl font-semibold hover:bg-ink/5">
                    {l.label}
                  </Link>
                </motion.div>
              ))}
              <div className="flex items-center justify-between px-4 pt-3">
                <span className="text-xs font-semibold text-muted">{t.lang.label}</span>
                <LanguageSwitch light id="lang-mobile" />
              </div>
              {me && <p className="px-4 pt-2 text-xs text-muted">{t.quota(me.quota.runs_remaining)}</p>}
            </div>
          </motion.nav>
        )}
      </AnimatePresence>
    </header>
  );
}

function Footer() {
  const t = useT();
  return (
    <footer className="bg-night text-white/70">
      <div className="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-12 md:flex-row md:items-end md:justify-between md:px-8">
        <div>
          <div className="flex items-center gap-2.5 text-white">
            <Orb size={32} />
            <span className="font-display text-2xl font-semibold">Kora</span>
          </div>
          <p className="mt-3 max-w-md text-sm">{t.landing.footer}</p>
        </div>
        <nav className="flex flex-wrap gap-x-6 gap-y-2 text-sm font-semibold">
          <Link href="/journeys" className="hover:text-white">
            {t.nav.journeys}
          </Link>
          <Link href="/tickets" className="hover:text-white">
            {t.nav.tickets}
          </Link>
          <Link href="/trips" className="hover:text-white">
            {t.nav.trips}
          </Link>
          <Link href="/account" className="hover:text-white">
            {t.nav.account}
          </Link>
        </nav>
      </div>
      <p className="border-t border-white/10 px-4 py-4 text-center text-[11px] text-white/45 md:px-8">{t.disclaimer}</p>
    </footer>
  );
}
