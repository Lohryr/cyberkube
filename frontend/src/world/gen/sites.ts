// Challenge site placement — the v2 evolution of ../placement.ts. Every
// challenge gets a deterministic site: its category picks a preferred biome
// (registry below), its name seeds a bounded rejection search for a spot
// inside that biome, and the terrain later flattens a circular pad around it.
//
// Stability guarantee: a site depends only on (world seed, challenge name,
// category) — never on the rest of the catalog — so adding or removing a
// challenge never moves the others (spec: "placement stable").
//
// Conventions kept from v1 (building.ts): cone marker = dynamic, sphere =
// static, scale ∝ points, gold when solved.

import { classifyBiome, biomeById, BIOMES, type Biome } from "./biomes";
import { createRng, fnv1a } from "./rng";
import type { TerrainSampler } from "./terrain";

export interface SiteInput {
  name: string;
  category: string;
  value: number;
}

export interface Site {
  name: string;
  category: string;
  /** world position, snapped to terrain height */
  x: number;
  z: number;
  y: number;
  /** structure scale factor, ∝ point value (same convention as v1) */
  scale: number;
  /** radius of the flattened circular pad the terrain reserves for the site */
  padRadius: number;
  biomeId: string;
}

/**
 * Category → preferred biome registry. Adding a category is one entry; an
 * unknown category is hashed onto the registry so it still gets a stable,
 * deterministic biome without code changes.
 */
export const CATEGORY_BIOMES: Record<string, string> = {
  web: "plains",
  crypto: "mountains",
  pwn: "desert",
  reverse: "tundra",
  forensics: "swamp",
  osint: "forest",
  misc: "plains",
  stegano: "highland-forest",
};

export function preferredBiome(category: string): Biome {
  const key = (category || "uncategorized").toLowerCase();
  const mapped = CATEGORY_BIOMES[key];
  const biome = mapped ? biomeById(mapped) : undefined;
  if (biome) return biome;
  return BIOMES[fnv1a(key) % BIOMES.length];
}

const MIN_SCALE = 1;
const MAX_SCALE = 3;
const MAX_ATTEMPTS = 64;
/** keep sites away from the world rim where chunks may never load */
const PLACEMENT_MARGIN = 0.85;

/**
 * computeSites places every challenge deterministically. Rejection sampling:
 * draw seeded candidate positions until one lands in the preferred biome
 * (bounded attempts); if none does, fall back to the first candidate — still
 * fully deterministic for that (seed, name).
 */
export function computeSites(
  seed: string,
  challenges: SiteInput[],
  sampler: TerrainSampler,
): Site[] {
  const maxValue = challenges.reduce((m, c) => Math.max(m, c.value), 1);
  const extent = sampler.config.worldExtent * PLACEMENT_MARGIN;

  return challenges.map((c) => {
    const rng = createRng(seed, `site:${c.name}`);
    const target = preferredBiome(c.category);

    let x = 0;
    let z = 0;
    let biome: Biome | null = null;
    for (let attempt = 0; attempt < MAX_ATTEMPTS; attempt++) {
      const cx = rng.range(-extent, extent);
      const cz = rng.range(-extent, extent);
      const b = classifyBiome(sampler.elevation(cx, cz), sampler.moisture(cx, cz));
      if (attempt === 0 || b.id === target.id) {
        x = cx;
        z = cz;
        biome = b;
        if (b.id === target.id) break;
      }
    }

    const value = Math.max(0, c.value);
    const scale = MIN_SCALE + (MAX_SCALE - MIN_SCALE) * (value / maxValue);

    return {
      name: c.name,
      category: c.category,
      x,
      z,
      y: sampler.baseHeight(x, z),
      scale,
      padRadius: 7 + 3 * scale,
      biomeId: (biome ?? target).id,
    };
  });
}
