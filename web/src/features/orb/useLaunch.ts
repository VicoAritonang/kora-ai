"use client";
// Starts a new conversation with a first message and moves the traveller to
// its live workspace, with the orb panel open so they see Kora at work.
import { useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useCallback } from "react";
import { ApiError, api } from "@/lib/api/client";
import { useT } from "@/i18n";
import { useOrb } from "./store";

export function useLaunchKora() {
  const t = useT();
  const router = useRouter();
  const qc = useQueryClient();
  return useCallback(
    async (text: string) => {
      const s = text.trim();
      const orb = useOrb.getState();
      if (!s || orb.launching) return;
      orb.setLaunch({ launching: true });
      orb.setOpen(true);
      try {
        const c = await api.createConversation();
        const r = await api.send(c.id, s);
        qc.setQueryData(["messages", c.id], { items: [r.message], next_cursor: null });
        qc.invalidateQueries({ queryKey: ["conversations"] });
        qc.invalidateQueries({ queryKey: ["me"] });
        router.push(`/c/${c.id}`);
        useOrb.getState().setLaunch({ launching: false });
      } catch (e) {
        const msg = e instanceof ApiError ? t.errors[e.code] ?? e.detail ?? t.errors.generic! : t.errors.generic!;
        useOrb.getState().setLaunch({ launching: false, launchError: msg });
      }
    },
    [qc, router, t],
  );
}
