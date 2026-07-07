// Seeded 2D simplex noise (Gustavson's public-domain reference, adapted to
// TypeScript) plus FBM and domain-warping helpers. No npm dependency: the
// permutation table is shuffled by our own seeded RNG so the field is fully
// deterministic per (seed, stream).

import { createRng } from "./rng";

export type Noise2D = (x: number, y: number) => number; // ≈ [-1, 1]

const GRAD2: ReadonlyArray<readonly [number, number]> = [
  [1, 1], [-1, 1], [1, -1], [-1, -1],
  [1, 0], [-1, 0], [0, 1], [0, -1],
];

const F2 = 0.5 * (Math.sqrt(3) - 1);
const G2 = (3 - Math.sqrt(3)) / 6;

/** createNoise2D returns a seeded simplex noise field in ≈[-1, 1]. */
export function createNoise2D(seed: string, stream = "noise"): Noise2D {
  const rng = createRng(seed, stream);
  const perm = new Uint8Array(512);
  const p = new Uint8Array(256);
  for (let i = 0; i < 256; i++) p[i] = i;
  // Fisher–Yates with the seeded RNG.
  for (let i = 255; i > 0; i--) {
    const j = rng.int(i + 1);
    const tmp = p[i];
    p[i] = p[j];
    p[j] = tmp;
  }
  for (let i = 0; i < 512; i++) perm[i] = p[i & 255];

  return (xin: number, yin: number): number => {
    // Skew input space to determine the simplex cell.
    const s = (xin + yin) * F2;
    const i = Math.floor(xin + s);
    const j = Math.floor(yin + s);
    const t = (i + j) * G2;
    const x0 = xin - (i - t);
    const y0 = yin - (j - t);

    const i1 = x0 > y0 ? 1 : 0;
    const j1 = x0 > y0 ? 0 : 1;

    const x1 = x0 - i1 + G2;
    const y1 = y0 - j1 + G2;
    const x2 = x0 - 1 + 2 * G2;
    const y2 = y0 - 1 + 2 * G2;

    const ii = i & 255;
    const jj = j & 255;

    let n0 = 0, n1 = 0, n2 = 0;

    let t0 = 0.5 - x0 * x0 - y0 * y0;
    if (t0 > 0) {
      t0 *= t0;
      const g = GRAD2[perm[ii + perm[jj]] & 7];
      n0 = t0 * t0 * (g[0] * x0 + g[1] * y0);
    }
    let t1 = 0.5 - x1 * x1 - y1 * y1;
    if (t1 > 0) {
      t1 *= t1;
      const g = GRAD2[perm[ii + i1 + perm[jj + j1]] & 7];
      n1 = t1 * t1 * (g[0] * x1 + g[1] * y1);
    }
    let t2 = 0.5 - x2 * x2 - y2 * y2;
    if (t2 > 0) {
      t2 *= t2;
      const g = GRAD2[perm[ii + 1 + perm[jj + 1]] & 7];
      n2 = t2 * t2 * (g[0] * x2 + g[1] * y2);
    }
    // 70 scales the sum into ≈[-1, 1].
    return 70 * (n0 + n1 + n2);
  };
}

export interface FbmParams {
  octaves: number;
  lacunarity: number;
  gain: number;
  /** base spatial frequency (1/wavelength) */
  frequency: number;
}

/** Fractional Brownian motion over a noise field, normalized to ≈[-1, 1]. */
export function fbm(noise: Noise2D, x: number, y: number, p: FbmParams): number {
  let amplitude = 1;
  let frequency = p.frequency;
  let sum = 0;
  let norm = 0;
  for (let o = 0; o < p.octaves; o++) {
    sum += amplitude * noise(x * frequency, y * frequency);
    norm += amplitude;
    amplitude *= p.gain;
    frequency *= p.lacunarity;
  }
  return norm > 0 ? sum / norm : 0;
}

export interface WarpParams {
  /** displacement amplitude in world units */
  strength: number;
  /** frequency of the warp field */
  frequency: number;
}

/**
 * domainWarp offsets a sample position by two decorrelated noise fields,
 * which turns bland FBM blobs into ridges, valleys and cliff-like shapes.
 */
export function domainWarp(
  warpX: Noise2D,
  warpY: Noise2D,
  x: number,
  y: number,
  p: WarpParams,
): { x: number; y: number } {
  return {
    x: x + p.strength * warpX(x * p.frequency, y * p.frequency),
    y: y + p.strength * warpY(x * p.frequency, y * p.frequency),
  };
}
