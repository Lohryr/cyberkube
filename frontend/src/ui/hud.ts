import { el } from "./dom";

// Persistent on-screen chrome: controls hint, a scoreboard button, and the
// contextual "enter" prompt when the rover is near a building.

export class Hud {
  private prompt: HTMLElement | null = null;

  constructor(onScoreboard: () => void) {
    const hint = el("div", { class: "hud" }, [
      el("div", {}, [kbd("W"), kbd("A"), kbd("S"), kbd("D"), document.createTextNode(" drive")]),
      el("div", {}, [kbd("E"), document.createTextNode(" enter challenge  ·  "), kbd("Tab"), document.createTextNode(" scoreboard")]),
    ]);

    const top = el("div", { class: "hud-top" }, [
      el("button", { onclick: onScoreboard }, ["Scoreboard"]),
    ]);

    document.body.append(hint, top);
  }

  setPrompt(text: string | null): void {
    if (text === null) {
      this.prompt?.remove();
      this.prompt = null;
      return;
    }
    if (!this.prompt) {
      this.prompt = el("div", { class: "prompt" });
      document.body.append(this.prompt);
    }
    this.prompt.textContent = text;
  }
}

function kbd(k: string): HTMLElement {
  return el("kbd", {}, [k]);
}
