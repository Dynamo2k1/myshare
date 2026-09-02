import { useRef, useState } from "preact/hooks";
import { api, type ClipboardItem } from "../lib/api";
import { useList } from "../lib/useList";
import { ago } from "../lib/format";
import { copyText } from "../lib/clipboard";
import { toast } from "../lib/toast";
import { ConfirmDialog } from "../components/Modal";

const pathFn = (q: string) => `/api/clipboard${q ? `?q=${encodeURIComponent(q)}` : ""}`;

export function ClipboardTab() {
  const list = useList<ClipboardItem>(pathFn, [
    "clipboard.created",
    "clipboard.updated",
    "clipboard.deleted",
    "clipboard.cleared",
  ]);
  const [draft, setDraft] = useState("");
  const [editing, setEditing] = useState<string>("");
  const [editVal, setEditVal] = useState("");
  const draftRef = useRef<HTMLTextAreaElement>(null);

  const autoGrow = (el: HTMLTextAreaElement | null) => {
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight + 2, window.innerHeight * 0.6) + "px";
  };
  const [clearing, setClearing] = useState(false);

  const add = async () => {
    const content = draft;
    if (!content.trim()) return;
    setDraft("");
    if (draftRef.current) draftRef.current.style.height = "";
    try {
      await api.post("/api/clipboard", { content });
    } catch (e) {
      toast((e as Error).message, "error");
      setDraft(content);
    }
  };

  const saveEdit = async (it: ClipboardItem) => {
    const content = editVal;
    setEditing("");
    if (content === it.content) return;
    try {
      await api.patch(`/api/clipboard/${it.id}`, { content });
    } catch (e) {
      toast((e as Error).message, "error");
    }
  };

  const togglePin = (it: ClipboardItem) =>
    api.patch(`/api/clipboard/${it.id}`, { pinned: !it.pinned }).catch(() => {});

  const del = async (id: string) => {
    await api.del(`/api/clipboard/${id}`);
    list.setItems((xs) => xs.filter((x) => x.id !== id));
  };

  return (
    <section class="tab-panel">
      <div class="panel-head">
        <h2>Clipboard</h2>
        <div class="panel-tools">
          <input
            class="search"
            type="search"
            placeholder="Search"
            value={list.query}
            onInput={(e) => list.setQuery((e.target as HTMLInputElement).value)}
          />
          {list.total > 0 && (
            <button class="btn btn-danger" onClick={() => setClearing(true)}>
              Clear all
            </button>
          )}
        </div>
      </div>

      <div class="composer">
        <textarea
          ref={draftRef}
          placeholder="Paste or type text to share across your devices…  (Ctrl/Cmd+Enter to add)"
          value={draft}
          onInput={(e) => {
            setDraft((e.target as HTMLTextAreaElement).value);
            autoGrow(e.target as HTMLTextAreaElement);
          }}
          onKeyDown={(e) => {
            if ((e.ctrlKey || e.metaKey) && e.key === "Enter") add();
          }}
        />
        <button class="btn btn-primary" onClick={add} disabled={!draft.trim()}>
          Add
        </button>
      </div>

      {list.loading && list.items.length === 0 && <div class="state">Loading…</div>}
      {!list.loading && list.items.length === 0 && (
        <div class="state state-empty">
          <div class="state-icon">📋</div>
          Nothing here yet. Add text above, or paste on any device.
        </div>
      )}

      <div class="card-list">
        {list.items.map((it) => (
          <div key={it.id} class={`clip-card ${it.pinned ? "is-pinned" : ""}`} data-id={it.id}>
            {editing === it.id ? (
              <textarea
                class="clip-edit"
                autoFocus
                value={editVal}
                onInput={(e) => setEditVal((e.target as HTMLTextAreaElement).value)}
                onBlur={() => saveEdit(it)}
                onKeyDown={(e) => {
                  if (e.key === "Escape") setEditing("");
                  if ((e.ctrlKey || e.metaKey) && e.key === "Enter") saveEdit(it);
                }}
              />
            ) : (
              <pre class={`clip-body fmt-${it.format}`}>{it.content}</pre>
            )}
            <div class="clip-foot">
              <span class="clip-time">
                {it.format !== "text" && <span class="tag">{it.format}</span>} {ago(it.created_at)}
              </span>
              <div class="clip-actions">
                <button class="btn btn-xs" onClick={() => copyText(it.content)}>
                  Copy
                </button>
                <button
                  class="btn btn-xs"
                  onClick={() => {
                    setEditing(it.id);
                    setEditVal(it.content);
                  }}
                >
                  Edit
                </button>
                <button class="btn btn-xs" onClick={() => togglePin(it)}>
                  {it.pinned ? "Unpin" : "Pin"}
                </button>
                <button class="btn btn-xs btn-danger" onClick={() => del(it.id)}>
                  Delete
                </button>
              </div>
            </div>
          </div>
        ))}
      </div>

      {list.hasMore && (
        <button class="btn load-more" onClick={list.loadMore}>
          Load more
        </button>
      )}

      {clearing && (
        <ConfirmDialog
          title="Clear clipboard"
          message="Delete every clipboard entry, including pinned ones?"
          confirmLabel="Clear all"
          onConfirm={async () => {
            await api.del("/api/clipboard");
            list.reload();
          }}
          onClose={() => setClearing(false)}
        />
      )}
    </section>
  );
}
