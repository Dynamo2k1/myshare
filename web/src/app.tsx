import { useEffect } from "preact/hooks";
import { signal } from "@preact/signals";
import { connectEvents } from "./lib/events";
import { ToastHost } from "./components/ToastHost";
import { Nav, TABS, type TabId } from "./components/Nav";
import { CommandPalette, paletteOpen } from "./components/CommandPalette";
import { GlobalDrop } from "./components/GlobalDrop";
import { FilesTab } from "./tabs/Files";
import { ClipboardTab } from "./tabs/Clipboard";
import { ScreenshotsTab } from "./tabs/Screenshots";
import { SnippetsTab } from "./tabs/Snippets";
import { NotesTab } from "./tabs/Notes";
import { MarkdownTab } from "./tabs/Markdown";
import { TransfersTab } from "./tabs/Transfers";
import { SettingsTab } from "./tabs/Settings";
import { startUpload } from "./lib/uploader";
import { toast } from "./lib/toast";
import { api } from "./lib/api";

const routeFromHash = (): TabId => {
  const h = location.hash.replace(/^#\/?/, "") as TabId;
  return TABS.some((t) => t.id === h) ? h : "files";
};

export const activeTab = signal<TabId>(routeFromHash());

export function App() {
  useEffect(() => {
    connectEvents();
    const onHash = () => (activeTab.value = routeFromHash());
    window.addEventListener("hashchange", onHash);

    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        paletteOpen.value = !paletteOpen.value;
      }
      if (e.key === "Escape") paletteOpen.value = false;
    };
    window.addEventListener("keydown", onKey);

    // Global paste — works from anywhere, no tab switch required:
    //   • an image  -> uploaded as a screenshot
    //   • plain text (when not typing in a field) -> added to the shared
    //     clipboard, so you can paste and immediately keep working
    const onPaste = (e: ClipboardEvent) => {
      const t = e.target as HTMLElement | null;
      const typingInField =
        !!t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable);

      const items = Array.from(e.clipboardData?.items || []);
      const imgItem = items.find((i) => i.type.startsWith("image/"));

      if (imgItem) {
        const blob = imgItem.getAsFile();
        if (blob) {
          e.preventDefault();
          const ext = blob.type.split("/")[1] || "png";
          const file = new File(
            [blob],
            `screenshot-${new Date().toISOString().replace(/[:.]/g, "-")}.${ext}`,
            { type: blob.type },
          );
          startUpload(file, "screenshot");
          toast("Screenshot added", "success", {
            label: "View",
            run: () => (location.hash = "#/screenshots"),
          });
        }
        return;
      }

      if (typingInField) return; // let the field handle its own text paste

      const text = e.clipboardData?.getData("text/plain") ?? "";
      if (text.trim()) {
        e.preventDefault();
        api
          .post("/api/clipboard", { content: text })
          .then(() =>
            toast("Added to clipboard", "success", {
              label: "Open",
              run: () => (location.hash = "#/clipboard"),
            }),
          )
          .catch((err) => toast((err as Error).message, "error"));
      }
    };
    window.addEventListener("paste", onPaste);

    return () => {
      window.removeEventListener("hashchange", onHash);
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("paste", onPaste);
    };
  }, []);

  const tab = activeTab.value;
  return (
    <div class="shell">
      <Nav />
      <main class="content">
        {tab === "files" && <FilesTab />}
        {tab === "clipboard" && <ClipboardTab />}
        {tab === "screenshots" && <ScreenshotsTab />}
        {tab === "snippets" && <SnippetsTab />}
        {tab === "notes" && <NotesTab />}
        {tab === "markdown" && <MarkdownTab />}
        {tab === "transfers" && <TransfersTab />}
        {tab === "settings" && <SettingsTab />}
      </main>
      <GlobalDrop />
      <CommandPalette />
      <ToastHost />
    </div>
  );
}
