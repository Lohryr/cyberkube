import { describe, it, expect } from "vitest";
import { createRng } from "./rng";
import { poissonDisc, scatterChunk } from "./scatter";
import { createWorldGen } from "./worldgen";

const CATALOG = [
  { name: "web-firesheep", category: "web", value: 100 },
  { name: "crypto-legion", category: "crypto", value: 200 },
];

describe("poisson-disc sampling", () => {
  it("no two points closer than the radius", () => {
    const radius = 4.5;
    const pts = poissonDisc(createRng("poisson", "t"), 64, 64, radius);
    expect(pts.length).toBeGreaterThan(20);
    for (let i = 0; i < pts.length; i++) {
      for (let j = i + 1; j < pts.length; j++) {
        const d = Math.hypot(pts[i].x - pts[j].x, pts[i].y - pts[j].y);
        expect(d).toBeGreaterThanOrEqual(radius - 1e-9);
      }
    }
  });

  it("all points stay inside the rectangle", () => {
    for (const p of poissonDisc(createRng("poisson", "bounds"), 50, 30, 3)) {
      expect(p.x).toBeGreaterThanOrEqual(0);
      expect(p.x).toBeLessThan(50);
      expect(p.y).toBeGreaterThanOrEqual(0);
      expect(p.y).toBeLessThan(30);
    }
  });

  it("is deterministic for the same rng seed", () => {
    const a = poissonDisc(createRng("p", "s"), 64, 64, 4);
    const b = poissonDisc(createRng("p", "s"), 64, 64, 4);
    expect(a).toEqual(b);
  });
});

describe("chunk scatter", () => {
  it("same seed + chunk → identical prop list", () => {
    const gen = createWorldGen("scatter-seed", CATALOG);
    const a = scatterChunk(gen, 1, -2, 64);
    const b = scatterChunk(gen, 1, -2, 64);
    expect(a).toEqual(b);
    expect(a.length).toBeGreaterThan(0);
  });

  it("props sit on the terrain and use registered kinds/scales", () => {
    const gen = createWorldGen("scatter-seed", CATALOG);
    for (const inst of scatterChunk(gen, 0, 0, 64)) {
      expect(inst.y).toBeCloseTo(gen.height(inst.x, inst.z), 8);
      expect(inst.scale).toBeGreaterThan(0);
      const biome = gen.biomeAt(inst.x, inst.z);
      expect(biome.scatterTable.map((e) => e.kind)).toContain(inst.kind);
    }
  });

  it("keeps challenge pads clear", () => {
    const gen = createWorldGen("scatter-seed", CATALOG);
    const site = gen.sites[0];
    const cx = Math.floor(site.x / 64);
    const cz = Math.floor(site.z / 64);
    for (const inst of scatterChunk(gen, cx, cz, 64)) {
      const d = Math.hypot(inst.x - site.x, inst.z - site.z);
      expect(d).toBeGreaterThan(site.padRadius);
    }
  });
});
