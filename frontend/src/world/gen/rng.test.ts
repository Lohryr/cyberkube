import { describe, it, expect } from "vitest";
import { createRng, fnv1a } from "./rng";

describe("seeded rng (splitmix64 → xoshiro128**)", () => {
  it("same (seed, stream) → same sequence", () => {
    const a = createRng("world-1", "terrain");
    const b = createRng("world-1", "terrain");
    for (let i = 0; i < 100; i++) expect(a.next()).toBe(b.next());
  });

  it("different seeds → different sequences", () => {
    const a = createRng("world-1");
    const b = createRng("world-2");
    const seqA = Array.from({ length: 8 }, () => a.next());
    const seqB = Array.from({ length: 8 }, () => b.next());
    expect(seqA).not.toEqual(seqB);
  });

  it("different streams of the same seed are decorrelated", () => {
    const a = createRng("world-1", "terrain");
    const b = createRng("world-1", "moisture");
    const seqA = Array.from({ length: 8 }, () => a.next());
    const seqB = Array.from({ length: 8 }, () => b.next());
    expect(seqA).not.toEqual(seqB);
  });

  it("next() stays in [0, 1) and looks roughly uniform", () => {
    const rng = createRng("uniformity");
    let sum = 0;
    const n = 10_000;
    for (let i = 0; i < n; i++) {
      const v = rng.next();
      expect(v).toBeGreaterThanOrEqual(0);
      expect(v).toBeLessThan(1);
      sum += v;
    }
    expect(sum / n).toBeGreaterThan(0.47);
    expect(sum / n).toBeLessThan(0.53);
  });

  it("int(n) stays in range", () => {
    const rng = createRng("ints");
    for (let i = 0; i < 1000; i++) {
      const v = rng.int(7);
      expect(v).toBeGreaterThanOrEqual(0);
      expect(v).toBeLessThan(7);
    }
  });

  it("fnv1a is stable", () => {
    expect(fnv1a("abc")).toBe(fnv1a("abc"));
    expect(fnv1a("abc")).not.toBe(fnv1a("abd"));
  });
});
