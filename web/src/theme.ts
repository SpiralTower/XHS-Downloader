export type ThemePreference = "light" | "dark" | "system";

const storageKey = "xhs-downloader-theme";

export function readThemePreference(): ThemePreference {
  try {
    const value = window.localStorage.getItem(storageKey);
    if (value === "light" || value === "dark" || value === "system") {
      return value;
    }
  } catch {
    // Storage may be unavailable in privacy-restricted browser contexts.
  }
  return "system";
}

export function storeThemePreference(theme: ThemePreference): void {
  try {
    window.localStorage.setItem(storageKey, theme);
  } catch {
    // Theme persistence is optional; the current page can still be themed.
  }
}

export function applyTheme(theme: ThemePreference): void {
  const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
  const isDark = theme === "dark" || (theme === "system" && prefersDark);
  const root = document.documentElement;

  root.classList.toggle("dark", isDark);
  root.classList.toggle("light", !isDark);
  root.dataset.theme = isDark ? "dark" : "light";

  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute("content", isDark ? "#090b10" : "#f5f6f8");
}
