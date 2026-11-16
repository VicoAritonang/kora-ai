"use client";
import { motion, useMotionValue, useSpring, useTransform } from "motion/react";
import Link from "next/link";
import type { Journey } from "@/lib/journeys";
import { useT } from "@/i18n";
import { Scene } from "@/components/scene/Scene";

/** Tall story card with a live mini-scene and a gentle 3D tilt. */
export function JourneyCard({ j, index = 0 }: { j: Journey; index?: number }) {
  const t = useT();
  const mx = useMotionValue(0.5);
  const my = useMotionValue(0.5);
  const rx = useSpring(useTransform(my, [0, 1], [6, -6]), { stiffness: 200, damping: 20 });
  const ry = useSpring(useTransform(mx, [0, 1], [-8, 8]), { stiffness: 200, damping: 20 });
  return (
    <motion.div
      initial={{ opacity: 0, y: 40 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, margin: "-10% 0px" }}
      transition={{ duration: 0.8, delay: (index % 3) * 0.1, ease: [0.22, 1, 0.36, 1] }}
      style={{ perspective: 1000 }}
    >
      <motion.div
        style={{ rotateX: rx, rotateY: ry }}
        onMouseMove={(e) => {
          const r = e.currentTarget.getBoundingClientRect();
          mx.set((e.clientX - r.left) / r.width);
          my.set((e.clientY - r.top) / r.height);
        }}
        onMouseLeave={() => {
          mx.set(0.5);
          my.set(0.5);
        }}
        className="group relative"
      >
        <Link href={`/journeys/${j.slug}`} className="relative block aspect-[4/5] overflow-hidden rounded-[28px] bg-night shadow-[0_30px_60px_-30px_rgba(13,18,38,.6)]">
          <div className="absolute inset-0 transition duration-700 ease-[cubic-bezier(.22,1,.36,1)] group-hover:scale-[1.06]">
            <Scene variant={j.scene} time={j.chapters[0]?.time ?? "dusk"} parallax={false} compact />
          </div>
          <div className="absolute inset-0 bg-gradient-to-t from-night via-night/40 to-transparent" />
          <div className="absolute inset-x-0 top-0 flex items-center justify-between p-5">
            <span className="glass-dark rounded-full px-3 py-1 text-[11px] font-bold uppercase tracking-[.16em] text-white">{j.kicker}</span>
            <span className="glass-dark rounded-full px-3 py-1 text-[11px] font-semibold text-white/85">{j.season}</span>
          </div>
          <div className="absolute inset-x-0 bottom-0 p-6 text-white">
            <h3 className="font-display text-[28px] font-semibold leading-[1.05]">{j.title}</h3>
            <p className="mt-2 line-clamp-2 text-sm text-white/75">{j.tagline}</p>
            <div className="mt-4 flex items-center justify-between">
              <div className="flex flex-wrap gap-1.5">
                {j.mood.map((m) => (
                  <span key={m} className="rounded-full border border-white/25 px-2.5 py-0.5 text-[11px] font-semibold text-white/85">
                    {m}
                  </span>
                ))}
              </div>
              <span className="flex items-center gap-1 text-sm font-semibold opacity-0 transition group-hover:translate-x-0 group-hover:opacity-100 md:-translate-x-2">
                {t.journey.read} →
              </span>
            </div>
          </div>
        </Link>
      </motion.div>
    </motion.div>
  );
}
