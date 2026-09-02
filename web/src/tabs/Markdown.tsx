import { useEffect, useRef, useState } from "preact/hooks";
import { renderMarkdown, markdownToPlain } from "../lib/markdown";
import { copyText } from "../lib/clipboard";
import { api } from "../lib/api";
import { toast } from "../lib/toast";

const STORE_KEY = "myshare_markdown_draft";
const SAMPLE = `# Markdown preview

Paste **Markdown** here and see it rendered — headings, *emphasis*,
\`inline code\`, lists and links become real formatting instead of raw symbols.

- one
- two
  - nested

> A blockquote.

\`\`\`js
console.log("fenced code blocks too");
\`\`\`

[A link](https://example.com)
`;

export function MarkdownTab() {
  const [src, setSrc] = useState<string>(() => {
    try {
      return localStorage.getItem(STORE_KEY) ?? SAMPLE;
    } catch {
      return SAMPLE;
    }
  });
  const [html, setHtml] = useState("");
  const [fullscreen, setFullscreen] = useState(false);
  const previewRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!fullscreen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setFullscreen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [fullscreen]);

  useEffect(() => {
    let cancelled = false;
    renderMarkdown(src).then((h) => {
      if (!cancelled) setHtml(h);
    });
    try {
      localStorage.setItem(STORE_KEY, src);
    } catch {
      /* non-persistent is fine */
    }
    return () => {
      cancelled = true;
    };
  }, [src]);

  // Paste an image? Not this tab's job — but paste of text just fills the box
  // naturally via the textarea.

  const sendToClipboard = async () => {
    if (!src.trim()) return;
    try {
      await api.post("/api/clipboard", { content: src, format: "markdown" });
      toast("Sent to the shared clipboard — open Clipboard on another device", "success");
    } catch (e) {
      toast((e as Error).message, "error");
    }
  };

  return (
    <section class="tab-panel">
      <div class="panel-head">
        <h2>Markdown</h2>
        <div class="panel-tools">
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
          <button class="btn btn-primary" onClick={sendToClipboard}>
            Send to devices
          </button>
        </div>
      </div>

      <div class="md-split">
        <textarea
          class="md-input"
          placeholder="Paste Markdown here…"
          value={src}
          onInput={(e) => setSrc((e.target as HTMLTextAreaElement).value)}
          spellcheck={false}
        />
        <div
          class="md-preview markdown-body"
          ref={previewRef}
          dangerouslySetInnerHTML={{ __html: html }}
        />
      </div>

      <p class="hint">
        The draft is kept on this device. “Send to devices” pushes the raw Markdown
        to the shared Clipboard so you can pick it up elsewhere. Select any text in
        the preview and copy it as usual.
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
