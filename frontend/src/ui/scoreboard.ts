import { api, ApiError } from "../api";
import { el, overlay } from "./dom";

// Scoreboard overlay: teams ranked by points (backend already sorts them).

export async function openScoreboard(onClose: () => void): Promise<void> {
  const body = el("tbody");
  const table = el("table", { class: "scores" }, [
    el("thead", {}, [
      el("tr", {}, [el("th", {}, ["#"]), el("th", {}, ["Team"]), el("th", { class: "pts" }, ["Points"])]),
    ]),
    body,
  ]);

  const closeBtn = el("span", { class: "close" }, ["×"]);
  const card = el("div", { class: "card" }, [closeBtn, el("h1", {}, ["Scoreboard"]), table]);
  const close = overlay(card, true);
  const dismiss = () => { close(); onClose(); };
  closeBtn.addEventListener("click", dismiss);
  card.parentElement?.addEventListener("click", (e) => {
    if (e.target === card.parentElement) onClose();
  });

  try {
    const entries = await api.scoreboard();
    if (entries.length === 0) {
      body.append(el("tr", {}, [el("td", { colSpan: 3, class: "sub" }, ["No scores yet."])]));
      return;
    }
    entries.forEach((e, i) => {
      body.append(
        el("tr", {}, [
          el("td", {}, [String(i + 1)]),
          el("td", {}, [e.teamName]),
          el("td", { class: "pts" }, [String(e.points)]),
        ]),
      );
    });
  } catch (err) {
    body.append(
      el("tr", {}, [
        el("td", { colSpan: 3, class: "msg error" }, [
          err instanceof ApiError ? err.message : "Could not load scoreboard",
        ]),
      ]),
    );
  }
}
