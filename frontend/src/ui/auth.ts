import { api, ApiError } from "../api";
import { el, overlay } from "./dom";

// Landing flow: authenticate, then ensure the player has a team (scoring and
// instances are team-scoped, so play is gated behind team membership).

/** Resolves once the user is authenticated AND in a team. */
export function runLandingFlow(): Promise<void> {
  return new Promise((resolve) => {
    void showAuth(resolve);
  });
}

async function showAuth(done: () => void): Promise<void> {
  // Already logged in? Skip straight to the team gate.
  try {
    await api.me();
    void showTeamGate(done);
    return;
  } catch {
    // fall through to the login/register card
  }
  renderAuthCard(done);
}

function renderAuthCard(done: () => void): void {
  let mode: "login" | "register" = "login";

  const title = el("h1", {}, ["CyberKube"]);
  const sub = el("p", { class: "sub" }, ["Sign in to enter the world."]);
  const msg = el("p", { class: "msg" });

  const usernameField = field("Username", "text", "username");
  const emailField = field("Email", "email", "email");
  const passwordField = field("Password", "password", "password");
  const submit = el("button", { class: "btn" }, ["Log in"]);
  const switcher = el("span", { class: "switch" });

  const card = el("div", { class: "card" }, [
    title,
    sub,
    usernameField.wrap,
    emailField.wrap,
    passwordField.wrap,
    submit,
    msg,
    switcher,
  ]);
  const close = overlay(card, false);

  const render = () => {
    emailField.wrap.style.display = mode === "register" ? "" : "none";
    submit.textContent = mode === "register" ? "Create account" : "Log in";
    sub.textContent = mode === "register" ? "Create your operator profile." : "Sign in to enter the world.";
    switcher.replaceChildren(
      document.createTextNode(mode === "register" ? "Have an account? " : "New here? "),
      el("a", { onclick: () => { mode = mode === "register" ? "login" : "register"; render(); } }, [
        mode === "register" ? "Log in" : "Create one",
      ]),
    );
    msg.textContent = "";
    msg.className = "msg";
  };
  render();

  const attempt = async () => {
    msg.className = "msg";
    msg.textContent = "";
    submit.disabled = true;
    try {
      if (mode === "register") {
        await api.register(usernameField.input.value, emailField.input.value, passwordField.input.value);
      }
      const loginId = mode === "register" ? usernameField.input.value : usernameField.input.value;
      await api.login(loginId, passwordField.input.value);
      close();
      void showTeamGate(done);
    } catch (err) {
      msg.className = "msg error";
      msg.textContent = err instanceof ApiError ? err.message : "Something went wrong";
      submit.disabled = false;
    }
  };

  submit.addEventListener("click", () => void attempt());
  passwordField.input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") void attempt();
  });
}

async function showTeamGate(done: () => void): Promise<void> {
  try {
    await api.myTeam();
    done(); // already in a team
    return;
  } catch {
    renderTeamCard(done);
  }
}

function renderTeamCard(done: () => void): void {
  let mode: "create" | "join" = "create";

  const sub = el("p", { class: "sub" }, ["Scoring is team-based — create a team or join one."]);
  const msg = el("p", { class: "msg" });

  const nameField = field("Team name", "text", "team");
  const codeField = field("Invite code", "text", "code");
  const submit = el("button", { class: "btn success" }, ["Create team"]);
  const switcher = el("span", { class: "switch" });

  const card = el("div", { class: "card" }, [
    el("h1", {}, ["Join the fight"]),
    sub,
    nameField.wrap,
    codeField.wrap,
    submit,
    msg,
    switcher,
  ]);
  const close = overlay(card, false);

  const render = () => {
    nameField.wrap.style.display = mode === "create" ? "" : "none";
    codeField.wrap.style.display = mode === "join" ? "" : "none";
    submit.textContent = mode === "create" ? "Create team" : "Join team";
    switcher.replaceChildren(
      document.createTextNode(mode === "create" ? "Got an invite code? " : "Want your own team? "),
      el("a", { onclick: () => { mode = mode === "create" ? "join" : "create"; render(); } }, [
        mode === "create" ? "Join a team" : "Create a team",
      ]),
    );
    msg.textContent = "";
    msg.className = "msg";
  };
  render();

  const attempt = async () => {
    submit.disabled = true;
    msg.className = "msg";
    try {
      if (mode === "create") {
        const team = await api.createTeam(nameField.input.value);
        msg.className = "msg ok";
        msg.textContent = team.inviteCode ? `Team created. Invite code: ${team.inviteCode}` : "Team created.";
        setTimeout(() => { close(); done(); }, 1400);
      } else {
        await api.joinTeam(codeField.input.value);
        close();
        done();
      }
    } catch (err) {
      msg.className = "msg error";
      msg.textContent = err instanceof ApiError ? err.message : "Something went wrong";
      submit.disabled = false;
    }
  };

  submit.addEventListener("click", () => void attempt());
}

function field(label: string, type: string, name: string) {
  const input = el("input", { type, name, autocomplete: "off" });
  const wrap = el("div", { class: "field" }, [el("label", {}, [label]), input]);
  return { wrap, input };
}
