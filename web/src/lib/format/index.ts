// Formatting only — no money arithmetic happens in the client
// (ENGINEERING_STANDARDS §1.2).
import type { Money } from "@/lib/api/client";

const minor: Record<string, number> = { IDR: 0, JPY: 0, KRW: 0 };

// The UI language picks the number/date locale (set by I18nProvider).
let current = "id-ID";
export function setFormatLocale(locale: string) {
  current = locale;
}
export const formatLocale = () => current;

export function money(m: Money | null | undefined, locale = current): string {
  if (!m) return "—";
  const exp = minor[m.currency] ?? 2;
  const value = m.amount / 10 ** exp;
  return new Intl.NumberFormat(locale, {
    style: "currency",
    currency: m.currency,
    // "Rp 17,850,000" in English rather than "IDR 17,850,000".
    currencyDisplay: locale.startsWith("en") ? "narrowSymbol" : "symbol",
    minimumFractionDigits: 0,
    maximumFractionDigits: exp,
  }).format(value);
}

export function rupiah(amount: number | null | undefined, locale = current): string {
  if (amount === null || amount === undefined) return "—";
  return money({ amount, currency: "IDR" }, locale);
}

export function date(iso: string | null | undefined, locale = current): string {
  if (!iso) return "";
  const d = new Date(iso.length === 10 ? `${iso}T00:00:00` : iso);
  return new Intl.DateTimeFormat(locale, { day: "numeric", month: "short", year: "numeric" }).format(d);
}

export function dayDate(iso: string | null | undefined, locale = current): string {
  if (!iso) return "";
  const d = new Date(`${iso}T00:00:00`);
  return new Intl.DateTimeFormat(locale, { weekday: "short", day: "numeric", month: "short" }).format(d);
}

export function dateTime(iso: string | null | undefined, locale = current): string {
  if (!iso) return "";
  return new Intl.DateTimeFormat(locale, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }).format(new Date(iso));
}

export function isExpired(iso: string | null | undefined): boolean {
  return !!iso && new Date(iso).getTime() < Date.now();
}
