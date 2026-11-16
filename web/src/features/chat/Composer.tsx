"use client";
import { useEffect, useRef, useState } from "react";
import { useT } from "@/i18n";

export function Composer({ busy, onSend, onCancel, autoFocus = false, placeholder }: { busy: boolean; onSend: (s: string) => void; onCancel?: () => void; autoFocus?: boolean; placeholder?: string }) {
  const t = useT();
  const [text, setText] = useState("");
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (autoFocus) ref.current?.focus();
  }, [autoFocus]);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
  }, [text]);
  const submit = () => {
    const s = text.trim();
    if (!s || busy) return;
    onSend(s);
    setText("");
  };
  return (
    <div className="px-3 pb-3 pt-2">
      <div className="flex items-end gap-2 rounded-[22px] border border-line bg-card p-1.5 pl-4 shadow-sm transition focus-within:border-brand/50 focus-within:shadow-[0_0_0_4px_rgba(224,67,42,.08)]">
        <label htmlFor="composer" className="sr-only">
          {t.placeholder}
        </label>
        <textarea
          id="composer"
          ref={ref}
          value={text}
          maxLength={4000}
          rows={1}
          placeholder={placeholder ?? t.placeholder}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
          className="max-h-40 min-h-[40px] flex-1 resize-none bg-transparent py-2.5 text-[15px] leading-snug outline-none placeholder:text-muted/70"
        />
        {onCancel ? (
          <button onClick={onCancel} className="h-10 shrink-0 rounded-full border border-line px-4 text-sm font-semibold hover:bg-ink/5">
            {t.cancel}
          </button>
        ) : (
          <button
            onClick={submit}
            disabled={busy || !text.trim()}
            aria-label={t.send}
            className="grid h-10 w-10 shrink-0 place-items-center rounded-full bg-brand text-white transition hover:bg-brand-deep disabled:bg-ink/15"
          >
            <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden>
              <path d="M8 13V3M3.5 7.5L8 3l4.5 4.5" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
        )}
      </div>
      <p className="mt-1.5 px-3 text-[10.5px] text-muted">{t.orb.hint}</p>
    </div>
  );
}
