// The Goals and Advice pages (docs/spec-desktop.md sections 5.3 and 8.4).
// Both edit wot-overlay.yaml through the Go service, which writes only the
// lines a change touches. The page holds exactly what the file sets; an empty
// field means "the default", shown as such. An edit made outside the app is
// picked up while the page is open.
import { call, el } from "./ui.js";

const $ = (id) => document.getElementById(id);
const clone = (v) => JSON.parse(JSON.stringify(v));

let watch = null; // the timer that notices outside edits
let active = null; // {name, version, dirty(), reload()}

export function closePages() {
  if (watch) clearInterval(watch);
  watch = null;
  active = null;
}

// watchFile reloads the page when the file changes on disk: at once if
// nothing is unsaved, else after asking.
function watchFile() {
  if (watch) clearInterval(watch);
  watch = setInterval(async () => {
    if (!active || document.hidden) return;
    const version = await call("OverlayVersion");
    if (version === active.version) return;
    if (!active.dirty()) {
      await active.reload("The settings file was changed outside Tank Advisor; this page now shows it.");
    } else {
      active.outside(version);
    }
  }, 3000);
}

// A small status line under a page's buttons.
function note(box, result) {
  box.className = "page-note " + (result.ok ? "ok" : "fail");
  box.textContent = result.message || "";
  box.hidden = !result.message;
}

// segmented draws a row of radio buttons; value "" is the default, which is
// marked. onchange gets the chosen value.
function segmented(name, choices, value, def, onchange, label) {
  return el("div", { class: "segmented", role: "radiogroup", "aria-label": label },
    ...choices.map((c) => el("label", { class: (value || def) === c.value ? "on" : "" },
      el("input", {
        type: "radio", name, value: c.value, checked: (value || def) === c.value,
        onchange: () => onchange(c.value),
      }),
      c.label + (c.value === def ? " (default)" : ""))));
}

function boolChoice(name, value, def, onchange, label) {
  const choices = [{ value: "on", label: "On" }, { value: "off", label: "Off" }];
  const v = value === null || value === undefined ? "" : value ? "on" : "off";
  return segmented(name, choices, v, def ? "on" : "off", (c) => onchange(c === "on"), label);
}

// ---------------------------------------------------------------- Advice

export async function openAdvice() {
  const root = $("advice-view");
  let page = await call("AdvicePage");
  let settings = clone(page.settings);
  let saved = JSON.stringify(settings);
  let previewTimer = null;
  let outsideVersion = null;

  const dirty = () => JSON.stringify(settings) !== saved;

  const preview = el("div", { class: "preview", "aria-live": "polite" });
  const status = el("p", { class: "page-note", hidden: true });

  function showPreview(pv) {
    const parts = [];
    if (pv.errors && pv.errors.length) {
      parts.push(el("div", { class: "preview-errors" }, el("strong", {}, "Cannot be saved:"),
        el("ul", {}, ...pv.errors.map((e) => el("li", {}, e)))));
    }
    parts.push(el("pre", {}, pv.text));
    preview.replaceChildren(...parts);
    renderRules(pv.conflicts || []);
  }

  function changed() {
    render();
    clearTimeout(previewTimer);
    previewTimer = setTimeout(async () => showPreview(await call("PreviewAdvice", settings)), 250);
  }

  const partsBox = el("div", { class: "advice-parts" });
  const rulesBox = el("div", { class: "rules" });
  let conflicts = [];

  function part(title, id, body, resetKeys) {
    return el("fieldset", { class: "part", id },
      el("legend", {}, title),
      ...body,
      el("button", {
        class: "quiet reset", onclick: () => {
          for (const k of resetKeys) settings[k] = clone(emptyOf(page.settings[k]));
          changed();
        },
      }, "Reset to defaults"));
  }

  function emptyOf(v) {
    if (Array.isArray(v)) return [];
    if (v && typeof v === "object") return {};
    if (typeof v === "number") return 0;
    if (typeof v === "boolean" || v === null) return null;
    return "";
  }

  function numberField(key, label, def, min, max) {
    return el("label", { class: "field" }, label,
      el("input", {
        type: "number", min, max, value: settings[key] ? String(settings[key]) : "", placeholder: String(def) + " (default)",
        onchange: (e) => { settings[key] = e.target.value === "" ? 0 : Number(e.target.value); changed(); },
      }));
  }

  function classTable() {
    const o = page.options;
    const rankChoices = [["0", "—"], ["1", "1"], ["2", "2"], ["3", "3"], ["4", "4"], ["5", "5"]];
    const toggle = (list, value, on) => {
      const set = new Set(settings[list]);
      on ? set.add(value) : set.delete(value);
      settings[list] = [...set];
      changed();
    };
    return el("table", { class: "classes" },
      el("thead", {}, el("tr", {}, el("th", {}, "Class"), el("th", {}, "Preference (1 = most)"),
        el("th", {}, "Never recommend"), el("th", {}, "Getting better at"))),
      el("tbody", {}, ...o.classes.map((c) => el("tr", {},
        el("th", { scope: "row" }, c.label),
        el("td", {}, el("select", {
          "aria-label": c.label + " preference",
          onchange: (e) => { const r = Number(e.target.value); if (r) settings.classRank[c.value] = r; else delete settings.classRank[c.value]; changed(); },
        }, ...rankChoices.map(([v, l]) => el("option", { value: v, selected: String(settings.classRank[c.value] || 0) === v }, l)))),
        el("td", {}, el("input", { type: "checkbox", "aria-label": "Never recommend " + c.label, checked: settings.avoidClasses.includes(c.value), onchange: (e) => toggle("avoidClasses", c.value, e.target.checked) })),
        el("td", {}, el("input", { type: "checkbox", "aria-label": "Getting better at " + c.label, checked: settings.improvementFocus.includes(c.value), onchange: (e) => toggle("improvementFocus", c.value, e.target.checked) }))))));
  }

  function renderRules(found) {
    conflicts = found;
    const o = page.options;
    rulesBox.replaceChildren(
      ...settings.rules.map((r, i) => el("div", { class: "rule" },
        el("textarea", {
          "aria-label": "Rule " + (i + 1), maxlength: o.maxRuleLength, rows: 2, value: r.text,
          oninput: (e) => { r.text = e.target.value; },
          onchange: () => changed(),
        }),
        el("div", { class: "rule-tools" },
          el("span", { class: "note" }, r.added ? "added " + r.added : "new"),
          el("button", { class: "quiet", "aria-label": "Move rule " + (i + 1) + " up", disabled: i === 0, onclick: () => { settings.rules.splice(i - 1, 0, settings.rules.splice(i, 1)[0]); changed(); } }, "↑"),
          el("button", { class: "quiet", "aria-label": "Move rule " + (i + 1) + " down", disabled: i === settings.rules.length - 1, onclick: () => { settings.rules.splice(i + 1, 0, settings.rules.splice(i, 1)[0]); changed(); } }, "↓"),
          el("button", { class: "quiet danger", onclick: () => { settings.rules.splice(i, 1); changed(); } }, "Delete")),
        ...conflicts.filter((c) => c.rule === i).map((c) => el("p", { class: "conflict" }, "⚠ This rule " + c.message + ".")))),
      settings.rules.length < o.maxRules
        ? el("button", { onclick: () => { settings.rules.push({ text: "", added: "" }); render(); rulesBox.querySelector(".rule:last-of-type textarea")?.focus(); } }, "Add a rule")
        : el("p", { class: "note" }, "That is the most rules there can be (" + o.maxRules + ")."));
  }

  function render() {
    const d = page.defaults, o = page.options;
    const weights = el("table", { class: "weights" }, el("tbody", {}, ...o.factors.map((f) => el("tr", {},
      el("th", { scope: "row" }, f.label),
      el("td", {}, segmented("w-" + f.value, o.levels, settings.weights[f.value] || "", d.weights[f.value],
        (v) => { settings.weights[f.value] = v; changed(); }, f.label))))));
    const tiers = [["0", "No limit"], ["5", "V"], ["6", "VI"], ["7", "VII"], ["8", "VIII"], ["9", "IX"], ["10", "X"]];
    partsBox.replaceChildren(
      part("About you", "part-you", [
        el("div", { class: "row2" },
          numberField("sessionMinutes", "Session length (minutes)", d.sessionMinutes, 10, 600),
          numberField("battlesPerHour", "Battles per hour", d.battlesPerHour, 1, 30)),
        el("p", { class: "field-label" }, "Experience"),
        segmented("experience", o.experiences, settings.experience, d.experience, (v) => { settings.experience = v; changed(); }, "Experience"),
        classTable(),
        el("label", { class: "field" }, "Strengths, in your words (comma separated)",
          el("input", {
            type: "text", value: settings.strengths.join(", "), placeholder: "for example: medium tanks, autoloaders",
            onchange: (e) => { settings.strengths = e.target.value.split(",").map((x) => x.trim()).filter(Boolean); changed(); },
          })),
      ], ["sessionMinutes", "battlesPerHour", "experience", "classRank", "avoidClasses", "improvementFocus", "strengths"]),
      part("What matters when choosing", "part-choosing", [
        weights,
        el("div", { class: "row2" },
          el("label", { class: "field" }, "Spend free XP on tanks up to tier",
            el("select", { onchange: (e) => { settings.freeXPMaxTier = Number(e.target.value); changed(); } },
              ...tiers.map(([v, l]) => el("option", { value: v, selected: String(settings.freeXPMaxTier || 0) === v }, l)))),
          numberField("creditBuffer", "Credits to keep after a purchase", 0, 0, 100000000)),
      ], ["weights", "freeXPMaxTier", "creditBuffer"]),
      part("How answers look", "part-answers", [
        el("p", { class: "field-label" }, "Length"),
        segmented("length", o.lengths, settings.length, d.length, (v) => { settings.length = v; changed(); }, "Length"),
        el("p", { class: "field-label" }, "Layout"),
        segmented("format", o.formats, settings.format, d.format, (v) => { settings.format = v; changed(); }, "Layout"),
        el("p", { class: "field-label" }, "Explain the basics"),
        segmented("basics", o.basics, settings.explainBasics, d.explainBasics, (v) => { settings.explainBasics = v; changed(); }, "Explain the basics"),
        el("p", { class: "field-label" }, "One unasked \"also worth knowing\" line"),
        boolChoice("also", settings.alsoWorthKnowing, d.alsoWorthKnowing, (v) => { settings.alsoWorthKnowing = v; changed(); }, "Also worth knowing"),
        el("p", { class: "field-label" }, "Coaching for the classes you are getting better at"),
        boolChoice("coaching", settings.coaching, d.coaching, (v) => { settings.coaching = v; changed(); }, "Coaching"),
      ], ["length", "format", "explainBasics", "alsoWorthKnowing", "coaching"]),
      part("Your rules", "part-rules", [
        el("p", { class: "note" }, "Your own judgement, in your own words. They never override the core rules: Claude still quotes the data's age, sample sizes and sources."),
        rulesBox,
      ], ["rules"]),
    );
    renderRules(conflicts);
  }

  const saveButton = el("button", { class: "primary" }, "Save");
  const discardButton = el("button", {}, "Discard changes");
  const outsideBox = el("div", { class: "banner", hidden: true });

  saveButton.onclick = async () => {
    const result = await call("Do", "advice-page-save", JSON.stringify({ version: page.version, settings }));
    if (result.ok || /changed outside/.test(result.message)) await reload();
    note(status, result);
  };
  discardButton.onclick = () => reload();

  async function reload(message) {
    page = await call("AdvicePage");
    settings = clone(page.settings);
    saved = JSON.stringify(settings);
    outsideBox.hidden = true;
    if (active) active.version = page.version;
    render();
    showPreview(page.preview);
    if (message) note(status, { ok: true, message });
  }

  root.replaceChildren(...[
    el("h2", {}, "Advice"),
    el("p", { class: "note" }, "How Claude advises you. Changes reach Claude at its next answer; a new conversation picks them up for certain. Saved to " + page.path + "."),
    page.problem ? el("div", { class: "banner fail" }, page.problem) : null,
    outsideBox,
    el("div", { class: "advice-layout" },
      partsBox,
      el("aside", { class: "preview-box", "aria-label": "What Claude reads" }, el("h3", {}, "What Claude reads"), preview)),
    el("div", { class: "actions page-actions" }, discardButton, saveButton),
    status].filter(Boolean));
  saveButton.disabled = Boolean(page.problem);

  active = {
    name: "advice", version: page.version, dirty, reload,
    outside(version) {
      if (outsideVersion === version) return;
      outsideVersion = version;
      outsideBox.hidden = false;
      outsideBox.replaceChildren(el("p", {}, "The settings file was changed outside Tank Advisor. Your unsaved changes here would overwrite it."),
        el("button", { onclick: () => reload("Reloaded from the file.") }, "Reload from the file"));
    },
  };
  render();
  showPreview(page.preview);
  watchFile();
}

// ---------------------------------------------------------------- Goals

// tankPicker is a text field that offers the synced vehicles matching what
// is typed. choice is {input, id, label}; onpick gets the new one.
function tankPicker(choice, label, onpick) {
  const list = el("ul", { class: "picks", hidden: true, role: "listbox" });
  let timer = null;
  const input = el("input", {
    type: "text", value: choice.input || "", "aria-label": label, placeholder: "Type a tank's name",
    oninput: (e) => {
      const text = e.target.value;
      onpick({ input: text, id: false, label: "" }, false);
      clearTimeout(timer);
      timer = setTimeout(async () => {
        const found = await call("FindTanks", text);
        list.replaceChildren(...found.map((c) => el("li", {},
          el("button", {
            class: "quiet", role: "option", onclick: () => {
              input.value = c.input;
              list.hidden = true;
              onpick(c, true);
            },
          }, c.label))));
        list.hidden = found.length === 0;
      }, 200);
    },
  });
  return el("div", { class: "picker" }, input,
    choice.label ? el("span", { class: "note" }, choice.label) : null, list);
}

export async function openGoals() {
  const root = $("goals-view");
  let page = await call("GoalsPage");
  let saved = JSON.stringify(page);
  const status = el("p", { class: "page-note", hidden: true });
  const outsideBox = el("div", { class: "banner", hidden: true });
  const body = el("div", {});
  const dirty = () => JSON.stringify(page) !== saved;

  async function viaChoices(goal, select) {
    const options = await call("ResearchedFrom", goal.target);
    select.replaceChildren(el("option", { value: "" }, options.length ? "Choose…" : "—"),
      ...options.map((o, i) => el("option", { value: String(i), selected: goal.via.tankId ? goal.via.tankId === o.tank.tankId : goal.via.input === o.tank.input },
        o.tank.label + (o.xpCost ? " — " + o.xpCost.toLocaleString() + " XP" : ""))));
    select.onchange = () => {
      const o = options[Number(select.value)];
      goal.via = o ? o.tank : { input: "", id: false, label: "" };
      if (o && o.xpCost) goal.xpRequired = o.xpCost;
      render();
    };
  }

  function numberInput(obj, key, label) {
    return el("label", { class: "field" }, label, el("input", {
      type: "number", min: 0, value: obj[key] === null || obj[key] === undefined ? "" : String(obj[key]),
      onchange: (e) => { obj[key] = e.target.value === "" ? null : Number(e.target.value); },
    }));
  }

  function tri(obj, key, label) {
    const v = obj[key] === null || obj[key] === undefined ? "" : obj[key] ? "yes" : "no";
    return el("label", { class: "field" }, label, el("select", {
      onchange: (e) => { obj[key] = e.target.value === "" ? null : e.target.value === "yes"; },
    }, ...[["", "Unknown"], ["yes", "Yes"], ["no", "No"]].map(([val, l]) => el("option", { value: val, selected: v === val }, l))));
  }

  function render() {
    const goals = page.goals.map((g, i) => {
      const via = el("select", { "aria-label": "Researched from" });
      viaChoices(g, via);
      return el("fieldset", { class: "part" },
        el("legend", {}, "Goal " + (i + 1)),
        el("label", { class: "field" }, "Tank to research", tankPicker(g.target, "Goal tank", (c, picked) => { g.target = c; if (picked) render(); })),
        el("label", { class: "field" }, "Researched from", via),
        el("div", { class: "row2" },
          numberInput(g, "xpRequired", "XP required"),
          page.hasDump ? el("p", { class: "note" }, "XP banked comes from the client mod.") : numberInput(g, "xpBanked", "XP banked")),
        el("button", { class: "quiet danger", onclick: () => { page.goals.splice(i, 1); render(); } }, "Remove this goal"));
    });
    const parts = [
      ...goals,
      el("button", { onclick: () => { page.goals.push({ target: { input: "", id: false, label: "" }, via: { input: "", id: false, label: "" }, xpRequired: null, xpBanked: null }); render(); } }, "Add a goal"),
    ];
    if (!page.hasDump) {
      parts.push(el("fieldset", { class: "part" },
        el("legend", {}, "Without the client mod"),
        el("p", { class: "note" }, "The client mod reads these from the game. Without it, set them here."),
        el("div", { class: "row2" }, tri(page, "premiumAccount", "Premium Account"), tri(page, "wotPlus", "WoT Plus")),
        el("p", { class: "field-label" }, "Researched but not bought"),
        ...page.researchedNotBought.map((r, i) => el("div", { class: "row2" },
          tankPicker(r, "Researched tank " + (i + 1), (c) => { page.researchedNotBought[i] = c; }),
          el("button", { class: "quiet danger", onclick: () => { page.researchedNotBought.splice(i, 1); render(); } }, "Remove"))),
        el("button", { onclick: () => { page.researchedNotBought.push({ input: "", id: false, label: "" }); render(); } }, "Add a tank")));
    }
    if (page.warnings && page.warnings.length) {
      parts.push(el("div", { class: "banner" }, el("strong", {}, "Worth a look:"),
        el("ul", {}, ...page.warnings.map((w) => el("li", {}, w)))));
    }
    body.replaceChildren(...parts);
  }

  async function reload(message) {
    page = await call("GoalsPage");
    saved = JSON.stringify(page);
    outsideBox.hidden = true;
    if (active) active.version = page.version;
    render();
    if (message) note(status, { ok: true, message });
  }

  const saveButton = el("button", {
    class: "primary", disabled: Boolean(page.problem), onclick: async () => {
      const result = await call("Do", "goals-save", JSON.stringify({ version: page.version, page }));
      if (result.ok || /changed outside/.test(result.message)) await reload();
      note(status, result);
    },
  }, "Save");

  root.replaceChildren(...[
    el("h2", {}, "Goals"),
    el("p", { class: "note" }, "The tanks you are grinding towards. Claude weighs them when you ask what to do next. Saved to " + page.path + "."),
    page.problem ? el("div", { class: "banner fail" }, page.problem) : null,
    outsideBox,
    body,
    el("div", { class: "actions page-actions" }, el("button", { onclick: () => reload() }, "Discard changes"), saveButton),
    status].filter(Boolean));

  active = {
    name: "goals", version: page.version, dirty, reload,
    outside() {
      outsideBox.hidden = false;
      outsideBox.replaceChildren(el("p", {}, "The settings file was changed outside Tank Advisor. Your unsaved changes here would overwrite it."),
        el("button", { onclick: () => reload("Reloaded from the file.") }, "Reload from the file"));
    },
  };
  render();
  watchFile();
}
