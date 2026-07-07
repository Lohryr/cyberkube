// Deterministic world layout. Every challenge's position and scale is derived
// purely from its own metadata — no randomness, no persisted layout. The same
// challenge always lands in the same spot across reloads (spec: "same
// challenge, same position across reloads"), and challenges sharing a category
// cluster into the same district (spec: "category determines district").
//
// This module is pure and framework-free so it can be unit-tested.

export interface PlacedChallenge {
  name: string;
  category: string;
  /** world X */
  x: number;
  /** world Z (ground plane) */
  z: number;
  /** building scale factor, derived from point value */
  scale: number;
}

export interface PlacementInput {
  name: string;
  category: string;
  value: number;
}

/** FNV-1a 32-bit hash — small, stable, dependency-free. */
export function hashString(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

/** Deterministic [0,1) PRNG (mulberry32) seeded by a 32-bit integer. */
export function seededRandom(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const DISTRICT_SPACING = 60;
const DISTRICT_RADIUS = 22;
const MIN_SCALE = 1;
const MAX_SCALE = 3;

/**
 * districtCenter maps a category name to a stable world position on a ring, so
 * all challenges of that category share a neighborhood. The ring index comes
 * from the category hash, keeping placement independent of iteration order.
 */
export function districtCenter(category: string): { x: number; z: number } {
  const h = hashString(category || "uncategorized");
  // Spread categories around a circle; radius grows slowly so many categories
  // still fit without overlap.
  const angle = (h % 3600) / 3600 * Math.PI * 2;
  const ringSlot = h % 8; // up to 8 rings before repeating radius
  const radius = DISTRICT_SPACING * (1 + Math.floor(ringSlot / 4));
  return { x: Math.cos(angle) * radius, z: Math.sin(angle) * radius };
}

/**
 * placeChallenges assigns each challenge a stable position inside its category
 * district (spiral packing seeded by the challenge name so buildings don't
 * overlap and order does not matter) and a scale from its point value.
 */
export function placeChallenges(challenges: PlacementInput[]): PlacedChallenge[] {
  const maxValue = challenges.reduce((m, c) => Math.max(m, c.value), 1);

  return challenges.map((c) => {
    const center = districtCenter(c.category);
    const rng = seededRandom(hashString(c.name));

    // Spiral offset within the district: angle + radius both seeded, so the
    // building sits at a stable, category-local spot.
    const angle = rng() * Math.PI * 2;
    const radius = Math.sqrt(rng()) * DISTRICT_RADIUS;

    const value = Math.max(0, c.value);
    const scale = MIN_SCALE + (MAX_SCALE - MIN_SCALE) * (value / maxValue);

    return {
      name: c.name,
      category: c.category,
      x: center.x + Math.cos(angle) * radius,
      z: center.z + Math.sin(angle) * radius,
      scale,
    };
  });
}

const DISTRICT_PALETTE = [
  0x4f8dfd, 0x2fbf71, 0xf2b134, 0xe4572e, 0x9b5de5, 0x00bbf9, 0xf15bb5, 0x8ac926,
];

/** categoryColor gives each category a stable, distinct color. */
export function categoryColor(category: string): number {
  return DISTRICT_PALETTE[hashString(category || "uncategorized") % DISTRICT_PALETTE.length];
}
