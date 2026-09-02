import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { api, rawURL, type FileItem } from "../lib/api";
import { useList } from "../lib/useList";
import { bytes, ago, extIcon } from "../lib/format";
import { copyText } from "../lib/clipboard";
import { toast } from "../lib/toast";
import { startUpload } from "../lib/uploader";
import { UploadTray } from "../components/UploadTray";
import { ConfirmDialog } from "../components/Modal";
import { ShareDialog } from "../components/ShareDialog";
import { showQR } from "../components/QRDialog";

type Sort = "created" | "name" | "size" | "updated";

export function FilesTab() {
  const [sort, setSort] = useState<Sort>("created");
  const [desc, setDesc] = useState(true);
  const pathFn = useMemo(
    () => (q: string) =>
      `/api/files?sort=${sort}&dir=${desc ? "desc" : "asc"}` +
      (q ? `&q=${encodeURIComponent(q)}` : ""),
    [sort, desc],
  );
  const list = useList<FileItem>(pathFn, ["file.created", "file.updated", "file.deleted"]);
  const fileInput = useRef<HTMLInputElement>(null);
  const [confirm, setConfirm] = useState<FileItem | null>(null);
  const [share, setShare] = useState<FileItem | null>(null);
  const [renaming, setRenaming] = useState<string>("");
  const [renameVal, setRenameVal] = useState("");
  const [confirmAll, setConfirmAll] = useState(false);

  const deleteAll = async () => {
    try {
      const r = await api.del<{ deleted: number }>("/api/files");
      list.setItems(() => []);
      list.reload();
      toast(`Deleted ${r.deleted} file${r.deleted === 1 ? "" : "s"}`, "success");
    } catch (e) {
      toast((e as Error).message, "error");
    }
  };

  useEffect(() => {
    list.reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sort, desc]);

  const del = async (f: FileItem) => {
    try {
      await api.del(`/api/files/${f.id}`);
      list.setItems((xs) => xs.filter((x) => x.id !== f.id));
      toast("Deleted", "success");
    } catch (e) {
      toast((e as Error).message, "error");
    }
  };

  const rename = async (f: FileItem) => {
    const name = renameVal.trim();
    setRenaming("");
    if (!name || name === f.name) return;
    try {
      const updated = await api.patch<FileItem>(`/api/files/${f.id}`, { name });
      list.setItems((xs) => xs.map((x) => (x.id === f.id ? updated : x)));
    } catch (e) {
      toast((e as Error).message, "error");
    }
  };

  return (
    <section class="tab-panel">
      <div class="panel-head">
        <h2>Files</h2>
        <div class="panel-tools">
          <input
            class="search"
            type="search"
            placeholder="Search files"
            value={list.query}
            onInput={(e) => list.setQuery((e.target as HTMLInputElement).value)}
          />
          <select value={sort} onChange={(e) => setSort((e.target as HTMLSelectElement).value as Sort)}>
            <option value="created">Newest</option>
            <option value="name">Name</option>
            <option value="size">Size</option>
            <option value="updated">Modified</option>
          </select>
          <button class="btn btn-icon" title="Toggle direction" onClick={() => setDesc((d) => !d)}>
            {desc ? "↓" : "↑"}
          </button>
          <button class="btn btn-primary" onClick={() => fileInput.current?.click()}>
            Upload
          </button>
          {list.total > 0 && (
            <>
              <a class="btn" href="/api/files/archive.zip" download>
                Download all (.zip)
              </a>
              <button class="btn btn-danger" onClick={() => setConfirmAll(true)}>
                Delete all
              </button>
            </>
          )}
          <input
            ref={fileInput}
            type="file"
            multiple
            hidden
            onChange={(e) => {
              const files = Array.from((e.target as HTMLInputElement).files || []);
              files.forEach((f) => startUpload(f, "file"));
              (e.target as HTMLInputElement).value = "";
            }}
          />
        </div>
      </div>

      <div
        class="dropzone"
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault();
          Array.from(e.dataTransfer?.files || []).forEach((f) => startUpload(f, "file"));
        }}
      >
        Drag files here, or use the Upload button. Large files upload resumably.
      </div>

      {list.error && <div class="state state-error">{list.error}</div>}
      {list.loading && list.items.length === 0 && <div class="state">Loading…</div>}
      {!list.loading && list.items.length === 0 && !list.error && (
        <div class="state state-empty">
          <div class="state-icon">📁</div>
          No files yet. Drop something here or paste a screenshot.
        </div>
      )}

      {list.items.length > 0 && (
        <div class="file-table">
          <div class="ft-head">
            <span>Name</span>
            <span class="col-size">Size</span>
            <span class="col-time">Added</span>
            <span class="col-actions" />
          </div>
          {list.items.map((f) => (
            <div key={f.id} class="ft-row" data-id={f.id}>
              <span class="ft-name">
                <span class="ft-icon">{extIcon(f.mime, f.name)}</span>
                {renaming === f.id ? (
                  <input
                    class="rename-input"
                    autoFocus
                    value={renameVal}
                    onInput={(e) => setRenameVal((e.target as HTMLInputElement).value)}
                    onBlur={() => rename(f)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") rename(f);
                      if (e.key === "Escape") setRenaming("");
                    }}
                  />
                ) : (
                  <a href={rawURL(f.id)} target="_blank" rel="noreferrer" class="ft-link">
                    {f.name}
                  </a>
                )}
              </span>
              <span class="col-size mono">{bytes(f.size)}</span>
              <span class="col-time" title={new Date(f.created_at * 1000).toLocaleString()}>
                {ago(f.created_at)}
              </span>
              <span class="col-actions">
                <a class="btn btn-xs" href={rawURL(f.id, true)} download={f.name}>
                  Download
                </a>
                <button
                  class="btn btn-xs"
                  onClick={() => {
                    setRenaming(f.id);
                    setRenameVal(f.name);
                  }}
                >
                  Rename
                </button>
                <button class="btn btn-xs" onClick={() => setShare(f)}>
                  Share
                </button>
                <button
                  class="btn btn-xs"
                  onClick={() => copyText(location.origin + rawURL(f.id))}
                  title="Copy direct link"
                >
                  Link
                </button>
                <button
                  class="btn btn-xs"
                  onClick={() => showQR(location.origin + rawURL(f.id), "Scan to open on another device")}
                >
                  QR
                </button>
                <button class="btn btn-xs btn-danger" onClick={() => setConfirm(f)}>
                  Delete
                </button>
              </span>
            </div>
          ))}
        </div>
      )}

      {list.hasMore && (
        <button class="btn load-more" onClick={list.loadMore}>
          Load more ({list.items.length} / {list.total})
        </button>
      )}

      <UploadTray />

      {confirm && (
        <ConfirmDialog
          title="Delete file"
          message={`Delete “${confirm.name}”? This removes it from storage.`}
          onConfirm={() => del(confirm)}
          onClose={() => setConfirm(null)}
        />
      )}
      {confirmAll && (
        <ConfirmDialog
          title="Delete all files"
          message={`Permanently delete all ${list.total} file${
            list.total === 1 ? "" : "s"
          }? This cannot be undone.`}
          confirmLabel="Delete all"
          onConfirm={deleteAll}
          onClose={() => setConfirmAll(false)}
        />
      )}
      {share && <ShareDialog file={share} onClose={() => setShare(null)} />}
    </section>
  );
}
