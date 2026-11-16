import { describe, expect, it } from "vitest";
import { applyPatch } from "../src/lib/workspace/patch";

describe("applyPatch", () => {
  it("replaces nested paths, creating parents", () => {
    const s = applyPatch({ a: 1 } as Record<string, unknown>, [{ op: "replace", path: "/effort/planner", value: { effort: "low" } }]);
    expect(s).toEqual({ a: 1, effort: { planner: { effort: "low" } } });
  });
  it("appends with /- and leaves the original untouched", () => {
    const orig = { notices: ["a"] };
    const s = applyPatch(orig, [{ op: "add", path: "/notices/-", value: "b" }]);
    expect(s.notices).toEqual(["a", "b"]);
    expect(orig.notices).toEqual(["a"]);
  });
  it("removes keys and array items", () => {
    const s = applyPatch({ x: { y: 1, z: 2 }, list: [1, 2, 3] }, [
      { op: "remove", path: "/x/y" },
      { op: "remove", path: "/list/1" },
    ]);
    expect(s).toEqual({ x: { z: 2 }, list: [1, 3] });
  });
  it("unescapes ~1 and ~0", () => {
    const s = applyPatch({} as Record<string, unknown>, [{ op: "replace", path: "/a~1b/c~0d", value: 1 }]);
    expect(s).toEqual({ "a/b": { "c~d": 1 } });
  });
});
