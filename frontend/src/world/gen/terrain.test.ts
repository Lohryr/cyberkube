import { describe, it, expect } from "vitest";
import { createNoise2D, fbm } from "./noise";
import { createTerrainSampler } from "./terrain";

const PROBES: ReadonlyArray<readonly [number, number]> = [
  [0, 0],
  [10.5, -3.25],
  [123, 456],
  [-250, 320],
  [399, -399],
];

describe("noise", () => {
  it("simplex output is bounded and seeded-deterministic", () => {
    const a = createNoise2D("seed-x");
    const b = createNoise2D("seed-x");
    for (let i = 0; i < 500; i++) {
      const x = (i * 7.13) % 100;
      const y = (i * 3.71) % 100;
      const v = a(x, y);
      expect(v).toBe(b(x, y));
      expect(Math.abs(v)).toBeLessThanOrEqual(1.05);
    }
  });

  it("fbm is deterministic and bounded", () => {
    const n = createNoise2D("seed-fbm");
    const p = { octaves: 5, lacunarity: 2, gain: 0.5, frequency: 0.01 };
    for (const [x, y] of PROBES) {
      const v = fbm(n, x, y, p);
      expect(v).toBe(fbm(n, x, y, p));
      expect(Math.abs(v)).toBeLessThanOrEqual(1.05);
    }
  });
});

describe("terrain sampler", () => {
  it("same seed → same heights at fixed points (snapshot)", () => {
    const t = createTerrainSampler("ctf-2026");
    const heights = PROBES.map(([x, z]) => t.baseHeight(x, z).toFixed(6));
    expect(heights).toMatchSnapshot();
    // and a fresh sampler agrees exactly
    const t2 = createTerrainSampler("ctf-2026");
    for (const [x, z] of PROBES) expect(t2.baseHeight(x, z)).toBe(t.baseHeight(x, z));
  });

  it("different seeds → different terrain", () => {
    const a = createTerrainSampler("seed-a");
    const b = createTerrainSampler("seed-b");
    const differs = PROBES.some(([x, z]) => a.baseHeight(x, z) !== b.baseHeight(x, z));
    expect(differs).toBe(true);
  });

  it("height stays within [0, heightScale] and moisture within [0, 1]", () => {
    const t = createTerrainSampler("bounds");
    for (let i = 0; i < 400; i++) {
      const x = -400 + (800 * i) / 399;
      const z = ((i * 37) % 800) - 400;
      const h = t.baseHeight(x, z);
      expect(h).toBeGreaterThanOrEqual(0);
      expect(h).toBeLessThanOrEqual(t.config.heightScale);
      const m = t.moisture(x, z);
      expect(m).toBeGreaterThanOrEqual(0);
      expect(m).toBeLessThanOrEqual(1);
    }
  });
});
