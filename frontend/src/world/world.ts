import * as THREE from "three";
import type { Challenge } from "../api";
import { createBuilding, createSitePad, type Building } from "./building";
import { ChunkManager } from "./gen/chunks";
import { createWorldGen, GENERATOR_VERSION, type WorldGen } from "./gen/worldgen";

// World owns the scene graph derived from the live challenge catalog and the
// server world descriptor. rebuild() re-runs the deterministic generation
// pipeline (terrain, biomes, sites) — new challenges appear, removed ones
// vanish, with no per-challenge authored data. Terrain streams in chunks via
// update(x, z) as the player moves.

export interface WorldDescriptor {
  seed: string;
  generatorVersion: number;
}

export class World {
  readonly scene = new THREE.Scene();
  private buildingsGroup = new THREE.Group();
  private buildings: Building[] = [];
  private chunkManager: ChunkManager | null = null;
  private gen: WorldGen | null = null;

  constructor() {
    this.scene.background = new THREE.Color(0x0b0e16);
    this.scene.fog = new THREE.Fog(0x0b0e16, 120, 320);

    const hemi = new THREE.HemisphereLight(0xbfd4ff, 0x20242e, 0.9);
    this.scene.add(hemi);

    const sun = new THREE.DirectionalLight(0xffffff, 1.1);
    sun.position.set(60, 120, 40);
    sun.castShadow = true;
    sun.shadow.mapSize.set(2048, 2048);
    sun.shadow.camera.left = -200;
    sun.shadow.camera.right = 200;
    sun.shadow.camera.top = 200;
    sun.shadow.camera.bottom = -200;
    this.scene.add(sun);

    this.scene.add(this.buildingsGroup);
  }

  /** Rebuild the world from the current challenge list + world descriptor. */
  rebuild(challenges: Challenge[], descriptor: WorldDescriptor): void {
    if (descriptor.generatorVersion > GENERATOR_VERSION) {
      console.warn(
        `world generatorVersion ${descriptor.generatorVersion} is newer than this client ` +
          `(v${GENERATOR_VERSION}) — rendering with the v${GENERATOR_VERSION} pipeline; ` +
          `the world may differ from up-to-date clients.`,
      );
    }

    this.disposeGroup(this.buildingsGroup);
    this.buildings = [];
    if (this.chunkManager) {
      this.scene.remove(this.chunkManager.group);
      this.chunkManager.dispose();
    }

    this.gen = createWorldGen(
      descriptor.seed,
      challenges.map((c) => ({ name: c.name, category: c.category, value: c.value })),
    );
    this.chunkManager = new ChunkManager(this.gen);
    this.scene.add(this.chunkManager.group);

    const siteByName = new Map(this.gen.sites.map((s) => [s.name, s]));
    for (const challenge of challenges) {
      const site = siteByName.get(challenge.name);
      if (!site) continue;

      this.buildingsGroup.add(
        createSitePad(site.x, site.y, site.z, challenge.category, site.padRadius),
      );

      const building = createBuilding(challenge, {
        name: site.name,
        category: site.category,
        x: site.x,
        z: site.z,
        scale: site.scale,
      });
      building.group.position.y = site.y;
      this.buildings.push(building);
      this.buildingsGroup.add(building.group);
    }
  }

  /** Stream terrain chunks around the focus position (call every frame). */
  update(x: number, z: number): void {
    this.chunkManager?.update(x, z);
  }

  /** Final terrain height (pads included) — the ground the player drives on. */
  heightAt(x: number, z: number): number {
    return this.gen ? this.gen.height(x, z) : 0;
  }

  /** Nearest building within `radius` of the given ground position, or null. */
  nearest(x: number, z: number, radius: number): Building | null {
    let best: Building | null = null;
    let bestDist = radius;
    for (const b of this.buildings) {
      const d = Math.hypot(b.position.x - x, b.position.y - z);
      if (d < bestDist) {
        bestDist = d;
        best = b;
      }
    }
    return best;
  }

  private disposeGroup(group: THREE.Group): void {
    group.traverse((obj) => {
      if (obj instanceof THREE.Mesh) {
        obj.geometry.dispose();
        const mat = obj.material;
        if (Array.isArray(mat)) mat.forEach((m) => m.dispose());
        else mat.dispose();
      }
    });
    group.clear();
  }
}
