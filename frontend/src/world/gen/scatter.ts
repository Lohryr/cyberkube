// Per-chunk vegetation/prop scatter: Bridson poisson-disc sampling seeded by
// (world seed, chunk coords), then each candidate point rolls against the
// local biome's scatterTable with a density modulated by a slow noise field
// (clearings / thickets). Pure module — THREE instancing lives in props.ts.

import type { PropKind } from "./biomes";
import { createNoise2D, type Noise2D } from "./noise";
import { createRng, type Rng } from "./rng";
import type { WorldGen } from "./worldgen";

export interface ScatterInstance {
  kind: PropKind;
  x: number;
  z: number;
  y: number;
  scale: number;
  /** yaw, radians */
  rotation: number;
}

export interface Point2 {
  x: number;
  y: number;
}

/**
 * poissonDisc (Bridson) fills a [0,width]×[0,height] rectangle with points at
 * least `radius` apart, driven entirely by the provided RNG (deterministic).
 */
export function poissonDisc(
  rng: Rng,
  width: number,
  height: number,
  radius: number,
  k = 20,
): Point2[] {
  const cell = radius / Math.SQRT2;
  const gw = Math.ceil(width / cell);
  const gh = Math.ceil(height / cell);
  const grid: Int32Array = new Int32Array(gw * gh).fill(-1);
  const points: Point2[] = [];
  const active: number[] = [];

  const gridIndex = (p: Point2): number =>
    Math.min(gh - 1, Math.floor(p.y / cell)) * gw + Math.min(gw - 1, Math.floor(p.x / cell));

  const farEnough = (p: Point2): boolean => {
    const gx = Math.min(gw - 1, Math.floor(p.x / cell));
    const gy = Math.min(gh - 1, Math.floor(p.y / cell));
    for (let yy = Math.max(0, gy - 2); yy <= Math.min(gh - 1, gy + 2); yy++) {
      for (let xx = Math.max(0, gx - 2); xx <= Math.min(gw - 1, gx + 2); xx++) {
        const idx = grid[yy * gw + xx];
        if (idx < 0) continue;
        const q = points[idx];
        const dx = q.x - p.x;
        const dy = q.y - p.y;
        if (dx * dx + dy * dy < radius * radius) return false;
      }
    }
    return true;
  };

  const push = (p: Point2): void => {
    grid[gridIndex(p)] = points.length;
    active.push(points.length);
    points.push(p);
  };

  push({ x: rng.range(0, width), y: rng.range(0, height) });

  while (active.length > 0) {
    const slot = rng.int(active.length);
    const origin = points[active[slot]];
    let placed = false;
    for (let i = 0; i < k; i++) {
      const angle = rng.range(0, Math.PI * 2);
      const dist = rng.range(radius, radius * 2);
      const cand = { x: origin.x + Math.cos(angle) * dist, y: origin.y + Math.sin(angle) * dist };
      if (cand.x < 0 || cand.x >= width || cand.y < 0 || cand.y >= height) continue;
      if (!farEnough(cand)) continue;
      push(cand);
      placed = true;
      break;
    }
    if (!placed) {
      active[slot] = active[active.length - 1];
      active.pop();
    }
  }
  return points;
}

/** Minimum spacing between props inside a chunk (world units). */
export const SCATTER_RADIUS = 4.5;

export interface ScatterOptions {
  radius?: number;
  /** slope above which nothing spawns (dHeight per unit) */
  maxSlope?: number;
}

let densityFieldCache: { seed: string; field: Noise2D } | null = null;
function densityField(seed: string): Noise2D {
  if (!densityFieldCache || densityFieldCache.seed !== seed) {
    densityFieldCache = { seed, field: createNoise2D(seed, "scatter:density") };
  }
  return densityFieldCache.field;
}

/**
 * scatterChunk generates the deterministic prop list of one chunk. Seeded by
 * (seed, cx, cz) so a chunk always regrows identically after being unloaded.
 * Pads stay clear, steep slopes stay bare.
 */
export function scatterChunk(
  gen: WorldGen,
  cx: number,
  cz: number,
  chunkSize: number,
  opts: ScatterOptions = {},
): ScatterInstance[] {
  const radius = opts.radius ?? SCATTER_RADIUS;
  const maxSlope = opts.maxSlope ?? 0.9;
  const rng = createRng(gen.seed, `scatter:${cx}:${cz}`);
  const density = densityField(gen.seed);
  const originX = cx * chunkSize;
  const originZ = cz * chunkSize;

  const out: ScatterInstance[] = [];
  for (const p of poissonDisc(rng, chunkSize, chunkSize, radius)) {
    // Draw the rolls unconditionally so the sequence — and therefore every
    // other point in the chunk — never depends on skip conditions.
    const roll = rng.next();
    const scaleRoll = rng.next();
    const rotation = rng.range(0, Math.PI * 2);

    const wx = originX + p.x;
    const wz = originZ + p.y;
    if (gen.onPad(wx, wz, 1)) continue;

    const h = gen.height(wx, wz);
    const slope = Math.abs(gen.height(wx + 1.5, wz) - h) + Math.abs(gen.height(wx, wz + 1.5) - h);
    if (slope / 1.5 > maxSlope) continue;

    const biome = gen.biomeAt(wx, wz);
    // Clearing/thicket modulation in [0.25, 1.15].
    const mod = 0.7 + 0.45 * density(wx * 0.01, wz * 0.01);

    let acc = 0;
    for (const entry of biome.scatterTable) {
      acc += entry.density * mod;
      if (roll < acc) {
        out.push({
          kind: entry.kind,
          x: wx,
          z: wz,
          y: h,
          scale: entry.minScale + (entry.maxScale - entry.minScale) * scaleRoll,
          rotation,
        });
        break;
      }
    }
  }
  return out;
}
