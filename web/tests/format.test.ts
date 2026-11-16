import { describe, expect, it } from "vitest";
import { money, rupiah } from "../src/lib/format";

describe("format", () => {
  it("formats rupiah without decimals", () => {
    expect(rupiah(17850000).replace(/\s/g, " ")).toMatch(/^Rp\s?17\.850\.000$/);
  });
  it("formats yen and dollars from minor units", () => {
    expect(money({ amount: 1200, currency: "JPY" })).toContain("1.200");
    expect(money({ amount: 150000, currency: "USD" })).toContain("1.500");
  });
  it("handles missing values", () => {
    expect(money(null)).toBe("—");
  });
});

describe("format in English", () => {
  it("follows the UI locale", async () => {
    const { setFormatLocale } = await import("../src/lib/format");
    setFormatLocale("en-US");
    try {
      expect(rupiah(17850000).replace(/\s/g, " ")).toMatch(/^Rp\s?17,850,000$/);
      expect(money({ amount: 1200, currency: "JPY" })).toBe("¥1,200");
    } finally {
      setFormatLocale("id-ID");
    }
  });
});
