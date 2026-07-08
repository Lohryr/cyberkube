import type { Challenge } from "../api";
import { PLAYABLE_BOUND } from "../world/bounds";
import { categoryColor } from "../world/placement";
import type { World } from "../world/world";

// Top-left minimap of the playable square (±PLAYABLE_BOUND). The biome layer
// is painted once per world rebuild (terrain is deterministic and static) by
// sampling biomeAt on a coarse grid; every frame only composites that layer,
// the challenge dots (pillar/category color, gold once solved — same
// convention as the 3D markers) and the player heading arrow.

const SIZE = 176; // CSS pixels
const SAMPLES = 96; // biome sampling grid, painted once per rebuild
const VIEW = PLAYABLE_BOUND; // world half-size shown

const SOLVED_COLOR = 0xffcc33;

function cssColor(hex: number): string {
  return `#${hex.toString(16).padStart(6, "0")}`;
}

function toPx(v: number): number {
  return ((v + VIEW) / (2 * VIEW)) * SIZE;
}

export class Minimap {
  private readonly canvas = document.createElement("canvas");
  private readonly ctx: CanvasRenderingContext2D;
  private readonly biomeLayer = document.createElement("canvas");
  private dots: { x: number; z: number; color: string }[] = [];

  constructor() {
    this.canvas.className = "minimap";
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    this.canvas.width = SIZE * dpr;
    this.canvas.height = SIZE * dpr;
    this.ctx = this.canvas.getContext("2d")!;
    this.ctx.scale(dpr, dpr);
    this.biomeLayer.width = SAMPLES;
    this.biomeLayer.height = SAMPLES;
    document.body.append(this.canvas);
  }

  /** Repaint the biome layer + challenge dots. Call after world.rebuild(). */
  rebuild(world: World, challenges: Challenge[]): void {
    const bctx = this.biomeLayer.getContext("2d")!;
    const img = bctx.createImageData(SAMPLES, SAMPLES);
    for (let py = 0; py < SAMPLES; py++) {
      const z = ((py + 0.5) / SAMPLES) * 2 * VIEW - VIEW;
      for (let px = 0; px < SAMPLES; px++) {
        const x = ((px + 0.5) / SAMPLES) * 2 * VIEW - VIEW;
        const ground = world.biomeAt(x, z)?.palette.ground ?? 0x0b0e16;
        const i = (py * SAMPLES + px) * 4;
        img.data[i] = (ground >> 16) & 0xff;
        img.data[i + 1] = (ground >> 8) & 0xff;
        img.data[i + 2] = ground & 0xff;
        img.data[i + 3] = 255;
      }
    }
    bctx.putImageData(img, 0, 0);

    const solved = new Set(challenges.filter((c) => c.solvedByTeam).map((c) => c.name));
    this.dots = world.sites.map((s) => ({
      x: s.x,
      z: s.z,
      color: cssColor(solved.has(s.name) ? SOLVED_COLOR : categoryColor(s.category)),
    }));
  }

  /** Composite biomes + dots + player arrow. Call every frame. */
  update(x: number, z: number, heading: number): void {
    const ctx = this.ctx;
    ctx.clearRect(0, 0, SIZE, SIZE);
    ctx.drawImage(this.biomeLayer, 0, 0, SIZE, SIZE);

    for (const d of this.dots) {
      ctx.beginPath();
      ctx.arc(toPx(d.x), toPx(d.z), 3, 0, Math.PI * 2);
      ctx.fillStyle = d.color;
      ctx.fill();
      ctx.lineWidth = 1;
      ctx.strokeStyle = "rgba(0, 0, 0, 0.6)";
      ctx.stroke();
    }

    // Player: triangle pointing along the rover heading. World motion is
    // (sin h, cos h) on (x, z); with the map's y-down projection the local
    // "up" triangle needs a rotation of π − h.
    ctx.save();
    ctx.translate(toPx(x), toPx(z));
    ctx.rotate(Math.PI - heading);
    ctx.beginPath();
    ctx.moveTo(0, -5.5);
    ctx.lineTo(4, 4.5);
    ctx.lineTo(-4, 4.5);
    ctx.closePath();
    ctx.fillStyle = "#f5f7fa";
    ctx.fill();
    ctx.lineWidth = 1.2;
    ctx.strokeStyle = "rgba(0, 0, 0, 0.7)";
    ctx.stroke();
    ctx.restore();
  }
}
