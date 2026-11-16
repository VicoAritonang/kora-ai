"use client";
import { useMemo } from "react";
import { journeysFor, type Journey } from "@/lib/journeys";
import { useLocale } from "@/i18n";

/** The curated journeys in the current UI language. */
export function useJourneys(): Journey[] {
  const { locale } = useLocale();
  return useMemo(() => journeysFor(locale), [locale]);
}
