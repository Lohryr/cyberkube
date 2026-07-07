// Chunked terrain streaming. The world is a grid of 64-unit chunks, each a
// displaced PlaneGeometry with per-vertex biome colors. Chunks are generated
// lazily around the player (few per frame to avoid main-thread stalls),
// LOD'd by distance (33×33 vertices near, 17×17 far), unloaded out of range
// and kept in a small LRU so a U-turn doesn't pay regeneration.

import * as THREE from "three";
import { buildScatterMeshes } from "./props";
import { scatterChunk } from "./scatter";
import type { WorldGen } from "./worldgen";

export const CHUNK_SIZE = 64;
/** grid segments per side: high LOD = 33×33 vertices, low = 17×17 */
const SEGMENTS_HIGH = 32;
const SEGMENTS_LOW = 16;

export interface ChunkManagerOptions {
  /** chunks kept loaded around the focus (Chebyshev distance) */
  viewRadius?: number;
  /** chunks rendered at high resolution */
  highLodRadius?: number;
  /** chunks that get vegetation/props */
  scatterRadius?: number;
  /** unloaded chunks kept warm in the LRU cache */
  cacheSize?: number;
  /** max chunks built per update() call (main-thread budget) */
  buildBudget?: number;
}

interface Chunk {
  key: string;
  group: THREE.Group;
  lod: number; // 0 = high, 1 = low
}

const TERRAIN_MATERIAL = new THREE.MeshStandardMaterial({
  vertexColors: true,
  roughness: 1,
  metalness: 0,
  flatShading: true,
});

function buildTerrainMesh(gen: WorldGen, cx: number, cz: number, segments: number): THREE.Mesh {
  const geo = new THREE.PlaneGeometry(CHUNK_SIZE, CHUNK_SIZE, segments, segments);
  geo.rotateX(-Math.PI / 2);
  // Corner convention: chunk (cx, cz) covers [cx·S, (cx+1)·S] — matches
  // scatterChunk and Math.floor(x / CHUNK_SIZE) lookups.
  geo.translate(CHUNK_SIZE / 2, 0, CHUNK_SIZE / 2);

  const originX = cx * CHUNK_SIZE;
  const originZ = cz * CHUNK_SIZE;
  const pos = geo.getAttribute("position") as THREE.BufferAttribute;
  const colors = new Float32Array(pos.count * 3);
  const color = new THREE.Color();

  for (let i = 0; i < pos.count; i++) {
    const wx = originX + pos.getX(i);
    const wz = originZ + pos.getZ(i);
    pos.setY(i, gen.height(wx, wz));

    const biome = gen.biomeAt(wx, wz);
    // Slight elevation shading keeps the relief readable without textures.
    const shade = 0.82 + 0.36 * gen.sampler.elevation(wx, wz);
    color.setHex(biome.palette.ground).multiplyScalar(shade);
    colors[i * 3] = color.r;
    colors[i * 3 + 1] = color.g;
    colors[i * 3 + 2] = color.b;
  }
  geo.setAttribute("color", new THREE.BufferAttribute(colors, 3));
  geo.computeVertexNormals();

  const mesh = new THREE.Mesh(geo, TERRAIN_MATERIAL);
  mesh.position.set(originX, 0, originZ);
  mesh.receiveShadow = true;
  return mesh;
}

export class ChunkManager {
  readonly group = new THREE.Group();

  private readonly loaded = new Map<string, Chunk>();
  private readonly lru = new Map<string, Chunk>(); // insertion order = age
  private readonly viewRadius: number;
  private readonly highLodRadius: number;
  private readonly scatterRadius: number;
  private readonly cacheSize: number;
  private readonly buildBudget: number;

  constructor(private readonly gen: WorldGen, opts: ChunkManagerOptions = {}) {
    this.viewRadius = opts.viewRadius ?? 3;
    this.highLodRadius = opts.highLodRadius ?? 1;
    this.scatterRadius = opts.scatterRadius ?? 2;
    this.cacheSize = opts.cacheSize ?? 24;
    this.buildBudget = opts.buildBudget ?? 2;
  }

  /** Number of chunks currently in the scene (exposed for tests/debug). */
  get loadedCount(): number {
    return this.loaded.size;
  }

  /**
   * update streams chunks around the focus position (player). Call it every
   * frame or on movement; it does bounded work per call.
   */
  update(x: number, z: number): void {
    const ccx = Math.floor(x / CHUNK_SIZE);
    const ccz = Math.floor(z / CHUNK_SIZE);
    const maxChunk = Math.ceil(this.gen.config.worldExtent / CHUNK_SIZE);

    // Unload chunks that fell out of range (move them to the LRU).
    for (const [key, chunk] of this.loaded) {
      const [cx, cz] = key.split(",").map(Number);
      if (Math.max(Math.abs(cx - ccx), Math.abs(cz - ccz)) > this.viewRadius) {
        this.loaded.delete(key);
        this.group.remove(chunk.group);
        this.cachePut(chunk);
      }
    }

    // Load missing chunks, nearest first, within the per-call budget.
    let built = 0;
    for (let r = 0; r <= this.viewRadius && built < this.buildBudget; r++) {
      for (let cx = ccx - r; cx <= ccx + r && built < this.buildBudget; cx++) {
        for (let cz = ccz - r; cz <= ccz + r && built < this.buildBudget; cz++) {
          if (Math.max(Math.abs(cx - ccx), Math.abs(cz - ccz)) !== r) continue;
          if (Math.abs(cx) > maxChunk || Math.abs(cz) > maxChunk) continue;
          const lod = r <= this.highLodRadius ? 0 : 1;
          const key = `${cx},${cz}`;
          const existing = this.loaded.get(key);
          if (existing) {
            if (existing.lod === lod) continue;
            // LOD switch: drop and rebuild at the right resolution.
            this.loaded.delete(key);
            this.group.remove(existing.group);
            this.cachePut(existing);
          }
          const chunk = this.cacheTake(key, lod) ?? this.buildChunk(cx, cz, lod, r);
          this.loaded.set(key, chunk);
          this.group.add(chunk.group);
          built++;
        }
      }
    }
  }

  dispose(): void {
    for (const chunk of this.loaded.values()) this.disposeChunk(chunk);
    for (const chunk of this.lru.values()) this.disposeChunk(chunk);
    this.loaded.clear();
    this.lru.clear();
    this.group.clear();
  }

  private buildChunk(cx: number, cz: number, lod: number, ring: number): Chunk {
    const group = new THREE.Group();
    group.add(buildTerrainMesh(this.gen, cx, cz, lod === 0 ? SEGMENTS_HIGH : SEGMENTS_LOW));
    if (ring <= this.scatterRadius) {
      for (const mesh of buildScatterMeshes(scatterChunk(this.gen, cx, cz, CHUNK_SIZE))) {
        group.add(mesh);
      }
    }
    return { key: `${cx},${cz}`, group, lod };
  }

  private cachePut(chunk: Chunk): void {
    const cacheKey = `${chunk.key}:${chunk.lod}`;
    if (this.lru.has(cacheKey)) this.lru.delete(cacheKey);
    this.lru.set(cacheKey, chunk);
    while (this.lru.size > this.cacheSize) {
      const oldest = this.lru.keys().next().value as string;
      const evicted = this.lru.get(oldest);
      this.lru.delete(oldest);
      if (evicted) this.disposeChunk(evicted);
    }
  }

  private cacheTake(key: string, lod: number): Chunk | null {
    const cacheKey = `${key}:${lod}`;
    const chunk = this.lru.get(cacheKey);
    if (!chunk) return null;
    this.lru.delete(cacheKey);
    return chunk;
  }

  private disposeChunk(chunk: Chunk): void {
    chunk.group.traverse((obj) => {
      // Terrain geometry is per-chunk → dispose. Prop geometry/materials are
      // shared module-wide (props.ts) → InstancedMesh.dispose() only frees
      // the instance buffers.
      if (obj instanceof THREE.InstancedMesh) obj.dispose();
      else if (obj instanceof THREE.Mesh) obj.geometry.dispose();
    });
    chunk.group.clear();
  }
}
