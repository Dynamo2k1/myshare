import { useEffect } from "preact/hooks";
import { signal } from "@preact/signals";
import { connectEvents } from "./lib/events";
import { ToastHost } from "./components/ToastHost";
import { Nav, TABS, type TabId } from "./components/Nav";
import { CommandPalette, paletteOpen } from "./components/CommandPalette";
import { GlobalDrop } from "./components/GlobalDrop";
import { FilesTab } from "./tabs/Files";
import { FilesBrowse } from "./tabs/FilesBrowse";
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
import { loadMode, serverMode, currentDir } from "./lib/mode";

const routeFromHash = (): TabId => {
  const h = location.hash.replace(/^#\/?/, "") as TabId;
  return TABS.some((t) => t.id === h) ? h : "files";
};

export const activeTab = signal<TabId>(routeFromHash());

// In directory mode, global drop/paste uploads land in the folder you're
// currently viewing; otherwise they go to the personal store.
const uploadDest = () =>
  serverMode.value === "directory"
    ? { endpoint: "/api/browse", dir: currentDir.value }
    : {};

export function App() {
  useEffect(() => {
    connectEvents();
    loadMode();
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
    //   • real files copied in the OS file manager -> uploaded
    //   • a screenshot / image on the clipboard      -> uploaded as a screenshot
    //   • plain text (when not typing in a field)    -> added to the shared clipboard
    const onPaste = (e: ClipboardEvent) => {
      const t = e.target as HTMLElement | null;
      const typingInField =
        !!t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable);

      const cd = e.clipboardData;
      if (!cd) return;

      // 1. Files copied from the OS file manager (or a pasted screenshot, which
      //    browsers also expose here as image/png). Multiple files supported.
      const files = Array.from(cd.files);
      if (files.length > 0) {
        e.preventDefault();
        let screenshots = 0;
        for (const f of files) {
          const isImg = f.type.startsWith("image/");
          const generic = !f.name || /^image\.\w+$/i.test(f.name);
          const named =
            isImg && generic
              ? new File(
                  [f],
                  `screenshot-${new Date().toISOString().replace(/[:.]/g, "-")}.${
                    f.type.split("/")[1] || "png"
                  }`,
                  { type: f.type },
                )
              : f;
          startUpload(named, isImg ? "screenshot" : "file", uploadDest());
          if (isImg) screenshots++;
        }
        const allImg = screenshots === files.length;
        toast(
          files.length === 1
            ? allImg
              ? "Screenshot added"
              : `Uploading ${files[0].name}`
            : `Uploading ${files.length} files`,
          "success",
          {
            label: "View",
            run: () => (location.hash = allImg ? "#/screenshots" : "#/files"),
          },
        );
        return;
      }

      // 2. An image that's only exposed as a clipboard *item* (some screenshot
      //    tools), not a File.
      const imgItem = Array.from(cd.items).find((i) => i.type.startsWith("image/"));
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
          startUpload(file, "screenshot", uploadDest());
          toast("Screenshot added", "success", {
            label: "View",
            run: () => (location.hash = "#/screenshots"),
          });
        }
        return;
      }

      // 3. Plain text -> shared clipboard (unless the user is typing in a field).
      if (typingInField) return;
      const text = cd.getData("text/plain");
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
        {tab === "files" &&
          (serverMode.value === "directory" ? <FilesBrowse /> : <FilesTab />)}
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
