import { describe, it, expect } from "vitest";
import {
  placeChallenges,
  districtCenter,
  categoryColor,
  hashString,
  type PlacementInput,
} from "./placement";

const sample: PlacementInput[] = [
  { name: "crypto-messager-legion", category: "Crypto", value: 150 },
  { name: "crypto-operation-blackout", category: "Crypto", value: 200 },
  { name: "web-firesheep", category: "web", value: 100 },
];

describe("deterministic placement", () => {
  it("same challenge → same position across calls", () => {
    const a = placeChallenges(sample);
    const b = placeChallenges(sample);
    for (let i = 0; i < a.length; i++) {
      expect(a[i].x).toBeCloseTo(b[i].x, 10);
      expect(a[i].z).toBeCloseTo(b[i].z, 10);
    }
  });

  it("position is independent of input order", () => {
    const forward = placeChallenges(sample);
    const reversed = placeChallenges([...sample].reverse());
    const byName = (list: ReturnType<typeof placeChallenges>) =>
      Object.fromEntries(list.map((p) => [p.name, p]));
    const f = byName(forward);
    const r = byName(reversed);
    for (const name of Object.keys(f)) {
      expect(f[name].x).toBeCloseTo(r[name].x, 10);
      expect(f[name].z).toBeCloseTo(r[name].z, 10);
    }
  });

  it("same category → same district (challenges cluster together)", () => {
    const placed = placeChallenges(sample);
    const crypto = placed.filter((p) => p.category === "Crypto");
    const cryptoCenter = districtCenter("Crypto");
    for (const c of crypto) {
      const dist = Math.hypot(c.x - cryptoCenter.x, c.z - cryptoCenter.z);
      expect(dist).toBeLessThanOrEqual(25); // within district radius
    }
  });

  it("different categories → different districts", () => {
    const crypto = districtCenter("Crypto");
    const web = districtCenter("web");
    expect(Math.hypot(crypto.x - web.x, crypto.z - web.z)).toBeGreaterThan(1);
  });

  it("higher point value → larger scale", () => {
    const placed = placeChallenges(sample);
    const legion = placed.find((p) => p.name === "crypto-messager-legion")!;
    const blackout = placed.find((p) => p.name === "crypto-operation-blackout")!;
    expect(blackout.scale).toBeGreaterThan(legion.scale); // 200 pts > 150 pts
  });

  it("handles a changing challenge set without error", () => {
    const grown = [...sample, { name: "new-chall", category: "pwn", value: 300 }];
    const placed = placeChallenges(grown);
    expect(placed).toHaveLength(4);
    // existing challenges keep their positions when a new one appears
    const before = placeChallenges(sample).find((p) => p.name === "web-firesheep")!;
    const after = placed.find((p) => p.name === "web-firesheep")!;
    expect(after.x).toBeCloseTo(before.x, 10);
    expect(after.z).toBeCloseTo(before.z, 10);
  });
});

describe("helpers", () => {
  it("hashString is stable", () => {
    expect(hashString("abc")).toBe(hashString("abc"));
    expect(hashString("abc")).not.toBe(hashString("abd"));
  });

  it("categoryColor is stable per category", () => {
    expect(categoryColor("web")).toBe(categoryColor("web"));
  });
});
