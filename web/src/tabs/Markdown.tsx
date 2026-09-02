import { useEffect, useRef, useState } from "preact/hooks";
import { renderMarkdown, markdownToPlain } from "../lib/markdown";
import { copyText } from "../lib/clipboard";
import { api } from "../lib/api";
import { onEvent } from "../lib/events";

// The Markdown tab is a single shared document that auto-saves and syncs to
// every device — no "save" or "send" button. Local edits are debounced to the
// server; remote edits arrive over SSE. A localStorage copy gives an instant
// first paint and an offline fallback.

const CACHE_KEY = "myshare_scratch_cache";

export function MarkdownTab() {
  const [src, setSrc] = useState<string>(() => {
    try {
      return localStorage.getItem(CACHE_KEY) ?? "";
    } catch {
      return "";
    }
  });
  const [html, setHtml] = useState("");
  const [status, setStatus] = useState<"synced" | "saving" | "offline">("synced");
  const [fullscreen, setFullscreen] = useState(false);
  const previewRef = useRef<HTMLDivElement>(null);

  // Guards so we don't echo our own SSE update back into a save.
  const lastSaved = useRef<string>("");
  const saveTimer = useRef<number | undefined>(undefined);
  const editing = useRef(false);

  // Initial load from the server (authoritative).
  useEffect(() => {
    api
      .get<{ content: string }>("/api/scratch")
      .then((d) => {
        lastSaved.current = d.content;
        if (!editing.current) {
          setSrc(d.content);
          cache(d.content);
        }
      })
      .catch(() => setStatus("offline"));
  }, []);

  // Live updates from other devices.
  useEffect(() => {
    return onEvent("scratch.updated", (d: { content: string }) => {
      if (d.content === lastSaved.current) return;
      lastSaved.current = d.content;
      // Don't clobber the user mid-keystroke; apply when they pause.
      if (!editing.current) {
        setSrc(d.content);
        cache(d.content);
      }
    });
  }, []);

  // Render preview whenever the text changes.
  useEffect(() => {
    let cancelled = false;
    renderMarkdown(src).then((h) => {
      if (!cancelled) setHtml(h);
    });
    return () => {
      cancelled = true;
    };
  }, [src]);

  // Fullscreen Esc handler.
  useEffect(() => {
    if (!fullscreen) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setFullscreen(false);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [fullscreen]);

  const onInput = (value: string) => {
    editing.current = true;
    setSrc(value);
    cache(value);
    setStatus("saving");
    window.clearTimeout(saveTimer.current);
    saveTimer.current = window.setTimeout(() => save(value), 600);
  };

  const save = async (value: string) => {
    if (value === lastSaved.current) {
      setStatus("synced");
      editing.current = false;
      return;
    }
    try {
      await api.put("/api/scratch", { content: value });
      lastSaved.current = value;
      setStatus("synced");
    } catch {
      setStatus("offline");
    } finally {
      editing.current = false;
    }
  };

  return (
    <section class="tab-panel">
      <div class="panel-head">
        <h2>Markdown</h2>
        <div class="panel-tools">
          <span class={`sync-badge sync-${status}`}>
            {status === "synced" ? "✓ synced" : status === "saving" ? "saving…" : "offline"}
          </span>
          <button class="btn" onClick={() => copyText(src)}>
            Copy Markdown
          </button>
          <button class="btn" onClick={() => copyText(markdownToPlain(src))}>
            Copy as text
          </button>
          <button class="btn" onClick={() => copyText(previewRef.current?.innerText ?? "")}>
            Copy rendered
          </button>
          <button class="btn" onClick={() => setFullscreen(true)}>
            ⛶ Fullscreen
          </button>
        </div>
      </div>

      <div class="md-split">
        <textarea
          class="md-input"
          placeholder="Type or paste Markdown — it saves and syncs to your other devices automatically."
          value={src}
          onInput={(e) => onInput((e.target as HTMLTextAreaElement).value)}
          spellcheck={false}
        />
        <div
          class="md-preview markdown-body"
          ref={previewRef}
          dangerouslySetInnerHTML={{ __html: html }}
        />
      </div>

      <p class="hint">
        This document is shared across every device on your MyShare — edits sync live, no save
        button. Select any text in the preview and copy it as usual.
      </p>

      {fullscreen && (
        <div class="md-fullscreen">
          <div class="md-fs-bar">
            <span>Rendered Markdown</span>
            <div>
              <button
                class="btn btn-sm"
                onClick={() => copyText(previewRef.current?.innerText ?? "")}
              >
                Copy all
              </button>
              <button class="btn btn-sm" onClick={() => setFullscreen(false)}>
                Close (Esc)
              </button>
            </div>
          </div>
          <div
            class="md-fs-body markdown-body"
            dangerouslySetInnerHTML={{ __html: html }}
          />
        </div>
      )}
    </section>
  );
}

function cache(v: string) {
  try {
    localStorage.setItem(CACHE_KEY, v);
  } catch {
    /* ignore */
  }
}
