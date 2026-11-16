// Minimal immutable RFC 6902 JSON Patch (add, replace, remove) over RFC 6901
// pointers. Missing intermediate objects are created, so a server patch to
// "/effort/planner" works on a fresh state.
import type { PatchOp } from "@/lib/api/client";

type Json = unknown;

function unescape(seg: string): string {
  return seg.replace(/~1/g, "/").replace(/~0/g, "~");
}

function setIn(node: Json, segs: string[], op: PatchOp["op"], value: Json): Json {
  const [head, ...rest] = segs;
  if (head === undefined) return value;
  if (Array.isArray(node)) {
    const arr = node.slice();
    if (rest.length === 0) {
      if (head === "-") {
        if (op !== "remove") arr.push(value);
        return arr;
      }
      const i = Number(head);
      if (op === "remove") arr.splice(i, 1);
      else if (op === "add") arr.splice(i, 0, value);
      else arr[i] = value;
      return arr;
    }
    const i = Number(head);
    arr[i] = setIn(arr[i], rest, op, value);
    return arr;
  }
  const obj: Record<string, Json> = node && typeof node === "object" ? { ...(node as Record<string, Json>) } : {};
  if (rest.length === 0) {
    if (op === "remove") delete obj[head];
    else obj[head] = value;
    return obj;
  }
  obj[head] = setIn(obj[head], rest, op, value);
  return obj;
}

export function applyPatch<T>(state: T, ops: PatchOp[]): T {
  let next: Json = state;
  for (const op of ops) {
    const segs = op.path.split("/").slice(1).map(unescape);
    next = setIn(next, segs, op.op, op.value);
  }
  return next as T;
}
