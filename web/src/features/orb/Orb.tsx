"use client";
import type { OrbMode } from "./store";

/** The Kora sphere: swirling gradient, two blinking eyes, rings while working. */
export function Orb({ mode = "idle", size = 60, className = "" }: { mode?: OrbMode; size?: number; className?: string }) {
  const working = mode === "thinking" || mode === "acting";
  return (
    <span className={`orb inline-block shrink-0 ${className}`} data-mode={mode} style={{ width: size, height: size }} aria-hidden>
      <span className="orb-glow" />
      <span className="orb-core">
        <span className="orb-swirl" />
        <span className="orb-shine" />
        {size >= 26 && (
          <span className="orb-eyes">
            <span className="orb-eye" />
            <span className="orb-eye" />
          </span>
        )}
      </span>
      {working && (
        <>
          <span className="orb-ring" />
          <span className="orb-ring" />
        </>
      )}
    </span>
  );
}
