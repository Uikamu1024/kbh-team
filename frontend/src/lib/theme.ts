export type Theme = "light" | "dark";

const THEME_KEY = "tsugaku-radio:theme";
const DEFAULT_THEME: Theme = "light";

export function getStoredTheme(): Theme {
  return window.localStorage.getItem(THEME_KEY) === "dark" ? "dark" : DEFAULT_THEME;
}

// <html data-theme="..."> がsrc/index.cssの配色を切り替える
// （:root がライト、:root[data-theme="dark"] がダーク）。
export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
  window.localStorage.setItem(THEME_KEY, theme);
}
