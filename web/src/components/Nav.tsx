import { activeTab } from "../app";
import { themeChoice, cycleTheme } from "../lib/theme";
import { tasks } from "../lib/uploader";

export type TabId =
  | "files"
  | "clipboard"
  | "screenshots"
  | "snippets"
  | "notes"
  | "markdown"
  | "transfers"
  | "settings";

export const TABS: { id: TabId; label: string; icon: string }[] = [
  { id: "files", label: "Files", icon: "📁" },
  { id: "clipboard", label: "Clipboard", icon: "📋" },
  { id: "screenshots", label: "Screenshots", icon: "🖼️" },
  { id: "snippets", label: "Snippets", icon: "‹›" },
  { id: "notes", label: "Notes", icon: "🗒️" },
  { id: "markdown", label: "Markdown", icon: "▾" },
  { id: "transfers", label: "Transfers", icon: "↕" },
  { id: "settings", label: "Settings", icon: "⚙︎" },
];

export function Nav() {
  const active = activeTab.value;
  const activeXfers = tasks.value.filter(
    (t) => t.status === "uploading" || t.status === "paused",
  ).length;

  return (
    <>
      <header class="topbar">
        <div class="brand" onClick={() => (location.hash = "#/files")}>
          <span class="brand-mark">◈</span> MyShare
        </div>
        <nav class="tabs desktop-only">
          {TABS.map((t) => (
            <a
              key={t.id}
              href={`#/${t.id}`}
              class={`tab ${active === t.id ? "is-active" : ""}`}
            >
              <span class="tab-icon">{t.icon}</span>
              {t.label}
              {t.id === "transfers" && activeXfers > 0 && (
                <span class="badge">{activeXfers}</span>
              )}
            </a>
          ))}
        </nav>
        <button
          class="theme-toggle"
          title={`Theme: ${themeChoice.value}`}
          onClick={cycleTheme}
        >
          {themeChoice.value === "light" ? "☀︎" : themeChoice.value === "dark" ? "☾" : "◐"}
        </button>
      </header>

      <nav class="bottom-nav mobile-only">
        {TABS.map((t) => (
          <a
            key={t.id}
            href={`#/${t.id}`}
            class={`bn-item ${active === t.id ? "is-active" : ""}`}
          >
            <span class="bn-icon">{t.icon}</span>
            <span class="bn-label">{t.label}</span>
            {t.id === "transfers" && activeXfers > 0 && (
              <span class="badge">{activeXfers}</span>
            )}
          </a>
        ))}
      </nav>
    </>
  );
}
