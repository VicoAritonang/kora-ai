"use client";
// Layered, code-drawn Japanese landscapes (no image assets). Each layer moves
// at its own speed on scroll (parallax) and every colour is a motion value
// interpolated across four times of day, so a story page can move the sky
// from dawn to night as the reader scrolls.
import { motion, useMotionTemplate, useMotionValue, useScroll, useTransform, type MotionValue } from "motion/react";
import { useEffect, useMemo, useRef } from "react";

export type SceneVariant = "osaka" | "kyoto" | "tokyo" | "fuji";
export type SceneTime = "dawn" | "day" | "dusk" | "night";

const TIMES: SceneTime[] = ["dawn", "day", "dusk", "night"];
const STOPS = [0, 1 / 3, 2 / 3, 1];
export const timeValue = (t: SceneTime) => TIMES.indexOf(t) / 3;

const P = {
  s1: ["#2a2c5e", "#5f9bd6", "#1c2048", "#060918"],
  s2: ["#c06a8a", "#a9d0ee", "#7d3d66", "#121838"],
  s3: ["#f7b98d", "#f4e7d3", "#f38a5a", "#2c2856"],
  far: ["#8c6390", "#a6b9cf", "#8f4c6e", "#2b3159"],
  mid: ["#5a4274", "#7189a8", "#4f2d59", "#1c2042"],
  near: ["#2a1f3f", "#33425d", "#1d1533", "#0d0e22"],
  ground: ["#1a1328", "#1f2a3f", "#120d22", "#07081a"],
  sun: ["#ffd9a8", "#fff6dc", "#ffb36b", "#f3eedf"],
  glow: ["rgba(255,190,140,0.55)", "rgba(255,245,210,0.55)", "rgba(255,150,90,0.6)", "rgba(220,225,255,0.3)"],
};

const W = 1440;
const H = 600;

// ---------------------------------------------------------------- geometry --
function rng(seed: number) {
  let s = seed >>> 0;
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 4294967296;
  };
}

function ridge(seed: number, base: number, amp: number, n: number): string {
  const r = rng(seed);
  const pts: [number, number][] = [];
  for (let i = 0; i <= n; i++) pts.push([(i / n) * W, base - amp * (0.35 + r() * 0.65)]);
  let d = `M0 ${H} L0 ${pts[0]![1]}`;
  for (let i = 1; i < pts.length; i++) {
    const [x0, y0] = pts[i - 1]!;
    const [x1, y1] = pts[i]!;
    d += ` Q ${x0 + (x1 - x0) / 2} ${Math.min(y0, y1) - amp * 0.25} ${x1} ${y1}`;
  }
  return d + ` L${W} ${H} Z`;
}

type Box = { x: number; y: number; w: number; h: number };

function skyline(seed: number, x0: number, x1: number, base: number, minH: number, maxH: number) {
  const r = rng(seed);
  const blocks: Box[] = [];
  const windows: Box[] = [];
  let x = x0;
  while (x < x1) {
    const w = 26 + r() * 46;
    const h = minH + r() * (maxH - minH);
    blocks.push({ x, y: base - h, w, h: h + 4 });
    if (r() > 0.35) {
      for (let wy = base - h + 10; wy < base - 8; wy += 13) {
        for (let wx = x + 6; wx < x + w - 6; wx += 10) if (r() > 0.55) windows.push({ x: wx, y: wy, w: 3.5, h: 5 });
      }
    }
    x += w + 2 + r() * 6;
  }
  return { blocks, windows };
}

function roof(cx: number, y: number, w: number, h: number) {
  return `M${cx - w} ${y} Q${cx - w * 0.72} ${y - h * 0.3} ${cx - w * 0.56} ${y - h} L${cx + w * 0.56} ${y - h} Q${cx + w * 0.72} ${y - h * 0.3} ${cx + w} ${y} Q${cx} ${y - h * 0.38} ${cx - w} ${y} Z`;
}

function pagoda(cx: number, base: number, s: number) {
  const parts: string[] = [`M${cx - 34 * s} ${base} h${68 * s} v${-8 * s} h${-68 * s} Z`];
  let y = base - 8 * s;
  for (let i = 0; i < 5; i++) {
    const bw = (26 - i * 3.2) * s;
    const bh = (17 - i) * s;
    parts.push(`M${cx - bw} ${y} h${bw * 2} v${-bh} h${-bw * 2} Z`);
    y -= bh;
    parts.push(roof(cx, y + 3 * s, (46 - i * 5.5) * s, 12 * s));
    y -= 12 * s;
  }
  parts.push(`M${cx - 1.6 * s} ${y + 2} h${3.2 * s} v${-46 * s} h${-3.2 * s} Z`);
  for (let k = 0; k < 6; k++) parts.push(`M${cx - 5 * s} ${y - 8 * s - k * 6 * s} h${10 * s} v${-2 * s} h${-10 * s} Z`);
  return parts.join(" ");
}

function castle(cx: number, base: number, s: number) {
  const parts: string[] = [`M${cx - 120 * s} ${base} L${cx - 92 * s} ${base - 56 * s} L${cx + 92 * s} ${base - 56 * s} L${cx + 120 * s} ${base} Z`];
  let y = base - 56 * s;
  const widths = [78, 64, 52, 40, 30];
  widths.forEach((bw0, i) => {
    const bw = bw0 * s;
    const bh = (24 - i * 2) * s;
    parts.push(`M${cx - bw} ${y} h${bw * 2} v${-bh} h${-bw * 2} Z`);
    y -= bh;
    parts.push(roof(cx, y + 4 * s, bw + 20 * s, 15 * s));
    if (i % 2 === 1) parts.push(`M${cx - 14 * s} ${y - 6 * s} L${cx} ${y - 22 * s} L${cx + 14 * s} ${y - 6 * s} Z`);
    y -= 15 * s;
  });
  parts.push(`M${cx - 16 * s} ${y + 2} L${cx - 10 * s} ${y - 9 * s} L${cx - 4 * s} ${y + 2} Z M${cx + 4 * s} ${y + 2} L${cx + 10 * s} ${y - 9 * s} L${cx + 16 * s} ${y + 2} Z`);
  return parts.join(" ");
}

function torii(cx: number, base: number, s: number) {
  return [
    `M${cx - 44 * s} ${base} h${9 * s} v${-108 * s} h${-9 * s} Z`,
    `M${cx + 35 * s} ${base} h${9 * s} v${-108 * s} h${-9 * s} Z`,
    `M${cx - 56 * s} ${base - 86 * s} h${112 * s} v${-8 * s} h${-112 * s} Z`,
    `M${cx - 58 * s} ${base - 104 * s} h${116 * s} v${-6 * s} h${-116 * s} Z`,
    `M${cx - 4 * s} ${base - 94 * s} h${8 * s} v${-10 * s} h${-8 * s} Z`,
    `M${cx - 74 * s} ${base - 108 * s} Q${cx} ${base - 118 * s} ${cx + 74 * s} ${base - 108 * s} L${cx + 70 * s} ${base - 120 * s} Q${cx} ${base - 132 * s} ${cx - 70 * s} ${base - 120 * s} Z`,
  ].join(" ");
}

function tower(cx: number, base: number, s: number) {
  return [
    `M${cx - 50 * s} ${base} L${cx - 9 * s} ${base - 240 * s} L${cx + 9 * s} ${base - 240 * s} L${cx + 50 * s} ${base} L${cx + 26 * s} ${base} Q${cx} ${base - 70 * s} ${cx - 26 * s} ${base} Z`,
    `M${cx - 27 * s} ${base - 96 * s} h${54 * s} v${-12 * s} h${-54 * s} Z`,
    `M${cx - 14 * s} ${base - 176 * s} h${28 * s} v${-8 * s} h${-28 * s} Z`,
    `M${cx - 2.5 * s} ${base - 238 * s} h${5 * s} v${-80 * s} h${-5 * s} Z`,
  ].join(" ");
}

function fujiPath(cx: number, base: number, w: number, h: number) {
  return `M${cx - w} ${base} C${cx - w * 0.45} ${base - h * 0.35} ${cx - w * 0.2} ${base - h * 0.92} ${cx - w * 0.09} ${base - h} L${cx + w * 0.09} ${base - h} C${cx + w * 0.2} ${base - h * 0.92} ${cx + w * 0.45} ${base - h * 0.35} ${cx + w} ${base} Z`;
}

function trees(seed: number, x0: number, x1: number, base: number, size: number) {
  const r = rng(seed);
  const out: { cx: number; cy: number; r: number }[] = [];
  for (let x = x0; x < x1; x += size * (0.6 + r() * 0.8)) {
    const rr = size * (0.5 + r() * 0.6);
    out.push({ cx: x, cy: base - rr * 0.6, r: rr });
    if (r() > 0.4) out.push({ cx: x + rr * 0.5, cy: base - rr * 1.2, r: rr * 0.7 });
  }
  return out;
}

// --------------------------------------------------------------- component --
type Props = {
  variant: SceneVariant;
  time?: SceneTime;
  /** 0 = dawn … 1 = night; overrides `time` when given. */
  progress?: MotionValue<number>;
  className?: string;
  petals?: boolean;
  parallax?: boolean;
  /** Lower horizon for short banners. */
  compact?: boolean;
};

export function Scene({ variant, time = "dusk", progress, className = "", petals = false, parallax = true, compact = false }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const fixed = useMotionValue(timeValue(time));
  useEffect(() => fixed.set(timeValue(time)), [time, fixed]);
  const p = progress ?? fixed;

  const s1 = useTransform(p, STOPS, P.s1);
  const s2 = useTransform(p, STOPS, P.s2);
  const s3 = useTransform(p, STOPS, P.s3);
  const far = useTransform(p, STOPS, P.far);
  const mid = useTransform(p, STOPS, P.mid);
  const near = useTransform(p, STOPS, P.near);
  const ground = useTransform(p, STOPS, P.ground);
  const sun = useTransform(p, STOPS, P.sun);
  const glow = useTransform(p, STOPS, P.glow);
  const sunY = useTransform(p, STOPS, [64, 20, 60, 18]);
  const sunX = useTransform(p, STOPS, [22, 50, 76, 70]);
  const stars = useTransform(p, [0.5, 0.9], [0, 1]);
  const lights = useTransform(p, [0, 0.33, 0.6, 1], [0.35, 0.08, 0.85, 1]);
  const sky = useMotionTemplate`linear-gradient(180deg, ${s1} 0%, ${s2} 58%, ${s3} 100%)`;
  const sunTop = useMotionTemplate`${sunY}%`;
  const sunLeft = useMotionTemplate`${sunX}%`;
  const halo = useMotionTemplate`radial-gradient(circle, ${glow} 0%, transparent 70%)`;

  const { scrollYProgress } = useScroll({ target: ref, offset: ["start start", "end start"] });
  const k = parallax ? 1 : 0;
  const yFar = useTransform(scrollYProgress, [0, 1], [0, 200 * k]);
  const yMid = useTransform(scrollYProgress, [0, 1], [0, 130 * k]);
  const yNear = useTransform(scrollYProgress, [0, 1], [0, 60 * k]);
  const ySun = useTransform(scrollYProgress, [0, 1], [0, 260 * k]);

  const g = useMemo(() => build(variant), [variant]);
  const starDots = useMemo(() => {
    const r = rng(7);
    return Array.from({ length: 60 }, () => ({ x: r() * 100, y: r() * 55, s: r() * 1.8 + 0.6, d: r() * 4 }));
  }, []);
  const petalDots = useMemo(() => {
    const r = rng(variant.length * 31);
    return Array.from({ length: 14 }, () => ({ left: r() * 110, delay: r() * 12, dur: 9 + r() * 9, size: 6 + r() * 7 }));
  }, [variant]);

  const svg = "absolute inset-x-0 bottom-0 h-full w-full";
  const horizon = compact ? "translate-y-[18%]" : "";

  return (
    <div ref={ref} className={`pointer-events-none absolute inset-0 overflow-hidden ${className}`} aria-hidden>
      <motion.div className="absolute inset-0" style={{ background: sky }} />
      <motion.div className="absolute inset-0" style={{ opacity: stars }}>
        {starDots.map((s, i) => (
          <span
            key={i}
            className="absolute rounded-full bg-white"
            style={{ left: `${s.x}%`, top: `${s.y}%`, width: s.s, height: s.s, animation: `twinkle ${2.5 + s.d}s ease-in-out ${s.d}s infinite` }}
          />
        ))}
      </motion.div>
      <motion.div className="absolute" style={{ top: sunTop, left: sunLeft, y: ySun, x: "-50%" }}>
        <motion.div className="absolute left-1/2 top-1/2 h-[46vmin] w-[46vmin] -translate-x-1/2 -translate-y-1/2 rounded-full" style={{ background: halo }} />
        <motion.div className="h-[9vmin] w-[9vmin] rounded-full" style={{ background: sun, boxShadow: "0 0 60px 10px rgba(255,220,170,.35)" }} />
      </motion.div>
      <div className="absolute inset-x-0 top-[22%] h-24 opacity-40">
        <div className="absolute h-10 w-72 rounded-full bg-white/40 blur-2xl" style={{ animation: "drift 90s linear infinite" }} />
        <div className="absolute top-10 h-8 w-96 rounded-full bg-white/30 blur-2xl" style={{ animation: "drift 140s linear -60s infinite" }} />
      </div>

      <div className={`absolute inset-0 ${horizon}`}>
        <motion.svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="xMidYMax slice" className={svg} style={{ y: yFar }}>
          {g.fuji ? (
            <>
              <defs>
                <clipPath id={`fuji-${variant}`}>
                  <path d={g.fuji} />
                </clipPath>
              </defs>
              <motion.path d={g.fuji} style={{ fill: far }} />
              <g clipPath={`url(#fuji-${variant})`}>
                <path d={g.snow!} fill="#f4f1ff" opacity={0.85} />
              </g>
            </>
          ) : null}
          <motion.path d={g.farRidge} style={{ fill: far }} opacity={g.fuji ? 0.75 : 1} />
        </motion.svg>

        <motion.svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="xMidYMax slice" className={svg} style={{ y: yMid }}>
          <motion.path d={g.midRidge} style={{ fill: mid }} />
          {g.midCity && (
            <>
              {g.midCity.blocks.map((b, i) => (
                <motion.rect key={i} x={b.x} y={b.y} width={b.w} height={b.h} style={{ fill: mid }} />
              ))}
              <motion.g style={{ opacity: lights }}>
                {g.midCity.windows.map((w, i) => (
                  <rect key={i} x={w.x} y={w.y} width={w.w} height={w.h} fill="#ffd38a" opacity={0.7} />
                ))}
              </motion.g>
            </>
          )}
          {g.pagoda && <motion.path d={g.pagoda} style={{ fill: mid }} />}
        </motion.svg>

        <motion.svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="xMidYMax slice" className={svg} style={{ y: yNear }}>
          {g.nearCity && (
            <>
              {g.nearCity.blocks.map((b, i) => (
                <motion.rect key={i} x={b.x} y={b.y} width={b.w} height={b.h} style={{ fill: near }} />
              ))}
              <motion.g style={{ opacity: lights }}>
                {g.nearCity.windows.map((w, i) => (
                  <rect key={i} x={w.x} y={w.y} width={w.w} height={w.h} fill="#ffcf7a" />
                ))}
              </motion.g>
            </>
          )}
          {g.landmark && <motion.path d={g.landmark} style={{ fill: near }} />}
          {g.tower && (
            <>
              <motion.path d={g.tower} style={{ fill: near }} />
              <motion.path d={g.tower} fill="#ff6a3d" style={{ opacity: lights }} />
            </>
          )}
          {g.trees.map((t, i) => (
            <motion.circle key={i} cx={t.cx} cy={t.cy} r={t.r} style={{ fill: near }} />
          ))}
          {g.maples.map((t, i) => (
            <circle key={i} cx={t.cx} cy={t.cy} r={t.r} fill="#b8361f" opacity={0.55} />
          ))}
          <motion.rect x={0} y={H - 34} width={W} height={40} style={{ fill: ground }} />
          {g.torii && <path d={g.torii} fill="#e0432a" />}
          {g.lanterns.map((l, i) => (
            <g key={i} style={{ animation: `lantern ${2 + (i % 3)}s ease-in-out ${i * 0.3}s infinite` }}>
              <circle cx={l.x} cy={l.y} r={18} fill="#ffb347" opacity={0.18} />
              <ellipse cx={l.x} cy={l.y} rx={6} ry={8} fill="#ffb347" />
            </g>
          ))}
        </motion.svg>
      </div>

      {petals && (
        <div className="absolute inset-0">
          {petalDots.map((d, i) => (
            <span
              key={i}
              className="absolute top-0 block rounded-[60%_0_60%_0] bg-sakura/80"
              style={{ left: `${d.left}%`, width: d.size, height: d.size * 0.8, animation: `petal-fall ${d.dur}s linear ${d.delay}s infinite` }}
            />
          ))}
        </div>
      )}
    </div>
  );
}

type Geometry = {
  farRidge: string;
  midRidge: string;
  fuji?: string;
  snow?: string;
  midCity?: ReturnType<typeof skyline>;
  nearCity?: ReturnType<typeof skyline>;
  pagoda?: string;
  landmark?: string;
  tower?: string;
  torii?: string;
  trees: { cx: number; cy: number; r: number }[];
  maples: { cx: number; cy: number; r: number }[];
  lanterns: { x: number; y: number }[];
};

function build(v: SceneVariant): Geometry {
  const base = H - 30;
  switch (v) {
    case "osaka":
      return {
        farRidge: ridge(11, 330, 120, 7),
        midRidge: ridge(12, 470, 40, 10),
        midCity: skyline(13, 0, W, 480, 40, 150),
        nearCity: skyline(14, 860, W, base, 30, 120),
        landmark: castle(470, base - 18, 1.35) + ` M200 ${base} Q470 ${base - 40} 760 ${base} Z`,
        trees: trees(15, 120, 820, base + 4, 34),
        maples: trees(16, 140, 760, base + 2, 20),
        lanterns: [],
      };
    case "kyoto": {
      return {
        farRidge: ridge(21, 360, 150, 6),
        midRidge: ridge(22, 470, 70, 8),
        pagoda: pagoda(1060, 476, 1.9),
        landmark: `M0 ${H} L0 ${base - 6} Q720 ${base - 30} ${W} ${base - 4} L${W} ${H} Z`,
        torii: torii(430, base - 2, 1.6),
        trees: trees(23, 700, W, base, 40),
        maples: trees(24, 0, 300, base, 36).concat(trees(25, 760, 1000, base - 6, 28)),
        lanterns: [
          { x: 620, y: base - 46 },
          { x: 680, y: base - 46 },
          { x: 240, y: base - 46 },
        ],
      };
    }
    case "tokyo":
      return {
        farRidge: ridge(31, 400, 60, 9),
        midRidge: ridge(32, 500, 20, 12),
        midCity: skyline(33, 0, W, 500, 90, 260),
        nearCity: skyline(34, 0, W, base, 30, 110),
        tower: tower(980, base - 8, 1.45),
        trees: trees(35, 0, 420, base + 6, 26),
        maples: [],
        lanterns: [],
      };
    case "fuji": {
      const cx = 760;
      const fuji = fujiPath(cx, 470, 440, 300);
      const snow = `M${cx - 300} 170 L${cx + 300} 170 L${cx + 300} 270 Q${cx + 220} 250 ${cx + 150} 285 Q${cx + 80} 255 ${cx} 290 Q${cx - 80} 255 ${cx - 150} 285 Q${cx - 220} 250 ${cx - 300} 270 Z`;
      return {
        farRidge: ridge(41, 470, 70, 8),
        midRidge: ridge(42, 520, 40, 10),
        fuji,
        snow,
        torii: torii(260, base - 2, 1.1),
        trees: trees(43, 0, 180, base + 4, 30).concat(trees(44, 1100, W, base + 4, 34)),
        maples: trees(45, 1150, 1380, base, 22),
        lanterns: [],
      };
    }
  }
}
