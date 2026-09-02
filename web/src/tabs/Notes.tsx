import { useState } from "preact/hooks";
import { api, type Note } from "../lib/api";
import { useList } from "../lib/useList";
import { ago } from "../lib/format";
import { copyText } from "../lib/clipboard";
import { toast } from "../lib/toast";
import { ConfirmDialog } from "../components/Modal";

const pathFn = (q: string) => `/api/notes${q ? `?q=${encodeURIComponent(q)}` : ""}`;

export function NotesTab() {
  const list = useList<Note>(pathFn, ["note.created", "note.updated", "note.deleted"]);
  const [editor, setEditor] = useState<Partial<Note> | null>(null);
  const [confirm, setConfirm] = useState<Note | null>(null);

  const save = async () => {
    if (!editor) return;
    const body = { title: editor.title || "", content: editor.content || "" };
    if (!body.title.trim() && !body.content.trim()) {
      setEditor(null);
      return;
    }
    try {
      if (editor.id) await api.patch(`/api/notes/${editor.id}`, body);
      else await api.post("/api/notes", body);
      setEditor(null);
    } catch (e) {
      toast((e as Error).message, "error");
    }
  };

  const del = async (n: Note) => {
    await api.del(`/api/notes/${n.id}`);
    list.setItems((xs) => xs.filter((x) => x.id !== n.id));
  };

  return (
    <section class="tab-panel">
      <div class="panel-head">
        <h2>Notes</h2>
        <div class="panel-tools">
          <input
            class="search"
            type="search"
            placeholder="Search"
            value={list.query}
            onInput={(e) => list.setQuery((e.target as HTMLInputElement).value)}
          />
          <button class="btn btn-primary" onClick={() => setEditor({ title: "", content: "" })}>
            New note
          </button>
        </div>
      </div>

      {editor && (
        <div class="snippet-editor">
          <input
            class="se-title"
            placeholder="Title"
            value={editor.title}
            onInput={(e) => setEditor({ ...editor, title: (e.target as HTMLInputElement).value })}
          />
          <textarea
            class="se-body"
            placeholder="Markdown supported…"
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
          <div class="state-icon">🗒️</div>
          No notes yet.
        </div>
      )}

      <div class="card-list">
        {list.items.map((n) => (
          <div key={n.id} class="note-card">
            <div class="snip-head">
              <span class="snip-title">{n.title || "untitled"}</span>
            </div>
            <pre class="note-body">{n.content}</pre>
            <div class="snip-foot">
              <span class="clip-time">{ago(n.updated_at)}</span>
              <div class="clip-actions">
                <button class="btn btn-xs" onClick={() => copyText(n.content)}>
                  Copy
                </button>
                <button class="btn btn-xs" onClick={() => setEditor(n)}>
                  Edit
                </button>
                <button class="btn btn-xs btn-danger" onClick={() => setConfirm(n)}>
                  Delete
                </button>
              </div>
            </div>
          </div>
        ))}
      </div>

      {confirm && (
        <ConfirmDialog
          title="Delete note"
          message={`Delete “${confirm.title || "untitled"}”?`}
          onConfirm={() => del(confirm)}
          onClose={() => setConfirm(null)}
        />
      )}
    </section>
  );
}
