import { useRef, useState } from "preact/hooks";
import { api, rawURL, type FileItem } from "../lib/api";
import { useList } from "../lib/useList";
import { ago, bytes } from "../lib/format";
import { copyText, copyImage, canCopyImage } from "../lib/clipboard";
import { toast } from "../lib/toast";
import { startUpload } from "../lib/uploader";
import { UploadTray } from "../components/UploadTray";
import { ConfirmDialog } from "../components/Modal";
import { showQR } from "../components/QRDialog";

const pathFn = (q: string) =>
  `/api/files?kind=screenshot&sort=created&dir=desc${q ? `&q=${encodeURIComponent(q)}` : ""}`;

export function ScreenshotsTab() {
  const list = useList<FileItem>(pathFn, ["file.created", "file.updated", "file.deleted"]);
  const fileInput = useRef<HTMLInputElement>(null);
  const [confirm, setConfirm] = useState<FileItem | null>(null);
  const secure = canCopyImage();

  const copyToClipboard = async (f: FileItem) => {
    try {
      const res = await fetch(rawURL(f.id));
      const blob = await res.blob();
      const ok = await copyImage(blob);
      if (!ok) toast("Image copy needs HTTPS — use --tls, or download instead", "error");
    } catch {
      toast("Couldn't copy image", "error");
    }
  };

  const del = async (f: FileItem) => {
    await api.del(`/api/files/${f.id}`);
    list.setItems((xs) => xs.filter((x) => x.id !== f.id));
  };

  return (
    <section class="tab-panel">
      <div class="panel-head">
        <h2>Screenshots</h2>
        <div class="panel-tools">
          <input
            class="search"
            type="search"
            placeholder="Search"
            value={list.query}
            onInput={(e) => list.setQuery((e.target as HTMLInputElement).value)}
          />
          <button class="btn btn-primary" onClick={() => fileInput.current?.click()}>
            Add image
          </button>
          <input
            ref={fileInput}
            type="file"
            accept="image/*"
            multiple
            hidden
            onChange={(e) => {
              Array.from((e.target as HTMLInputElement).files || []).forEach((f) =>
                startUpload(f, "screenshot"),
              );
              (e.target as HTMLInputElement).value = "";
            }}
          />
        </div>
      </div>

      <div class="paste-hint">
        Press <kbd>Ctrl</kbd>+<kbd>V</kbd> anywhere to upload a screenshot from your clipboard.
        {!secure && (
          <span class="hint"> · Copying an image back to the clipboard needs HTTPS (start with --tls).</span>
        )}
      </div>

      {list.loading && list.items.length === 0 && <div class="state">Loading…</div>}
      {!list.loading && list.items.length === 0 && (
        <div class="state state-empty">
          <div class="state-icon">🖼️</div>
          No screenshots yet. Take one and paste it here.
        </div>
      )}

      <div class="shot-grid">
        {list.items.map((f) => (
          <figure key={f.id} class="shot" data-id={f.id}>
            <a href={rawURL(f.id)} target="_blank" rel="noreferrer">
              <img src={rawURL(f.id)} alt={f.name} loading="lazy" />
            </a>
            <figcaption>
              <span class="shot-name" title={f.name}>
                {f.name}
              </span>
              <span class="shot-meta">
                {bytes(f.size)} · {ago(f.created_at)}
              </span>
              <div class="shot-actions">
                <a class="btn btn-xs" href={rawURL(f.id, true)} download={f.name}>
                  Download
                </a>
                {secure && (
                  <button class="btn btn-xs" onClick={() => copyToClipboard(f)}>
                    Copy image
                  </button>
                )}
                <button
                  class="btn btn-xs"
                  onClick={() => copyText(location.origin + rawURL(f.id))}
                >
                  Link
                </button>
                <button
                  class="btn btn-xs"
                  onClick={() => showQR(location.origin + rawURL(f.id), "Scan to view on your phone")}
                >
                  QR
                </button>
                <button class="btn btn-xs btn-danger" onClick={() => setConfirm(f)}>
                  Delete
                </button>
              </div>
            </figcaption>
          </figure>
        ))}
      </div>

      {list.hasMore && (
        <button class="btn load-more" onClick={list.loadMore}>
          Load more
        </button>
      )}

      <UploadTray />
      {confirm && (
        <ConfirmDialog
          title="Delete screenshot"
          message={`Delete “${confirm.name}”?`}
          onConfirm={() => del(confirm)}
          onClose={() => setConfirm(null)}
        />
      )}
    </section>
  );
}
