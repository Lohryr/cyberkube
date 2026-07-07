// Low-poly prop meshes (registry) + InstancedMesh assembly. One merged
// vertex-colored geometry per prop kind, one shared material, one
// InstancedMesh per (chunk, kind) — the whole vegetation of a chunk costs a
// handful of draw calls. No external textures, consistent with the flat-shaded
// look of the rest of the world.

import * as THREE from "three";
import { mergeGeometries } from "three/examples/jsm/utils/BufferGeometryUtils.js";
import type { PropKind } from "./biomes";
import type { ScatterInstance } from "./scatter";

/** Paint a whole geometry with one vertex color (so kinds can be merged). */
function colored(geo: THREE.BufferGeometry, color: number): THREE.BufferGeometry {
  const c = new THREE.Color(color);
  const count = geo.getAttribute("position").count;
  const colors = new Float32Array(count * 3);
  for (let i = 0; i < count; i++) {
    colors[i * 3] = c.r;
    colors[i * 3 + 1] = c.g;
    colors[i * 3 + 2] = c.b;
  }
  geo.setAttribute("color", new THREE.BufferAttribute(colors, 3));
  return geo;
}

function merged(parts: THREE.BufferGeometry[]): THREE.BufferGeometry {
  const geo = mergeGeometries(parts, false);
  parts.forEach((p) => p.dispose());
  return geo;
}

/**
 * Prop registry: kind → geometry factory. Adding a prop kind = one entry
 * here + referencing it from a biome scatterTable.
 */
export const PROP_FACTORIES: Record<PropKind, () => THREE.BufferGeometry> = {
  tree: () =>
    merged([
      colored(new THREE.CylinderGeometry(0.22, 0.32, 1.6, 6).translate(0, 0.8, 0), 0x6d4a2f),
      colored(new THREE.ConeGeometry(1.5, 3.2, 7).translate(0, 3.0, 0), 0x3f7d3a),
    ]),
  pine: () =>
    merged([
      colored(new THREE.CylinderGeometry(0.18, 0.26, 1.2, 6).translate(0, 0.6, 0), 0x5d3f28),
      colored(new THREE.ConeGeometry(1.4, 2.2, 7).translate(0, 2.0, 0), 0x2c5a33),
      colored(new THREE.ConeGeometry(1.0, 1.8, 7).translate(0, 3.3, 0), 0x336b3b),
    ]),
  rock: () => merged([colored(new THREE.IcosahedronGeometry(0.9, 0).translate(0, 0.45, 0), 0x7c7f88)]),
  cactus: () =>
    merged([
      colored(new THREE.CylinderGeometry(0.35, 0.4, 2.6, 8).translate(0, 1.3, 0), 0x3f7d46),
      colored(
        new THREE.CylinderGeometry(0.18, 0.2, 1.0, 6)
          .rotateZ(Math.PI / 2)
          .translate(0.75, 1.5, 0),
        0x3f7d46,
      ),
      colored(new THREE.CylinderGeometry(0.18, 0.2, 0.9, 6).translate(1.15, 2.0, 0), 0x468a4e),
    ]),
  grass: () =>
    merged([
      colored(new THREE.ConeGeometry(0.1, 0.9, 4).translate(-0.25, 0.45, 0.1), 0x6fae52),
      colored(new THREE.ConeGeometry(0.1, 1.1, 4).translate(0.05, 0.55, -0.15), 0x7dbb5e),
      colored(new THREE.ConeGeometry(0.1, 0.8, 4).translate(0.28, 0.4, 0.12), 0x639a49),
    ]),
  reed: () =>
    merged([
      colored(new THREE.CylinderGeometry(0.05, 0.07, 2.2, 5).translate(-0.2, 1.1, 0.1), 0x7a8a4a),
      colored(new THREE.CylinderGeometry(0.05, 0.07, 2.6, 5).translate(0.15, 1.3, -0.1), 0x8a9a55),
      colored(new THREE.CylinderGeometry(0.05, 0.07, 1.9, 5).translate(0.3, 0.95, 0.2), 0x6e7d42),
    ]),
};

const geometryCache = new Map<PropKind, THREE.BufferGeometry>();

export function propGeometry(kind: PropKind): THREE.BufferGeometry {
  let geo = geometryCache.get(kind);
  if (!geo) {
    geo = PROP_FACTORIES[kind]();
    geometryCache.set(kind, geo);
  }
  return geo;
}

/** One shared material for every prop — vertex colors carry the look. */
export const PROP_MATERIAL = new THREE.MeshStandardMaterial({
  vertexColors: true,
  roughness: 0.9,
  metalness: 0,
  flatShading: true,
});

/**
 * buildScatterMeshes groups instances by kind and returns one InstancedMesh
 * per kind — the caller adds/removes them with the owning chunk. Geometry and
 * material are shared module-wide; disposing the InstancedMesh only frees the
 * per-instance buffer.
 */
export function buildScatterMeshes(instances: ScatterInstance[]): THREE.InstancedMesh[] {
  const byKind = new Map<PropKind, ScatterInstance[]>();
  for (const inst of instances) {
    const list = byKind.get(inst.kind);
    if (list) list.push(inst);
    else byKind.set(inst.kind, [inst]);
  }

  const meshes: THREE.InstancedMesh[] = [];
  const m = new THREE.Matrix4();
  const pos = new THREE.Vector3();
  const quat = new THREE.Quaternion();
  const scl = new THREE.Vector3();
  const up = new THREE.Vector3(0, 1, 0);

  for (const [kind, list] of byKind) {
    const mesh = new THREE.InstancedMesh(propGeometry(kind), PROP_MATERIAL, list.length);
    for (let i = 0; i < list.length; i++) {
      const inst = list[i];
      pos.set(inst.x, inst.y, inst.z);
      quat.setFromAxisAngle(up, inst.rotation);
      scl.setScalar(inst.scale);
      m.compose(pos, quat, scl);
      mesh.setMatrixAt(i, m);
    }
    mesh.instanceMatrix.needsUpdate = true;
    mesh.castShadow = true;
    mesh.receiveShadow = false;
    meshes.push(mesh);
  }
  return meshes;
}
