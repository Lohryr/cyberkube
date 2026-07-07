// Continuous heightmap + moisture field for the procedural world. Pure
// functions of (seed, config, x, z): no state, no Math.random, so any client
// with the same seed samples the exact same terrain at any coordinate.
//
// Shape recipe: domain-warped simplex FBM, then a redistribution curve that
// widens habitable plains (flatten a band around the plains level) while
// keeping mountains steep and valleys marked.

import { createNoise2D, fbm, domainWarp, type FbmParams, type WarpParams } from "./noise";

export interface TerrainConfig {
  /** world half-size: terrain is meaningful in [-worldExtent, +worldExtent] */
  worldExtent: number;
  /** height (world units) of the tallest peaks */
  heightScale: number;
  height: FbmParams;
  warp: WarpParams;
  moisture: FbmParams;
  /**
   * Redistribution: exponent > 1 flattens lowlands and sharpens peaks;
   * the plains band [plainsCenter ± plainsWidth] is pulled toward
   * plainsCenter^exponent by plainsFlatten to create wide buildable flats.
   */
  redistribution: {
    exponent: number;
    plainsCenter: number;
    plainsWidth: number;
    plainsFlatten: number;
  };
}

export const DEFAULT_TERRAIN_CONFIG: TerrainConfig = {
  worldExtent: 400,
  heightScale: 55,
  height: { octaves: 5, lacunarity: 2.0, gain: 0.5, frequency: 1 / 260 },
  warp: { strength: 60, frequency: 1 / 320 },
  moisture: { octaves: 3, lacunarity: 2.1, gain: 0.55, frequency: 1 / 340 },
  redistribution: { exponent: 2.6, plainsCenter: 0.45, plainsWidth: 0.16, plainsFlatten: 0.75 },
};

export interface TerrainSampler {
  readonly config: TerrainConfig;
  /** raw terrain height in world units (no challenge-pad flattening) */
  baseHeight(x: number, z: number): number;
  /** normalized elevation in [0, 1] (0 = valley floor, 1 = peak) */
  elevation(x: number, z: number): number;
  /** moisture in [0, 1] (dry → wet), second field for biome classification */
  moisture(x: number, z: number): number;
}

function smoothstep(edge0: number, edge1: number, x: number): number {
  const t = Math.min(1, Math.max(0, (x - edge0) / (edge1 - edge0)));
  return t * t * (3 - 2 * t);
}

/** createTerrainSampler builds the deterministic height/moisture fields. */
export function createTerrainSampler(
  seed: string,
  config: TerrainConfig = DEFAULT_TERRAIN_CONFIG,
): TerrainSampler {
  const heightNoise = createNoise2D(seed, "terrain:height");
  const warpXNoise = createNoise2D(seed, "terrain:warp-x");
  const warpZNoise = createNoise2D(seed, "terrain:warp-z");
  const moistureNoise = createNoise2D(seed, "terrain:moisture");

  const { redistribution: rd } = config;
  const plainsLevel = Math.pow(rd.plainsCenter, rd.exponent);

  const elevation = (x: number, z: number): number => {
    const w = domainWarp(warpXNoise, warpZNoise, x, z, config.warp);
    const n = fbm(heightNoise, w.x, w.y, config.height); // ≈[-1, 1]
    const e = Math.min(1, Math.max(0, 0.5 + 0.5 * n));

    // Redistribution: flatten lows / sharpen highs…
    let shaped = Math.pow(e, rd.exponent);
    // …then widen the habitable plains band around plainsCenter.
    const band =
      1 -
      smoothstep(0, rd.plainsWidth, Math.abs(e - rd.plainsCenter)); // 1 at center, 0 outside band
    shaped = shaped + (plainsLevel - shaped) * band * rd.plainsFlatten;
    return shaped;
  };

  return {
    config,
    elevation,
    baseHeight: (x, z) => elevation(x, z) * config.heightScale,
    moisture: (x, z) => {
      const m = fbm(moistureNoise, x, z, config.moisture);
      return Math.min(1, Math.max(0, 0.5 + 0.5 * m));
    },
  };
}
