"use client";
import { motion } from "motion/react";
import { useLocale, useT, type Locale } from "@/i18n";

const options: Locale[] = ["en", "id"];

/** EN | ID toggle; `light` is the paper navbar, otherwise it sits on a dark hero. */
export function LanguageSwitch({ light, id = "lang" }: { light: boolean; id?: string }) {
  const t = useT();
  const { locale, setLocale } = useLocale();
  return (
    <div role="radiogroup" aria-label={t.lang.label} className={`relative flex rounded-full p-0.5 text-[11px] font-bold tracking-wider ${light ? "bg-ink/[.07]" : "bg-white/15"}`}>
      {options.map((l) => {
        const on = locale === l;
        return (
          <button
            key={l}
            role="radio"
            aria-checked={on}
            title={on ? undefined : t.lang.switchTo}
            onClick={() => !on && setLocale(l)}
            className={`relative rounded-full px-2.5 py-1 transition-colors ${on ? (light ? "text-paper" : "text-ink") : light ? "text-muted hover:text-ink" : "text-white/70 hover:text-white"}`}
          >
            {on && <motion.span layoutId={`${id}-pill`} className={`absolute inset-0 rounded-full ${light ? "bg-ink" : "bg-white"}`} transition={{ type: "spring", damping: 30, stiffness: 420 }} />}
            <span className="relative">{l === "en" ? t.lang.en : t.lang.id}</span>
          </button>
        );
      })}
    </div>
  );
}
