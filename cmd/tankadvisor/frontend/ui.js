// What the status window and the setup wizard share: calls into the Go
// service, and building DOM nodes without innerHTML.
import { Call } from "/wails/runtime.js";

const SERVICE = "github.com/ondrejkouril/tank-advisor/internal/app.Service";

export const call = (method, ...args) => Call.ByName(`${SERVICE}.${method}`, ...args);

export function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs)) {
    if (key === "class") node.className = value;
    else if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else if (key === "checked" || key === "value") node[key] = value;
    else if (value !== undefined && value !== false && value !== null) node.setAttribute(key, value === true ? "" : value);
  }
  for (const child of children.flat()) {
    if (child !== null && child !== undefined && child !== false) node.append(child);
  }
  return node;
}
