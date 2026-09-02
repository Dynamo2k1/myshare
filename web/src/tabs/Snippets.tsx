import { useEffect, useRef, useState } from "preact/hooks";
import { api, type Snippet } from "../lib/api";
import { useList } from "../lib/useList";
import { ago } from "../lib/format";
import { copyText } from "../lib/clipboard";
import { toast } from "../lib/toast";
import { ConfirmDialog } from "../components/Modal";

const LANGS = [
  "plaintext", "bash", "shell", "python", "javascript", "typescript", "json",
  "html", "css", "sql", "yaml", "markdown", "go", "rust", "java", "c", "cpp",
];

const pathFn = (q: string) => `/api/snippets${q ? `?q=${encodeURIComponent(q)}` : ""}`;

export function SnippetsTab() {
  const list = useList<Snippet>(pathFn, ["snippet.created", "snippet.updated", "snippet.deleted"]);
  const [editor, setEditor] = useState<Partial<Snippet> | null>(null);
  const [confirm, setConfirm] = useState<Snippet | null>(null);

  const save = async () => {
    if (!editor) return;
    const body = {
      title: editor.title || "",
      content: editor.content || "",
      language: editor.language || "plaintext",
    };
    if (!body.content.trim()) {
      toast("Nothing to save", "error");
      return;
    }
    try {
      if (editor.id) await api.patch(`/api/snippets/${editor.id}`, body);
      else await api.post("/api/snippets", body);
      setEditor(null);
    } catch (e) {
      toast((e as Error).message, "error");
    }
  };

  const del = async (s: Snippet) => {
    await api.del(`/api/snippets/${s.id}`);
    list.setItems((xs) => xs.filter((x) => x.id !== s.id));
  };

  return (
    <section class="tab-panel">
      <div class="panel-head">
        <h2>Snippets</h2>
        <div class="panel-tools">
          <input
            class="search"
            type="search"
            placeholder="Search"
            value={list.query}
            onInput={(e) => list.setQuery((e.target as HTMLInputElement).value)}
          />
          <button
            class="btn btn-primary"
            onClick={() => setEditor({ title: "", content: "", language: "plaintext" })}
          >
            New snippet
          </button>
        </div>
      </div>

      {editor && (
        <div class="snippet-editor">
          <div class="se-row">
            <input
              class="se-title"
              placeholder="Title"
              value={editor.title}
              onInput={(e) => setEditor({ ...editor, title: (e.target as HTMLInputElement).value })}
            />
            <select
              value={editor.language}
              onChange={(e) => setEditor({ ...editor, language: (e.target as HTMLSelectElement).value })}
            >
              {LANGS.map((l) => (
                <option key={l} value={l}>
                  {l}
                </option>
              ))}
            </select>
          </div>
          <textarea
            class="se-body"
            placeholder="Paste code…"
            value={editor.content}
            onInput={(e) => setEditor({ ...editor, content: (e.target as HTMLTextAreaElement).value })}
          />
          <div class="modal-actions">
            <button class="btn" onClick={() => setEditor(null)}>
              Cancel
            </button>
            <button class="btn btn-primary" onClick={save}>
              Save
            </button>
          </div>
        </div>
      )}

      {!list.loading && list.items.length === 0 && !editor && (
        <div class="state state-empty">
          <div class="state-icon">‹›</div>
          No snippets yet.
        </div>
      )}

      <div class="card-list">
        {list.items.map((s) => (
          <SnippetCard
            key={s.id}
            s={s}
            onEdit={() => setEditor(s)}
            onDelete={() => setConfirm(s)}
            onDuplicate={async () => {
              await api.post(`/api/snippets/${s.id}/duplicate`);
            }}
            onPin={() => api.patch(`/api/snippets/${s.id}`, { pinned: !s.pinned })}
          />
        ))}
      </div>

      {list.hasMore && (
        <button class="btn load-more" onClick={list.loadMore}>
          Load more
        </button>
      )}

      {confirm && (
        <ConfirmDialog
          title="Delete snippet"
          message={`Delete “${confirm.title || "untitled"}”?`}
          onConfirm={() => del(confirm)}
          onClose={() => setConfirm(null)}
        />
      )}
    </section>
  );
}

function SnippetCard({
  s,
  onEdit,
  onDelete,
  onDuplicate,
  onPin,
}: {
  s: Snippet;
  onEdit: () => void;
  onDelete: () => void;
  onDuplicate: () => void;
  onPin: () => void;
}) {
  const codeRef = useRef<HTMLElement>(null);
  const [highlighted, setHighlighted] = useState(false);

  useEffect(() => {
    if (highlighted || s.language === "plaintext" || !codeRef.current) return;
    let cancelled = false;
    (async () => {
      try {
        const hljs = (await import("highlight.js/lib/common")).default;
        if (cancelled || !codeRef.current) return;
        codeRef.current.innerHTML = hljs.highlightAuto(s.content, [s.language]).value;
        setHighlighted(true);
      } catch {
        /* leave as plain text */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [s.content, s.language, highlighted]);

  return (
    <div class={`snip-card ${s.pinned ? "is-pinned" : ""}`}>
      <div class="snip-head">
        <span class="snip-title">{s.title || "untitled"}</span>
        <span class="tag">{s.language}</span>
      </div>
      <pre class="snip-body">
        <code ref={codeRef}>{s.content}</code>
      </pre>
      <div class="snip-foot">
        <span class="clip-time">{ago(s.updated_at)}</span>
        <div class="clip-actions">
          <button class="btn btn-xs" onClick={() => copyText(s.content)}>
            Copy
          </button>
          <button class="btn btn-xs" onClick={onEdit}>
            Edit
          </button>
          <button class="btn btn-xs" onClick={onDuplicate}>
            Duplicate
          </button>
          <button class="btn btn-xs" onClick={onPin}>
            {s.pinned ? "Unpin" : "Pin"}
          </button>
          <button class="btn btn-xs btn-danger" onClick={onDelete}>
            Delete
          </button>
        </div>
      </div>
    </div>
  );
}
