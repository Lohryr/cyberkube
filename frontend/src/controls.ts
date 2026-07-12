import * as THREE from "three";
import { PLAYABLE_BOUND } from "./world/bounds";

// A simple drivable rover: WASD/ZQSD/arrows steer and accelerate a body on
// the ground plane; the camera chases it. Movement reads physical key codes
// (KeyboardEvent.code), so the same four physical keys work on any layout:
// WASD on QWERTY is ZQSD on AZERTY without any configuration. No physics
// engine — just eased kinematics that feel good.

export class Rover {
  readonly object = new THREE.Group();
  private _velocity = 0;
  private heading = 0; // radians
  private readonly keys = new Set<string>();

  private readonly maxSpeed = 42;
  private readonly accel = 55;
  private readonly friction = 28;
  private readonly turnRate = 2.4;

  constructor() {
    const bodyMat = new THREE.MeshStandardMaterial({ color: 0xf5f7fa, metalness: 0.3, roughness: 0.4 });
    const body = new THREE.Mesh(new THREE.BoxGeometry(2.4, 1, 4), bodyMat);
    body.position.y = 1;
    body.castShadow = true;
    this.object.add(body);

    const cabinMat = new THREE.MeshStandardMaterial({ color: 0x4f8dfd, metalness: 0.2, roughness: 0.3 });
    const cabin = new THREE.Mesh(new THREE.BoxGeometry(1.8, 0.9, 1.8), cabinMat);
    cabin.position.set(0, 1.8, -0.2);
    cabin.castShadow = true;
    this.object.add(cabin);

    const wheelGeo = new THREE.CylinderGeometry(0.6, 0.6, 0.5, 12);
    const wheelMat = new THREE.MeshStandardMaterial({ color: 0x15171e });
    for (const [dx, dz] of [
      [-1.3, 1.4],
      [1.3, 1.4],
      [-1.3, -1.4],
      [1.3, -1.4],
    ]) {
      const wheel = new THREE.Mesh(wheelGeo, wheelMat);
      wheel.rotation.z = Math.PI / 2;
      wheel.position.set(dx, 0.6, dz);
      this.object.add(wheel);
    }

    this.object.position.set(0, 0, 90); // start on the edge, looking inward
    this.heading = Math.PI;
  }

  attach(): void {
    window.addEventListener("keydown", this.onKeyDown);
    window.addEventListener("keyup", this.onKeyUp);
  }

  detach(): void {
    window.removeEventListener("keydown", this.onKeyDown);
    window.removeEventListener("keyup", this.onKeyUp);
  }

  /** Suspend input while an overlay/panel is open. */
  setEnabled(enabled: boolean): void {
    if (!enabled) this.keys.clear();
    this.enabled = enabled;
  }

  private enabled = true;

  private readonly onKeyDown = (e: KeyboardEvent) => {
    if (!this.enabled) return;
    this.keys.add(e.code);
  };
  private readonly onKeyUp = (e: KeyboardEvent) => {
    this.keys.delete(e.code);
  };

  private pressed(...codes: string[]): boolean {
    return codes.some((c) => this.keys.has(c));
  }

  /** Current forward(+)/backward(-) speed. Exposed read-only for the minimap/HUD and tests. */
  get velocity(): number {
    return this._velocity;
  }

  /**
   * Test seam: simulate a physical key being held/released without a real
   * KeyboardEvent/DOM. Behaves exactly like `onKeyDown`/`onKeyUp`.
   */
  pressKey(code: string): void {
    if (!this.enabled) return;
    this.keys.add(code);
  }
  releaseKey(code: string): void {
    this.keys.delete(code);
  }

  update(dt: number): void {
    const forward = this.pressed("KeyW", "ArrowUp");
    const back = this.pressed("KeyS", "ArrowDown");
    const left = this.pressed("KeyA", "ArrowLeft");
    const right = this.pressed("KeyD", "ArrowRight");

    if (forward) this._velocity += this.accel * dt;
    else if (back) this._velocity -= this.accel * dt;
    else {
      // ease toward zero
      const drop = this.friction * dt;
      if (Math.abs(this._velocity) <= drop) this._velocity = 0;
      else this._velocity -= Math.sign(this._velocity) * drop;
    }
    this._velocity = THREE.MathUtils.clamp(this._velocity, -this.maxSpeed * 0.5, this.maxSpeed);

    // Steering scales with speed so it turns only while moving. While
    // reversing, invert the steering sign so a "turn left" key still curves
    // the rover's path to the screen-left (matching real-vehicle reverse
    // steering), instead of swinging the nose the opposite way.
    const reverseSign = this._velocity < 0 ? -1 : 1;
    const steer = ((left ? 1 : 0) - (right ? 1 : 0)) * reverseSign;
    this.heading += steer * this.turnRate * dt * Math.min(1, Math.abs(this._velocity) / 6);

    this.object.position.x += Math.sin(this.heading) * this._velocity * dt;
    this.object.position.z += Math.cos(this.heading) * this._velocity * dt;
    this.object.rotation.y = this.heading;

    // keep inside the world bounds (same wall the site placement respects)
    this.object.position.x = THREE.MathUtils.clamp(this.object.position.x, -PLAYABLE_BOUND, PLAYABLE_BOUND);
    this.object.position.z = THREE.MathUtils.clamp(this.object.position.z, -PLAYABLE_BOUND, PLAYABLE_BOUND);
  }

  get groundPosition(): { x: number; z: number } {
    return { x: this.object.position.x, z: this.object.position.z };
  }

  /** Current facing angle in radians (for the minimap arrow). */
  get facing(): number {
    return this.heading;
  }

  /** Position the chase camera behind the rover. */
  updateCamera(camera: THREE.PerspectiveCamera): void {
    const back = new THREE.Vector3(-Math.sin(this.heading), 0, -Math.cos(this.heading));
    const camPos = this.object.position
      .clone()
      .add(back.multiplyScalar(16))
      .add(new THREE.Vector3(0, 11, 0));
    camera.position.lerp(camPos, 0.12);
    // Track the rover's terrain height so hills don't push it off-frame.
    camera.lookAt(this.object.position.x, this.object.position.y + 2, this.object.position.z);
  }
}
