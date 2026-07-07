import { describe, it, expect } from "vitest";
import { BIOMES, classifyBiome } from "./biomes";

describe("biome registry + classification", () => {
  it("registry has at least the six required biomes", () => {
    const ids = BIOMES.map((b) => b.id);
    for (const required of ["plains", "forest", "desert", "mountains", "tundra", "swamp"]) {
      expect(ids).toContain(required);
    }
  });

  it("classification is total over the whole (elevation × moisture) domain", () => {
    for (let e = 0; e <= 20; e++) {
      for (let m = 0; m <= 20; m++) {
        const biome = classifyBiome(e / 20, m / 20);
        expect(BIOMES).toContain(biome);
      }
    }
  });

  it("classification is deterministic", () => {
    expect(classifyBiome(0.4, 0.4).id).toBe(classifyBiome(0.4, 0.4).id);
  });

  it("hits the expected corners of the Whittaker table", () => {
    expect(classifyBiome(0.9, 0.5).id).toBe("mountains");
    expect(classifyBiome(0.1, 0.1).id).toBe("desert");
    expect(classifyBiome(0.05, 0.9).id).toBe("swamp");
    expect(classifyBiome(0.25, 0.45).id).toBe("plains");
    expect(classifyBiome(0.3, 0.8).id).toBe("forest");
    expect(classifyBiome(0.55, 0.3).id).toBe("tundra");
  });

  it("every biome in the registry is reachable by classification", () => {
    const seen = new Set<string>();
    for (let e = 0; e <= 100; e++) {
      for (let m = 0; m <= 100; m++) {
        seen.add(classifyBiome(e / 100, m / 100).id);
      }
    }
    for (const b of BIOMES) expect(seen).toContain(b.id);
  });
});
