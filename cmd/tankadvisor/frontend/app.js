// The status window: internal/app's rows drawn as cards, and its actions as
// buttons. All logic is in Go; this only draws and forwards clicks.
import { Events, Browser, CancelError } from "/wails/runtime.js";
import { call, el } from "./ui.js";
import { openWizard, wizardOpen } from "./wizard.js";
import { openAdvice, openGoals, closePages } from "./pages.js";

// view is the tab on screen: status, goals or advice.
let view = "status";

const stateLabels = { ok: "OK", warn: "Needs attention", fail: "Not working", unknown: "Unknown" };

// pending is the action this window started and is waiting on.
let pending = null;
let status = null;

const $ = (id) => document.getElementById(id);

// setupOffered is set once the window has opened the setup by itself, so
// closing it does not bring it straight back.
let setupOffered = false;

async function refresh() {
  if (wizardOpen() || view !== "status") return;
  try {
    status = await call("Status");
    if (status.needsSetup && !setupOffered) {
      setupOffered = true;
      showWizard();
      return;
    }
    render();
  } catch (err) {
    showResult({ ok: false, message: "Could not read the status: " + err });
  }
}

function render() {
  const st = status;
  $("account").textContent = (st.account ? st.account + " · " : "") + "version " + st.version;

  const busyWith = pending ? "" : st.running;
  $("busy").hidden = !busyWith;
  $("busy").textContent = busyWith ? "Busy: " + busyWith + "…" : "";

  $("rows").replaceChildren(...st.rows.map(renderRow));
  renderAbout(st.about);
}

function renderRow(row) {
  const section = el("section", { class: "row state-" + row.state, "aria-labelledby": "row-" + row.id },
    el("header", {},
      el("span", { class: "dot", "aria-hidden": "true" }),
      el("h2", { id: "row-" + row.id }, row.title),
      el("span", { class: "badge" }, stateLabels[row.state] || row.state)),
    el("p", { class: "summary" }, row.summary),
    row.lines && row.lines.length ? el("ul", { class: "lines" }, ...row.lines.map((l) => el("li", {}, l))) : null,
    row.hint ? el("p", { class: "hint" }, row.hint) : null,
  );

  const actions = el("div", { class: "actions" });
  const disabled = Boolean(pending) || Boolean(status.running);
  for (const action of row.actions || []) {
    if (pending && pending.id === action.id && pending.arg === (action.arg || "")) {
      actions.append(el("span", { class: "working" }, action.waits
        ? "Waiting for you to log in in your browser…"
        : "Working…"));
      if (action.waits) {
        actions.append(el("button", { onclick: () => pending.promise.cancel() }, "Cancel"));
      }
      continue;
    }
    let select = null;
    if (action.choices && action.choices.length) {
      select = el("select", { "aria-label": action.label + ": choose", disabled },
        ...action.choices.map((c) => el("option", { value: c.value }, c.label)));
      actions.append(select);
    }
    const cls = action.primary ? "primary" : action.danger ? "danger" : "";
    actions.append(el("button", {
      class: cls,
      disabled,
      onclick: () => action.id === "setup" ? showWizard() : run(action, select ? select.value : action.arg || ""),
    }, action.label));
  }
  if (actions.childElementCount) section.append(actions);
  return section;
}

function renderAbout(about) {
  $("notices").replaceChildren(...about.notices.map((n) => el("p", {}, n)));
  $("links").replaceChildren(...about.links.map((link) =>
    el("button", { class: "quiet", onclick: () => Browser.OpenURL(link.url) }, link.label)));
  $("support").textContent = about.support.label;
  $("support").onclick = () => Browser.OpenURL(about.support.url);
}

async function run(action, arg) {
  if (action.confirm && !(await confirm(action))) return;
  hideResult();
  const promise = call("Do", action.id, arg);
  pending = { id: action.id, arg: arg || "", promise };
  render();
  try {
    showResult(await promise);
  } catch (err) {
    showResult(err instanceof CancelError
      ? { ok: false, message: "Cancelled." }
      : { ok: false, message: String(err) });
  } finally {
    pending = null;
    await refresh();
  }
}

function confirm(action) {
  const dialog = $("confirm");
  $("confirm-title").textContent = action.label + "?";
  $("confirm-text").textContent = action.confirm;
  $("confirm-ok").textContent = action.label;
  dialog.returnValue = "";
  dialog.showModal();
  return new Promise((resolve) => {
    dialog.addEventListener("close", () => resolve(dialog.returnValue === "ok"), { once: true });
  });
}

function showResult(result) {
  if (!result || (!result.message && !result.output)) return;
  const box = $("result");
  box.className = "banner " + (result.ok ? "ok" : "fail");
  $("result-message").textContent = result.message || (result.ok ? "Done." : "It did not work.");
  $("result-details").hidden = !result.output;
  $("result-output").textContent = result.output || "";
  box.hidden = false;
}

function hideResult() {
  $("result").hidden = true;
}

// The wizard takes the window's place; closing it brings the rows back.
function showWizard(step) {
  showView("status");
  hideResult();
  for (const id of ["rows", "about", "busy", "tabs"]) $(id).hidden = true;
  openWizard(step, () => {
    for (const id of ["rows", "about", "tabs"]) $(id).hidden = false;
    refresh();
  });
}

// showView switches tabs. The status rows and About belong to the status
// tab; each page loads fresh when opened.
function showView(name) {
  view = name;
  closePages();
  for (const b of document.querySelectorAll("#tabs button")) {
    b.toggleAttribute("aria-current", b.dataset.view === name);
    if (b.dataset.view === name) b.setAttribute("aria-current", "page");
  }
  const onStatus = name === "status";
  for (const id of ["rows", "about"]) $(id).hidden = !onStatus;
  if (!onStatus) {
    $("busy").hidden = true;
    hideResult();
  }
  $("goals-view").hidden = name !== "goals";
  $("advice-view").hidden = name !== "advice";
  if (name === "goals") openGoals();
  if (name === "advice") openAdvice();
  if (onStatus) refresh();
}

for (const b of document.querySelectorAll("#tabs button")) {
  b.addEventListener("click", () => { if (b.dataset.view !== view) showView(b.dataset.view); });
}

$("refresh").addEventListener("click", refresh);
$("open-setup").addEventListener("click", () => showWizard());
$("result-close").addEventListener("click", hideResult);

// A sync from the tray, or anything else outside this window.
Events.On("status-changed", (event) => {
  if (event.data) showResult(event.data);
  refresh();
});

window.addEventListener("focus", refresh);
setInterval(() => { if (!document.hidden) refresh(); }, 30000);
refresh();
