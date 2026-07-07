import { api, ApiError, type Challenge, type Instance } from "../api";
import { el, overlay } from "./dom";

// The interaction panel shown when a player enters a challenge structure.
// Both modes show description + scoring + a flag submit box. Static challenges
// add attachment downloads; dynamic challenges add instance launch +
// connection info.

export interface PanelCallbacks {
  /** called after a successful solve so the world can re-render (glow) */
  onSolved: (challengeName: string) => void;
  /** called when the panel closes so controls resume */
  onClose: () => void;
}

export function openChallengePanel(challenge: Challenge, cb: PanelCallbacks): void {
  const msg = el("p", { class: "msg" });

  const header = el("div", {}, [
    el("h1", {}, [challenge.displayName || challenge.name]),
    el("div", {}, [
      el("span", { class: "tag" }, [challenge.category || "misc"]),
      el("span", { class: `tag mode-${challenge.mode}` }, [challenge.mode]),
      el("span", { class: "tag" }, [`${challenge.value} pts`]),
      el("span", { class: "tag" }, [`${challenge.solves} solves`]),
      ...(challenge.solvedByTeam ? [el("span", { class: "tag solved" }, ["✓ solved"])] : []),
    ]),
  ]);

  const desc = el("div", { class: "desc" }, [challenge.description || "No description."]);

  const dynamicArea = el("div");
  if (challenge.mode === "dynamic") {
    dynamicArea.append(buildDynamicArea(challenge));
  } else {
    dynamicArea.append(buildAttachments(challenge));
  }

  // Flag submission
  const flagInput = el("input", { type: "text", placeholder: "CTF{...}", name: "flag", autocomplete: "off" });
  const submitBtn = el("button", { class: "btn success" }, ["Submit flag"]);
  const submitRow = el("div", { class: "row" }, [
    el("div", { class: "field", style: "flex:1;margin:0" }, [flagInput]),
    submitBtn,
  ]);

  const closeBtn = el("span", { class: "close" }, ["×"]);
  const card = el("div", { class: "card" }, [closeBtn, header, desc, dynamicArea, submitRow, msg]);
  const close = overlay(card, true);

  const dismiss = () => { close(); cb.onClose(); };
  closeBtn.addEventListener("click", dismiss);
  card.parentElement?.addEventListener("click", (e) => {
    if (e.target === card.parentElement) cb.onClose();
  });

  const submit = async () => {
    const flag = flagInput.value.trim();
    if (!flag) return;
    submitBtn.disabled = true;
    msg.className = "msg";
    try {
      const res = await api.submit(challenge.name, flag);
      if (res.correct) {
        msg.className = "msg ok";
        msg.textContent = res.points ? `Correct! +${res.points} points` : "Correct! (already solved)";
        cb.onSolved(challenge.name);
      } else {
        msg.className = "msg error";
        msg.textContent = "Incorrect flag.";
        submitBtn.disabled = false;
      }
    } catch (err) {
      msg.className = "msg error";
      msg.textContent = err instanceof ApiError ? err.message : "Submission failed";
      submitBtn.disabled = false;
    }
  };
  submitBtn.addEventListener("click", () => void submit());
  flagInput.addEventListener("keydown", (e) => {
    if (e.key === "Enter") void submit();
  });
}

function buildAttachments(challenge: Challenge): HTMLElement {
  const names = challenge.attachments ?? [];
  if (names.length === 0) return el("div");
  return el("div", { class: "attachments" }, [
    el("p", { class: "sub", style: "margin:0 0 6px" }, ["Files"]),
    ...names.map((n) => el("a", { href: api.attachmentUrl(challenge.name, n), download: n }, [n])),
  ]);
}

function buildDynamicArea(challenge: Challenge): HTMLElement {
  const container = el("div");
  const status = el("div");
  const launchBtn = el("button", { class: "btn" }, ["Launch instance"]);
  container.append(launchBtn, status);

  const showInstance = (inst: Instance) => {
    if (inst.status === "ready") {
      launchBtn.style.display = "none";
      const conn = inst.connectionInfo ?? "";
      const looksUrl = /^https?:\/\//.test(conn);
      status.replaceChildren(
        el("div", { class: "conn" }, [
          looksUrl
            ? el("a", { href: conn, target: "_blank", rel: "noreferrer" }, [conn])
            : document.createTextNode(conn || "Instance ready"),
        ]),
        ...(inst.expiresAt
          ? [el("p", { class: "sub", style: "margin:0" }, [`Expires ${new Date(inst.expiresAt).toLocaleTimeString()}`])]
          : []),
      );
    } else if (inst.status === "pending") {
      launchBtn.style.display = "none";
      status.replaceChildren(
        el("p", { class: "sub" }, []),
        spinnerText("Provisioning your instance…"),
      );
      // poll until ready
      setTimeout(() => void poll(), 2500);
    } else {
      launchBtn.style.display = "";
      status.replaceChildren();
    }
  };

  const poll = async () => {
    try {
      showInstance(await api.instance(challenge.name));
    } catch {
      status.replaceChildren(el("p", { class: "msg error" }, ["Could not fetch instance status."]));
    }
  };

  const launch = async () => {
    launchBtn.disabled = true;
    status.replaceChildren(spinnerText("Launching…"));
    try {
      showInstance(await api.launch(challenge.name));
    } catch (err) {
      launchBtn.disabled = false;
      status.replaceChildren(
        el("p", { class: "msg error" }, [err instanceof ApiError ? err.message : "Launch failed"]),
      );
    }
  };
  launchBtn.addEventListener("click", () => void launch());

  // On open, reflect any existing instance.
  void poll();

  return container;
}

function spinnerText(text: string): HTMLElement {
  return el("p", { class: "sub" }, [el("span", { class: "spinner" }), document.createTextNode(" " + text)]);
}
