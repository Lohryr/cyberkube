// WorldGen composes the whole deterministic pipeline: terrain sampler +
// biome classification + challenge sites, and exposes the FINAL height
// function — base terrain blended flat across each challenge pad, so
// buildings sit on level ground and the terrain "knows" about the sites.

import { classifyBiome, type Biome } from "./biomes";
import { fnv1a } from "./rng";
import { computeSites, type Site, type SiteInput } from "./sites";
import {
  createTerrainSampler,
  DEFAULT_TERRAIN_CONFIG,
  type TerrainConfig,
  type TerrainSampler,
} from "./terrain";

/** Version of the client-side generation algorithm. Bump on breaking changes. */
export const GENERATOR_VERSION = 1;

export interface WorldGen {
  readonly seed: string;
  readonly config: TerrainConfig;
  readonly sites: Site[];
  readonly sampler: TerrainSampler;
  /** final terrain height (pad flattening applied) */
  height(x: number, z: number): number;
  biomeAt(x: number, z: number): Biome;
  /** true when (x,z) lies on/near a challenge pad (used to keep pads clear) */
  onPad(x: number, z: number, extraMargin?: number): boolean;
}

function smoothstep(edge0: number, edge1: number, x: number): number {
  const t = Math.min(1, Math.max(0, (x - edge0) / (edge1 - edge0)));
  return t * t * (3 - 2 * t);
}

/** Pad influence extends past the flat disc so the terrain blends smoothly. */
const PAD_BLEND = 1.9;

export function createWorldGen(
  seed: string,
  challenges: SiteInput[],
  config: TerrainConfig = DEFAULT_TERRAIN_CONFIG,
): WorldGen {
  const sampler = createTerrainSampler(seed, config);
  const sites = computeSites(seed, challenges, sampler);

  const height = (x: number, z: number): number => {
    let h = sampler.baseHeight(x, z);
    // Blend toward each nearby site's pad level. Sites are few (≤ ~100),
    // a linear scan with a cheap bbox reject is fast enough per vertex.
    for (const s of sites) {
      const influence = s.padRadius * PAD_BLEND;
      const dx = x - s.x;
      const dz = z - s.z;
      if (Math.abs(dx) > influence || Math.abs(dz) > influence) continue;
      const d = Math.hypot(dx, dz);
      if (d >= influence) continue;
      const t = smoothstep(s.padRadius, influence, d); // 0 on pad → 1 outside
      h = s.y + (h - s.y) * t;
    }
    return h;
  };

  return {
    seed,
    config,
    sites,
    sampler,
    height,
    biomeAt: (x, z) => classifyBiome(sampler.elevation(x, z), sampler.moisture(x, z)),
    onPad: (x, z, extraMargin = 0) => {
      for (const s of sites) {
        const r = s.padRadius * PAD_BLEND + extraMargin;
        const dx = x - s.x;
        const dz = z - s.z;
        if (Math.abs(dx) > r || Math.abs(dz) > r) continue;
        if (dx * dx + dz * dz < r * r) return true;
      }
      return false;
    },
  };
}

/**
 * Fallback seed when GET /api/v1/world is unavailable (backend not updated
 * yet): hash of the sorted challenge names, so all clients of the same
 * catalog still agree on one world.
 */
export function deriveSeedFromChallenges(names: string[]): string {
  const canonical = [...names].sort().join("\n");
  return `fallback-${fnv1a(canonical).toString(16)}`;
}
