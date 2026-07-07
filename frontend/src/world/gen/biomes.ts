// Biome registry + elevation×moisture classification (simplified Whittaker
// table). Adding a biome is a single push into BIOMES: the classifier, the
// chunk colorizer and the scatter pass all read the registry — no central
// switch to edit (spec: "nouveau biome ⇒ aucun changement du pipeline").

export type PropKind = "tree" | "pine" | "rock" | "cactus" | "grass" | "reed";

export interface ScatterEntry {
  kind: PropKind;
  /** probability [0,1] that a poisson-disc candidate point spawns this prop */
  density: number;
  minScale: number;
  maxScale: number;
}

export interface Biome {
  id: string;
  /** classification domain, both in [0,1] */
  elevation: readonly [number, number];
  moisture: readonly [number, number];
  palette: {
    /** terrain vertex color */
    ground: number;
    /** accent used for vegetation tinting / minimap-ish uses */
    vegetation: number;
  };
  scatterTable: ScatterEntry[];
}

/**
 * The registry. Ranges may overlap or leave gaps: classification picks the
 * biome whose domain contains the sample, tie-broken (and gap-filled) by
 * distance to the domain center — so the classifier is total by construction.
 */
export const BIOMES: Biome[] = [
  {
    id: "plains",
    elevation: [0.12, 0.35],
    moisture: [0.3, 0.62],
    palette: { ground: 0x4a7a3f, vegetation: 0x6fae52 },
    scatterTable: [
      { kind: "grass", density: 0.55, minScale: 0.7, maxScale: 1.4 },
      { kind: "tree", density: 0.06, minScale: 0.8, maxScale: 1.3 },
      { kind: "rock", density: 0.04, minScale: 0.5, maxScale: 1.0 },
    ],
  },
  {
    id: "forest",
    elevation: [0.12, 0.5],
    moisture: [0.62, 1.0],
    palette: { ground: 0x2f5d33, vegetation: 0x3f7d3a },
    scatterTable: [
      { kind: "tree", density: 0.5, minScale: 0.9, maxScale: 1.8 },
      { kind: "pine", density: 0.2, minScale: 0.9, maxScale: 1.6 },
      { kind: "grass", density: 0.25, minScale: 0.6, maxScale: 1.2 },
    ],
  },
  {
    id: "desert",
    elevation: [0.0, 0.35],
    moisture: [0.0, 0.3],
    palette: { ground: 0xb99e5e, vegetation: 0x8aa353 },
    scatterTable: [
      { kind: "cactus", density: 0.12, minScale: 0.8, maxScale: 1.6 },
      { kind: "rock", density: 0.1, minScale: 0.5, maxScale: 1.4 },
    ],
  },
  {
    id: "mountains",
    elevation: [0.62, 1.0],
    moisture: [0.0, 1.0],
    palette: { ground: 0x6b6f78, vegetation: 0x7d8289 },
    scatterTable: [
      { kind: "rock", density: 0.3, minScale: 0.8, maxScale: 2.2 },
      { kind: "pine", density: 0.05, minScale: 0.7, maxScale: 1.2 },
    ],
  },
  {
    id: "tundra",
    elevation: [0.5, 0.62],
    moisture: [0.0, 0.55],
    palette: { ground: 0x9aa6a4, vegetation: 0xb7c4bf },
    scatterTable: [
      { kind: "rock", density: 0.15, minScale: 0.6, maxScale: 1.4 },
      { kind: "grass", density: 0.12, minScale: 0.5, maxScale: 0.9 },
    ],
  },
  {
    id: "swamp",
    elevation: [0.0, 0.12],
    moisture: [0.55, 1.0],
    palette: { ground: 0x3d4a35, vegetation: 0x55643f },
    scatterTable: [
      { kind: "reed", density: 0.35, minScale: 0.8, maxScale: 1.6 },
      { kind: "tree", density: 0.1, minScale: 0.7, maxScale: 1.2 },
    ],
  },
  {
    id: "highland-forest",
    elevation: [0.35, 0.62],
    moisture: [0.55, 1.0],
    palette: { ground: 0x3a5c40, vegetation: 0x4e7c4a },
    scatterTable: [
      { kind: "pine", density: 0.4, minScale: 1.0, maxScale: 1.9 },
      { kind: "rock", density: 0.08, minScale: 0.5, maxScale: 1.2 },
    ],
  },
];

export function biomeById(id: string): Biome | undefined {
  return BIOMES.find((b) => b.id === id);
}

function rangeScore(v: number, [lo, hi]: readonly [number, number]): number {
  if (v >= lo && v <= hi) return 0;
  return v < lo ? lo - v : v - hi;
}

/**
 * classifyBiome maps (elevation, moisture) — both in [0,1] — to a biome.
 * Total function: exact-domain match wins; otherwise the biome with the
 * smallest distance to its domain (then to its center) is chosen, so gaps or
 * registry edits can never produce "no biome".
 */
export function classifyBiome(elevation: number, moisture: number): Biome {
  let best: Biome = BIOMES[0];
  let bestScore = Infinity;
  for (const b of BIOMES) {
    const outside = rangeScore(elevation, b.elevation) * 2 + rangeScore(moisture, b.moisture);
    const centerDist =
      Math.abs(elevation - (b.elevation[0] + b.elevation[1]) / 2) +
      Math.abs(moisture - (b.moisture[0] + b.moisture[1]) / 2);
    const score = outside * 10 + centerDist; // domain containment dominates
    if (score < bestScore) {
      bestScore = score;
      best = b;
    }
  }
  return best;
}
