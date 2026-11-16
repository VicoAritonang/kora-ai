// Reads a run's NDJSON event stream (application/x-ndjson) with fetch and
// a ReadableStream. On a dropped connection it resumes from the last seq
// (?after=N), so no event is lost or applied twice (NFR-13).
import { api, type StreamLine } from "@/lib/api/client";

export type Follow = { stop: () => void };

export function followRun(
  runId: string,
  onLine: (line: StreamLine) => void,
  opts: { after?: number; onReconnect?: (attempt: number) => void; onEnd?: () => void } = {},
): Follow {
  let after = opts.after ?? 0;
  let stopped = false;
  let controller: AbortController | null = null;
  let attempt = 0;

  const run = async () => {
    while (!stopped) {
      controller = new AbortController();
      let terminal = false;
      try {
        const res = await fetch(api.eventsUrl(runId, after), {
          credentials: "include",
          headers: { Accept: "application/x-ndjson" },
          signal: controller.signal,
        });
        if (!res.ok || !res.body) throw new Error(`HTTP ${res.status}`);
        attempt = 0;
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buf = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += decoder.decode(value, { stream: true });
          let nl: number;
          while ((nl = buf.indexOf("\n")) >= 0) {
            const raw = buf.slice(0, nl).trim();
            buf = buf.slice(nl + 1);
            if (!raw) continue;
            let line: StreamLine;
            try {
              line = JSON.parse(raw) as StreamLine;
            } catch {
              continue;
            }
            if (line.type === "ping") continue;
            if (typeof line.seq === "number") {
              if (line.seq <= after) continue; // already applied
              after = line.seq;
            }
            onLine(line);
            if (line.type === "run.completed" || line.type === "run.failed") terminal = true;
          }
        }
      } catch {
        if (stopped) return;
      }
      if (terminal || stopped) {
        opts.onEnd?.();
        return;
      }
      attempt++;
      opts.onReconnect?.(attempt);
      await new Promise((r) => setTimeout(r, Math.min(8000, 500 * 2 ** attempt)));
    }
  };
  void run();
  return {
    stop: () => {
      stopped = true;
      controller?.abort();
    },
  };
}
