"use client";
import { useQueryClient } from "@tanstack/react-query";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api/client";
import { useT } from "@/i18n";
import { Button, Spinner } from "@/components/ui";

function Verify() {
  const t = useT();
  const token = useSearchParams().get("token") ?? "";
  const router = useRouter();
  const qc = useQueryClient();
  const [failed, setFailed] = useState(false);
  const ran = useRef(false);
  useEffect(() => {
    if (ran.current) return; // the token is single use; StrictMode runs effects twice
    ran.current = true;
    api
      .verify(token)
      .then((me) => {
        qc.setQueryData(["me"], me);
        qc.invalidateQueries();
        router.replace("/");
      })
      .catch(() => setFailed(true));
  }, [token, qc, router]);
  return (
    <div className="grid min-h-screen place-items-center bg-paper p-6 text-center">
      {failed ? (
        <div>
          <p className="mb-3">{t.verifyFailed}</p>
          <Button onClick={() => router.replace("/account")}>{t.login}</Button>
        </div>
      ) : (
        <Spinner label={t.verifying} />
      )}
    </div>
  );
}

export default function VerifyPage() {
  return (
    <Suspense>
      <Verify />
    </Suspense>
  );
}
