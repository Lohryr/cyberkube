import * as THREE from "three";
import type { Challenge } from "../api";
import { categoryColor, type PlacedChallenge } from "./placement";

// A building is the interactive structure representing one challenge. Solved
// challenges glow gold; a small floating marker sits above each so players can
// spot them from a distance.

export interface Building {
  challenge: Challenge;
  group: THREE.Group;
  /** ground position, for proximity checks */
  position: THREE.Vector2;
}

const BASE_HEIGHT = 6;

export function createBuilding(challenge: Challenge, placed: PlacedChallenge): Building {
  const group = new THREE.Group();
  group.position.set(placed.x, 0, placed.z);

  const height = BASE_HEIGHT * placed.scale;
  const width = 4 * placed.scale;
  const color = categoryColor(challenge.category);

  const bodyMat = new THREE.MeshStandardMaterial({
    color,
    roughness: 0.6,
    metalness: 0.1,
    emissive: challenge.solvedByTeam ? 0xffcc33 : 0x000000,
    emissiveIntensity: challenge.solvedByTeam ? 0.35 : 0,
  });
  const body = new THREE.Mesh(new THREE.BoxGeometry(width, height, width), bodyMat);
  body.position.y = height / 2;
  body.castShadow = true;
  body.receiveShadow = true;
  group.add(body);

  // Roof marker: cone for dynamic (launchable), sphere for static.
  const markerMat = new THREE.MeshStandardMaterial({
    color: challenge.solvedByTeam ? 0xffcc33 : 0xffffff,
    emissive: challenge.solvedByTeam ? 0xffcc33 : 0x333333,
    emissiveIntensity: 0.5,
  });
  const marker =
    challenge.mode === "dynamic"
      ? new THREE.Mesh(new THREE.ConeGeometry(width * 0.4, 2.5, 8), markerMat)
      : new THREE.Mesh(new THREE.SphereGeometry(width * 0.35, 16, 12), markerMat);
  marker.position.y = height + 2;
  group.add(marker);

  // Store the challenge name so raycasting/click can resolve back to it.
  group.userData.challengeName = challenge.name;

  return { challenge, group, position: new THREE.Vector2(placed.x, placed.z) };
}

/**
 * A translucent tinted disc on the flattened terrain pad under each challenge
 * site, so sites read as places from a distance. Sits at the site's terrain
 * height (the generator flattens a circular pad of `radius` around it).
 */
export function createSitePad(
  x: number,
  y: number,
  z: number,
  category: string,
  radius: number,
): THREE.Mesh {
  const geo = new THREE.CircleGeometry(radius, 40);
  const mat = new THREE.MeshBasicMaterial({
    color: categoryColor(category),
    transparent: true,
    opacity: 0.14,
  });
  const pad = new THREE.Mesh(geo, mat);
  pad.rotation.x = -Math.PI / 2;
  pad.position.set(x, y + 0.06, z);
  return pad;
}
