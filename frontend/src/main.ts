import * as THREE from "three";
import "./ui/styles.css";
import { api, ApiError, type Challenge } from "./api";
import { World } from "./world/world";
import { deriveSeedFromChallenges, GENERATOR_VERSION } from "./world/gen/worldgen";
import { Rover } from "./controls";
import { Hud } from "./ui/hud";
import { runLandingFlow } from "./ui/auth";
import { openChallengePanel } from "./ui/challengePanel";
import { openScoreboard } from "./ui/scoreboard";
import { el, overlay } from "./ui/dom";

const ENTER_RADIUS = 9;

async function main(): Promise<void> {
  await runLandingFlow(); // resolves once authed + in a team
  void bootWorld();
}

async function bootWorld(): Promise<void> {
  const app = document.getElementById("app")!;

  const renderer = new THREE.WebGLRenderer({ antialias: true });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  renderer.setSize(window.innerWidth, window.innerHeight);
  renderer.shadowMap.enabled = true;
  renderer.shadowMap.type = THREE.PCFSoftShadowMap;
  app.append(renderer.domElement);

  const camera = new THREE.PerspectiveCamera(60, window.innerWidth / window.innerHeight, 0.1, 1000);

  const world = new World();
  const rover = new Rover();
  world.scene.add(rover.object);
  rover.attach();

  let paused = false;
  const setPaused = (p: boolean) => {
    paused = p;
    rover.setEnabled(!p);
  };

  const hud = new Hud(() => {
    setPaused(true);
    void openScoreboard(() => setPaused(false));
  });

  // Load the world descriptor + catalog and render. Re-runnable so a solve or
  // a new challenge set regenerates the world with no code change. If the
  // backend doesn't expose GET /api/v1/world yet, fall back to a seed derived
  // from the sorted challenge names — still identical across clients.
  let challenges: Challenge[] = [];
  const loadChallenges = async () => {
    try {
      let seed: string;
      let generatorVersion = GENERATOR_VERSION;
      try {
        const info = await api.world();
        challenges = info.challenges ?? (await api.challenges());
        seed = info.seed;
        generatorVersion = info.generatorVersion;
      } catch (err) {
        challenges = await api.challenges();
        seed = deriveSeedFromChallenges(challenges.map((c) => c.name));
        console.info("GET /api/v1/world unavailable, using seed derived from catalog", err);
      }
      world.rebuild(challenges, { seed, generatorVersion });
    } catch (err) {
      showBanner(err instanceof ApiError ? err.message : "Could not load challenges");
    }
  };
  await loadChallenges();

  window.addEventListener("resize", () => {
    camera.aspect = window.innerWidth / window.innerHeight;
    camera.updateProjectionMatrix();
    renderer.setSize(window.innerWidth, window.innerHeight);
  });

  // Interaction: E enters the nearest building; Tab opens the scoreboard.
  window.addEventListener("keydown", (e) => {
    if (paused) return;
    const key = e.key.toLowerCase();
    if (key === "e") {
      const { x, z } = rover.groundPosition;
      const near = world.nearest(x, z, ENTER_RADIUS);
      if (near) {
        setPaused(true);
        openChallengePanel(near.challenge, {
          onSolved: () => void loadChallenges(),
          onClose: () => setPaused(false),
        });
      }
    } else if (key === "tab") {
      e.preventDefault();
      setPaused(true);
      void openScoreboard(() => setPaused(false));
    }
  });

  const clock = new THREE.Clock();
  const tick = () => {
    const dt = Math.min(clock.getDelta(), 0.05);
    if (!paused) rover.update(dt);

    // The ground follows the procedural terrain: snap the rover to the
    // heightmap and stream chunks around it (bounded work per frame).
    {
      const { x, z } = rover.groundPosition;
      rover.object.position.y = world.heightAt(x, z);
      world.update(x, z);
    }
    rover.updateCamera(camera);

    // Contextual prompt when near an unsolved-or-any building.
    if (!paused) {
      const { x, z } = rover.groundPosition;
      const near = world.nearest(x, z, ENTER_RADIUS);
      hud.setPrompt(near ? `Press E — ${near.challenge.displayName || near.challenge.name}` : null);
    }

    renderer.render(world.scene, camera);
    requestAnimationFrame(tick);
  };
  tick();
}

function showBanner(message: string): void {
  const card = el("div", { class: "card" }, [
    el("h2", {}, ["Heads up"]),
    el("p", { class: "sub" }, [message]),
    el("button", { class: "btn", onclick: () => location.reload() }, ["Retry"]),
  ]);
  overlay(card, true);
}

void main();
