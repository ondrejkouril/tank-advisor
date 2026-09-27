// The setup wizard (docs/spec-desktop.md section 5.1): eight steps, each a
// page drawn from the Go service's Wizard state. The wizard never shows a
// command line; an error says what to click.
import { Application, CancelError } from "/wails/runtime.js";
import { call, el } from "./ui.js";

const $ = (id) => document.getElementById(id);

let open = false;
let onClose = null;
let state = null; // the last Wizard from Go
let current = ""; // the step on screen
let pending = null; // the action running, as {id, promise}
let poll = null; // the timer that watches for the first dump
// answers holds the questionnaire answers the player touched; untouched
// questions keep their defaults and are not written.
let answers = {};

export const wizardOpen = () => open;

export async function openWizard(step, closed) {
  open = true;
  onClose = closed;
  answers = {};
  $("wizard").hidden = false;
  await load();
  show(step || state.first);
}

function close() {
  open = false;
  stopPoll();
  $("wizard").hidden = true;
  if (onClose) onClose();
}

async function load() {
  state = await call("Wizard");
}

function stepIndex(id) {
  return state.steps.findIndex((s) => s.id === id);
}

// show moves to a step, reading the state again first: a login finished in
// the browser, or a dump the game wrote, changes it from outside.
async function show(id) {
  current = id;
  stopPoll();
  message(null);
  await load();
  render();
}

function next() {
  const i = stepIndex(current);
  if (i < state.steps.length - 1) show(state.steps[i + 1].id);
}

function back() {
  const i = stepIndex(current);
  if (i > 0) show(state.steps[i - 1].id);
}

function message(result) {
  const box = $("wizard-message");
  if (!result || !result.message) {
    box.hidden = true;
    return;
  }
  box.hidden = false;
  box.className = "wizard-message " + (result.ok ? "ok" : "fail");
  box.textContent = result.message;
}

// act runs one of the service's actions, redraws, and returns its result.
async function act(id, arg = "") {
  message(null);
  const promise = call("Do", id, arg);
  pending = { id, promise };
  render();
  let result;
  try {
    result = await promise;
  } catch (err) {
    result = { ok: false, message: err instanceof CancelError ? "Cancelled." : String(err) };
  }
  pending = null;
  await load();
  render();
  message(result);
  return result;
}

function render() {
  $("wizard-steps").replaceChildren(...state.steps.map((s, i) =>
    el("li", { class: s.done ? "done" : "" },
      el("button", {
        "aria-current": s.id === current ? "step" : false,
        disabled: Boolean(pending),
        onclick: () => show(s.id),
      }, el("span", { class: "mark", "aria-hidden": "true" }, s.done ? "✓" : String(i + 1)),
      s.title, s.done ? el("span", { class: "sr-only" }, " (done)") : null))));

  const page = pages[current] || pages.welcome;
  const { body, footer } = page();
  $("wizard-body").replaceChildren(...body.filter((node) => node));

  const first = stepIndex(current) === 0;
  const nav = [];
  nav.push(el("button", { class: "quiet", onclick: close, disabled: Boolean(pending) },
    state.completed ? "Close" : "Finish later"));
  nav.push(el("span", { class: "spacer" }));
  if (!first) nav.push(el("button", { onclick: back, disabled: Boolean(pending) }, "Back"));
  $("wizard-footer").replaceChildren(...nav, ...footer);
}

function busyNote(id, text) {
  if (!pending || pending.id !== id) return null;
  return el("p", { class: "waiting" }, text || "Working…");
}

function nextButton(label = "Next", enabled = true, onclick = next) {
  return el("button", { class: "primary", disabled: !enabled || Boolean(pending), onclick }, label);
}

const pages = {
  welcome() {
    const agreed = state.steps[0].done;
    return {
      body: [
        el("h2", {}, "Welcome to Tank Advisor"),
        ...state.notice.map((p) => el("p", {}, p)),
      ],
      footer: agreed
        ? [nextButton()]
        : [
          el("button", { class: "quiet", onclick: () => Application.Quit() }, "Quit"),
          nextButton("I agree", true, async () => {
            if ((await act("agree")).ok) next();
          }),
        ],
    };
  },

  server() {
    const name = "realm";
    const body = [
      el("h2", {}, "Your server"),
      el("p", {}, "Choose the server your account plays on."),
    ];
    if (state.realmLocked) {
      body.push(el("p", {}, "This installation holds " + state.account + ". To use another server, delete your data from the status window first."));
    }
    body.push(el("div", { class: "choice-list", role: "radiogroup" },
      ...state.realms.map((r) => el("label", {},
        el("input", {
          type: "radio", name, value: r.value,
          checked: r.value === state.realm,
          disabled: state.realmLocked,
        }), r.label))));
    return {
      body,
      footer: [nextButton("Next", true, async () => {
        if (state.realmLocked) return next();
        const chosen = document.querySelector(`input[name="${name}"]:checked`);
        if ((await act("set-realm", chosen ? chosen.value : "")).ok) next();
      })],
    };
  },

  login() {
    const done = state.steps[stepIndex("login")].done;
    const body = [el("h2", {}, "Log in with Wargaming")];
    if (done) {
      body.push(el("p", {}, "Logged in as " + state.account + "."));
    } else {
      body.push(
        el("p", {}, "Your browser opens Wargaming's own login page. Log in there with the account you want advice for; Tank Advisor never sees your password."),
        el("p", { class: "note" }, "The login lasts two weeks, and Tank Advisor renews it before it runs out."),
      );
      if (pending && pending.id === "login") {
        body.push(el("p", { class: "waiting" }, "Waiting for you to log in in your browser…"),
          el("button", { onclick: () => pending.promise.cancel() }, "Cancel"));
      }
    }
    return {
      body,
      footer: done
        ? [nextButton()]
        : [nextButton("Log in with Wargaming", true, async () => {
          if ((await act("login")).ok) next();
        })],
    };
  },

  game() {
    const name = "game";
    const body = [
      el("h2", {}, "World of Tanks"),
      el("p", {}, "The client mod goes into this game folder, and its version tells when the game has been updated."),
    ];
    if (state.games && state.games.length) {
      body.push(el("div", { class: "choice-list", role: "radiogroup" },
        ...state.games.map((g, i) => el("label", {},
          el("input", { type: "radio", name, value: g.dir, checked: g.chosen || (!state.games.some((x) => x.chosen) && i === 0) }),
          g.label))));
    }
    if (state.gameDir && !(state.games || []).some((g) => g.chosen)) {
      body.push(el("p", {}, "Using " + state.gameDir + " (game " + state.gameVersion + ")."));
    } else if (state.gameVersion) {
      body.push(el("p", { class: "note" }, "Game version " + state.gameVersion + "."));
    }
    if (state.gameProblem) body.push(el("p", {}, state.gameProblem));
    body.push(el("button", { onclick: () => act("game-choose"), disabled: Boolean(pending) }, "Choose another folder…"));
    const hasGames = state.games && state.games.length;
    return {
      body,
      footer: [nextButton("Next", Boolean(hasGames || state.gameVersion), async () => {
        const chosen = document.querySelector(`input[name="${name}"]:checked`);
        if (!chosen) return next();
        if ((await act("game-use", chosen.value)).ok) next();
      })],
    };
  },

  mod() {
    const m = state.mod;
    const body = [
      el("h2", {}, "The client mod"),
      el("p", {}, "The game shows things no Wargaming service gives out: each tank's XP, your progress to the next mark, loadouts and crew. A small mod reads them in the garage and writes them to a file on this computer."),
      el("p", { class: "note" }, "It only reads, only in the garage, and shows nothing in battle. It does not touch the network."),
    ];
    const footer = [];
    if (!m.package) {
      body.push(el("p", {}, "This build of Tank Advisor carries no mod, so this step is skipped."));
      footer.push(nextButton());
    } else if (!m.installed) {
      body.push(busyNote("mod-install", "Installing… If Windows asks for permission, the game folder needs it for this one copy."));
      footer.push(
        el("button", { class: "quiet", onclick: next, disabled: Boolean(pending) }, "Skip"),
        nextButton("Install the mod", true, () => act("mod-install")),
      );
    } else if (m.dumpArrived) {
      body.push(el("p", {}, "Installed, and the game has written its first file (" + m.dump + ")."));
      footer.push(nextButton());
    } else {
      body.push(el("p", {}, "Installed. Now start World of Tanks, log in, and wait in the garage for a few seconds."));
      if (m.otherAccount) {
        body.push(el("p", {}, "The game is logged in as another account than " + state.account + ". Log in with that account in the game, and wait in the garage again."));
      }
      body.push(el("p", { class: "waiting" }, "Waiting for the game…"));
      startPoll();
      footer.push(
        el("button", { class: "quiet", onclick: next }, "Skip for now"),
        nextButton("Next", false),
      );
    }
    body.push(el("p", { class: "note" }, "Skipping leaves tank XP, marks, loadouts and crew unknown. After a game update, Tank Advisor puts the mod into the new version's folder by itself."));
    return { body, footer };
  },

  claude() {
    const c = state.claude;
    const body = [el("h2", {}, "Connect Claude")];
    if (c.desktop) {
      const status = c.extensionCurrent ? "The Tank Advisor extension is installed."
        : c.extension ? "An older Tank Advisor extension (" + c.extension + ") is installed."
          : "Claude Desktop is installed, without the Tank Advisor extension.";
      body.push(el("h3", {}, "Claude Desktop"), el("p", {}, status));
      if (!c.extensionCurrent && c.bundle) {
        body.push(el("button", { class: "primary", onclick: () => act("claude-ext"), disabled: Boolean(pending) },
          c.extension ? "Update the extension" : "Install the extension"));
        body.push(el("p", { class: "note" }, "Claude Desktop asks you to confirm. If Extensions is missing from its settings, your organisation has turned extensions off."));
      }
    } else {
      body.push(el("h3", {}, "Claude Desktop"), el("p", {}, "Claude Desktop was not found."),
        el("button", { onclick: () => act("claude-get"), disabled: Boolean(pending) }, "Get Claude Desktop"),
        el("p", { class: "note" }, "Install it, then come back to this step: Setup, then Connect Claude."));
    }
    if (c.codeCLI) {
      body.push(el("h3", {}, "Claude Code"));
      if (c.plugin) {
        body.push(el("p", {}, "The wot plugin is installed."));
      } else {
        body.push(el("p", {}, "Claude Code is installed. The wot plugin gives it the same tools."),
          el("button", { onclick: () => act("claude-code"), disabled: Boolean(pending) }, "Install the plugin"),
          busyNote("claude-code"));
      }
    }
    body.push(el("p", { class: "note" }, "On claude.ai and the phone, use Export for claude.ai in the status window."));
    return { body, footer: [nextButton()] };
  },

  advice() {
    const body = [
      el("h2", {}, "Your advice"),
      el("p", {}, "A few questions that change how Claude advises you. Each has a sensible default; leave any you are unsure about."),
    ];
    for (const q of state.questions) body.push(question(q));
    return {
      body,
      footer: [
        el("button", { class: "quiet", onclick: next, disabled: Boolean(pending) }, "Skip"),
        nextButton("Save and continue", true, async () => {
          if (Object.keys(answers).length === 0) return next();
          if ((await act("advice-save", JSON.stringify(answers))).ok) {
            answers = {};
            next();
          }
        }),
      ],
    };
  },

  sync() {
    const done = state.steps[stepIndex("sync")].done;
    const body = [el("h2", {}, "First sync")];
    if (!done) {
      body.push(el("p", {}, "Tank Advisor now fetches your account from Wargaming. It takes a minute."),
        busyNote("sync", "Syncing…"));
    } else {
      body.push(el("p", {}, "Your data is here. Open Claude and ask, for example:"),
        el("ul", { class: "examples" }, ...state.examples.map((q) => el("li", {}, q))),
        el("p", { class: "note" }, "Tank Advisor stays in the tray and keeps the data current. Its window shows what is working and what needs a click."));
    }
    return {
      body,
      footer: done
        ? [nextButton("Finish", true, async () => {
          if ((await act("setup-finish")).ok) close();
        })]
        : [nextButton("Sync now", true, () => act("sync"))],
    };
  },
};

function question(q) {
  const id = "q-" + q.key.replaceAll(".", "-");
  const set = (v) => { answers[q.key] = v; };
  let input;
  if (q.kind === "choice") {
    input = el("select", { id, onchange: (e) => set(e.target.value) },
      ...q.choices.map((c) => el("option", { value: c.value, selected: String(q.value) === c.value }, c.label)));
    if (q.key in answers) input.value = answers[q.key];
  } else if (q.kind === "number") {
    input = el("input", { id, type: "number", min: q.min, max: q.max, value: String(answers[q.key] ?? q.value),
      onchange: (e) => set(Number(e.target.value)) });
  } else {
    const chosen = new Set(answers[q.key] ?? q.value ?? []);
    input = el("div", { class: "checks" }, ...q.choices.map((c) => el("label", {},
      el("input", {
        type: "checkbox", value: c.value, checked: chosen.has(c.value),
        onchange: (e) => {
          e.target.checked ? chosen.add(c.value) : chosen.delete(c.value);
          set([...chosen]);
        },
      }), " " + c.label)));
    return el("fieldset", { class: "question" }, el("legend", {}, q.text),
      q.help ? el("p", { class: "help" }, q.help) : null, input);
  }
  return el("div", { class: "question" }, el("label", { for: id }, q.text),
    q.help ? el("p", { class: "help" }, q.help) : null, input);
}

// While the mod step waits for the game, look for the first dump every few
// seconds.
function startPoll() {
  if (poll) return;
  poll = setInterval(async () => {
    if (current !== "mod" || pending) return;
    await load();
    if (state.mod.dumpArrived) {
      stopPoll();
      message(null);
      render();
    }
  }, 3000);
}

function stopPoll() {
  if (poll) clearInterval(poll);
  poll = null;
}
