import { describe, it, expect } from "vitest";
import { PLAYABLE_BOUND, SITE_BOUND } from "../bounds";
import { biomeById } from "./biomes";
import { computeSites, preferredBiome, CATEGORY_BIOMES } from "./sites";
import { createTerrainSampler } from "./terrain";
import { createWorldGen, deriveSeedFromChallenges, GENERATOR_VERSION } from "./worldgen";

const SEED = "ctf-2026";

const CATALOG = [
  { name: "crypto-messager-legion", category: "crypto", value: 150 },
  { name: "crypto-operation-blackout", category: "crypto", value: 200 },
  { name: "web-firesheep", category: "web", value: 100 },
  { name: "forensics-drive", category: "forensics", value: 250 },
  { name: "pwn-heapster", category: "pwn", value: 300 },
];

describe("challenge sites", () => {
  it("same {seed, catalog} → identical sites (snapshot)", () => {
    const sampler = createTerrainSampler(SEED);
    const a = computeSites(SEED, CATALOG, sampler);
    const b = computeSites(SEED, CATALOG, sampler);
    expect(a).toEqual(b);
    expect(
      a.map((s) => ({ ...s, x: s.x.toFixed(4), z: s.z.toFixed(4), y: s.y.toFixed(4) })),
    ).toMatchSnapshot();
  });

  it("adding a challenge does not move existing sites", () => {
    const sampler = createTerrainSampler(SEED);
    const before = computeSites(SEED, CATALOG, sampler);
    const grown = [...CATALOG, { name: "misc-newcomer", category: "misc", value: 50 }];
    const after = computeSites(SEED, grown, sampler);
    expect(after).toHaveLength(CATALOG.length + 1);
    for (const site of before) {
      const same = after.find((s) => s.name === site.name)!;
      expect(same.x).toBe(site.x);
      expect(same.z).toBe(site.z);
      expect(same.y).toBe(site.y);
    }
  });

  it("every site stays inside the invisible wall the rover is clamped to", () => {
    const sampler = createTerrainSampler(SEED);
    // Big catalog to exercise many rejection-sampling paths.
    const catalog = Array.from({ length: 60 }, (_, i) => ({
      name: `chall-${i}`,
      category: ["web", "crypto", "pwn", "reverse", "forensics", "osint"][i % 6],
      value: 50 + i,
    }));
    for (const site of computeSites(SEED, catalog, sampler)) {
      expect(Math.abs(site.x)).toBeLessThanOrEqual(SITE_BOUND);
      expect(Math.abs(site.z)).toBeLessThanOrEqual(SITE_BOUND);
      expect(Math.abs(site.x)).toBeLessThan(PLAYABLE_BOUND);
      expect(Math.abs(site.z)).toBeLessThan(PLAYABLE_BOUND);
    }
  });

  it("scale grows with point value (v1 convention kept)", () => {
    const sampler = createTerrainSampler(SEED);
    const sites = computeSites(SEED, CATALOG, sampler);
    const s150 = sites.find((s) => s.name === "crypto-messager-legion")!;
    const s300 = sites.find((s) => s.name === "pwn-heapster")!;
    expect(s300.scale).toBeGreaterThan(s150.scale);
  });

  it("category → preferred biome registry resolves, including unknowns", () => {
    for (const [cat, biomeId] of Object.entries(CATEGORY_BIOMES)) {
      expect(preferredBiome(cat).id).toBe(biomeId);
      expect(biomeById(biomeId)).toBeDefined();
    }
    const unknown = preferredBiome("some-brand-new-category");
    expect(unknown).toBeDefined();
    expect(preferredBiome("some-brand-new-category").id).toBe(unknown.id); // stable
  });

  it("sites are snapped to terrain and the pad is flattened around them", () => {
    const gen = createWorldGen(SEED, CATALOG);
    for (const site of gen.sites) {
      // Inside the pad radius the final height equals the site height.
      for (const [dx, dz] of [
        [0, 0],
        [site.padRadius * 0.5, 0],
        [0, -site.padRadius * 0.7],
        [-site.padRadius * 0.4, site.padRadius * 0.4],
      ]) {
        expect(gen.height(site.x + dx, site.z + dz)).toBeCloseTo(site.y, 6);
      }
    }
  });

  it("fallback seed derivation is order-independent and stable", () => {
    const names = CATALOG.map((c) => c.name);
    const seed = deriveSeedFromChallenges(names);
    expect(deriveSeedFromChallenges([...names].reverse())).toBe(seed);
    expect(seed.startsWith("fallback-")).toBe(true);
    expect(GENERATOR_VERSION).toBe(1);
  });
});
