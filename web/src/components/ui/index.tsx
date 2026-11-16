"use client";
// Small accessible primitives (shadcn-style: the code lives in the repo).
import { AnimatePresence, motion, type HTMLMotionProps } from "motion/react";
import { useEffect, type ButtonHTMLAttributes, type ReactNode } from "react";
import { useT } from "@/i18n";

type Variant = "primary" | "ghost" | "outline" | "danger" | "light" | "dark";

export function Button({ variant = "primary", className = "", ...p }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant }) {
  const base =
    "inline-flex items-center justify-center gap-2 rounded-full px-4 py-2 text-sm font-semibold transition duration-200 active:scale-[.97] disabled:opacity-50 disabled:cursor-not-allowed disabled:active:scale-100";
  const v: Record<Variant, string> = {
    primary: "bg-brand text-white shadow-[0_8px_20px_-8px_rgba(224,67,42,.8)] hover:bg-brand-deep",
    ghost: "text-ink hover:bg-ink/5",
    outline: "border border-line bg-card text-ink hover:border-ink/30 hover:bg-paper",
    danger: "bg-bad text-white hover:bg-red-800",
    light: "bg-white/90 text-ink shadow-lg hover:bg-white",
    dark: "bg-ink text-paper hover:bg-ink-soft",
  };
  return <button type="button" className={`${base} ${v[variant]} ${className}`} {...p} />;
}

export function Badge({ tone = "neutral", children, className = "" }: { tone?: "neutral" | "live" | "est" | "bad" | "brand" | "ok" | "night"; children: ReactNode; className?: string }) {
  const t = {
    neutral: "bg-ink/[.06] text-muted",
    live: "bg-live-soft text-live",
    est: "bg-est-soft text-est",
    bad: "bg-bad-soft text-bad",
    brand: "bg-brand-soft text-brand-deep",
    ok: "bg-emerald-50 text-emerald-700",
    night: "bg-night text-white",
  }[tone];
  return <span className={`inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-[11px] font-semibold tracking-wide ${t} ${className}`}>{children}</span>;
}

export function Card({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`rounded-3xl border border-line bg-card shadow-[0_1px_0_rgba(0,0,0,.02),0_12px_30px_-18px_rgba(40,25,10,.25)] ${className}`}>{children}</div>;
}

export function Spinner({ label, light = false }: { label?: string; light?: boolean }) {
  return (
    <span role="status" className={`inline-flex items-center gap-2 text-sm ${light ? "text-white/80" : "text-muted"}`}>
      <span className="relative h-4 w-4" aria-hidden>
        <span className="absolute inset-0 animate-spin rounded-full border-2 border-brand border-t-transparent" />
      </span>
      {label}
    </span>
  );
}

export function Kicker({ children, light = false }: { children: ReactNode; light?: boolean }) {
  return (
    <p className={`mb-3 inline-flex items-center gap-2 text-xs font-bold uppercase tracking-[.22em] ${light ? "text-white/70" : "text-brand"}`}>
      <span className={`h-px w-8 ${light ? "bg-white/50" : "bg-brand"}`} />
      {children}
    </p>
  );
}

/** Fades and lifts its children into view once, when scrolled to. */
export function Reveal({ children, delay = 0, y = 28, className = "", ...rest }: { children: ReactNode; delay?: number; y?: number; className?: string } & Omit<HTMLMotionProps<"div">, "children">) {
  return (
    <motion.div
      className={className}
      initial={{ opacity: 0, y }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, margin: "-12% 0px" }}
      transition={{ duration: 0.8, delay, ease: [0.22, 1, 0.36, 1] }}
      {...rest}
    >
      {children}
    </motion.div>
  );
}

export function Switch({ checked, onChange, label, title }: { checked: boolean; onChange: (v: boolean) => void; label: ReactNode; title?: string }) {
  return (
    <button type="button" role="switch" aria-checked={checked} title={title} onClick={() => onChange(!checked)} className="inline-flex items-center gap-2 text-xs font-semibold">
      <span className={`relative h-5 w-9 rounded-full transition ${checked ? "bg-brand" : "bg-ink/20"}`}>
        <motion.span layout className="absolute top-0.5 h-4 w-4 rounded-full bg-white shadow" style={{ left: checked ? 18 : 2 }} />
      </span>
      {label}
    </button>
  );
}

/** Right-side drawer with Escape to close and focus on open. */
export function Sheet({ open, onClose, title, children }: { open: boolean; onClose: () => void; title: string; children: ReactNode }) {
  const t = useT();
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);
  return (
    <AnimatePresence>
      {open && (
        <div className="fixed inset-0 z-[60] flex justify-end" role="dialog" aria-modal="true" aria-label={title}>
          <motion.button
            className="absolute inset-0 bg-night/50 backdrop-blur-sm"
            aria-label={t.close}
            onClick={onClose}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
          />
          <motion.div
            className="relative flex h-full w-full max-w-2xl flex-col bg-paper shadow-2xl"
            initial={{ x: "100%" }}
            animate={{ x: 0 }}
            exit={{ x: "100%" }}
            transition={{ type: "spring", damping: 32, stiffness: 300 }}
          >
            <div className="flex items-center justify-between border-b border-line px-6 py-4">
              <h2 className="font-display text-xl font-semibold">{title}</h2>
              <Button variant="ghost" onClick={onClose} autoFocus aria-label={t.close}>
                ✕
              </Button>
            </div>
            <div className="flex-1 overflow-y-auto px-6 py-5">{children}</div>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
}

export function Tabs<T extends string>({ value, onChange, tabs }: { value: T; onChange: (v: T) => void; tabs: { id: T; label: string }[] }) {
  return (
    <div role="tablist" className="mb-4 flex gap-1 rounded-full bg-ink/[.06] p-1">
      {tabs.map((t) => (
        <button
          key={t.id}
          role="tab"
          aria-selected={value === t.id}
          onClick={() => onChange(t.id)}
          className={`relative flex-1 rounded-full px-3 py-1.5 text-sm font-semibold transition ${value === t.id ? "text-ink" : "text-muted hover:text-ink"}`}
        >
          {value === t.id && <motion.span layoutId="tabs-pill" className="absolute inset-0 rounded-full bg-card shadow-sm" transition={{ type: "spring", damping: 30, stiffness: 400 }} />}
          <span className="relative">{t.label}</span>
        </button>
      ))}
    </div>
  );
}
