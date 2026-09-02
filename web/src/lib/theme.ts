import { signal, effect } from "@preact/signals";

export type ThemeChoice = "light" | "dark" | "system";

const KEY = "myshare_theme";

function readStored(): ThemeChoice {
  try {
    const v = localStorage.getItem(KEY);
    if (v === "light" || v === "dark" || v === "system") return v;
  } catch {
    /* ignore */
  }
  return "system";
}

export const themeChoice = signal<ThemeChoice>(readStored());

function apply(choice: ThemeChoice) {
  const root = document.documentElement;
  if (choice === "system") root.removeAttribute("data-theme");
  else root.setAttribute("data-theme", choice);
}

effect(() => {
  apply(themeChoice.value);
  try {
    localStorage.setItem(KEY, themeChoice.value);
  } catch {
    /* non-persistent is fine */
  }
});

export function cycleTheme() {
  themeChoice.value =
    themeChoice.value === "light"
      ? "dark"
      : themeChoice.value === "dark"
        ? "system"
        : "light";
}
