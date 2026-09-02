import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { api } from "../lib/api";
import { onEvent, onResync } from "../lib/events";
import { serveDir, ephemeral, currentDir } from "../lib/mode";
import { bytes, ago, extIcon } from "../lib/format";
import { copyText } from "../lib/clipboard";
import { toast } from "../lib/toast";
import { UploadTray } from "../components/UploadTray";
import { ConfirmDialog, Modal } from "../components/Modal";
import { startUpload } from "../lib/uploader";
import { showQR } from "../components/QRDialog";

interface Entry {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  mime?: string;
  mod_time: number;
}
interface Listing {
  dir: string;
  parent: string;
  entries: Entry[];
}

const rawURL = (p: string, dl = false) =>
  `/api/browse/raw?path=${encodeURIComponent(p)}${dl ? "&dl=1" : ""}`;

export function FilesBrowse() {
  const [cwd, setCwd] = useState<string>(() => {
    try {
      return sessionStorage.getItem("myshare_cwd") ?? "";
    } catch {
      return "";
    }
  });
  const [listing, setListing] = useState<Listing | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [confirmDel, setConfirmDel] = useState<Entry | null>(null);
  const [confirmAll, setConfirmAll] = useState(false);
  const [mkdirOpen, setMkdirOpen] = useState(false);
  const [mkdirName, setMkdirName] = useState("");
  const [renaming, setRenaming] = useState("");
  const [renameVal, setRenameVal] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);

  const load = useCallback(
    async (dir: string) => {
      setError("");
      try {
        const l = await api.get<Listing>(`/api/browse?path=${encodeURIComponent(dir)}`);
        setListing(l);
      } catch (e) {
        setError((e as Error).message || "Could not read that folder");
      }
    },
    [],
  );

  useEffect(() => {
    load(cwd);
    currentDir.value = cwd;
    try {
      sessionStorage.setItem("myshare_cwd", cwd);
    } catch {
      /* ignore */
    }
  }, [cwd, load]);

  useEffect(() => {
    const offs = [onEvent("browse.changed", () => load(cwd)), onResync(() => load(cwd))];
    return () => offs.forEach((f) => f());
  }, [cwd, load]);

  const go = (dir: string) => setCwd(dir);

  const uploadHere = (files: File[]) => {
    files.forEach((f) =>
      startUpload(f, "file", { endpoint: "/api/browse", dir: cwd }),
    );
  };

  const del = async (e: Entry) => {
    try {
      await api.del(`/api/browse?path=${encodeURIComponent(e.path)}`);
      setListing((l) => (l ? { ...l, entries: l.entries.filter((x) => x.path !== e.path) } : l));
    } catch (err) {
      toast((err as Error).message, "error");
    }
  };

  const deleteAll = async () => {
    try {
      const r = await api.del<{ deleted: number }>(
        `/api/browse/all?path=${encodeURIComponent(cwd)}`,
      );
      toast(`Deleted ${r.deleted} item${r.deleted === 1 ? "" : "s"}`, "success");
      load(cwd);
    } catch (err) {
      toast((err as Error).message, "error");
    }
  };

  const rename = async (e: Entry) => {
    const name = renameVal.trim();
    setRenaming("");
    if (!name || name === e.name) return;
    try {
      await api.patch(`/api/browse?path=${encodeURIComponent(e.path)}`, { name });
      load(cwd);
    } catch (err) {
      toast((err as Error).message, "error");
    }
  };

  const makeDir = async () => {
    const name = mkdirName.trim();
    if (!name) return;
    try {
      await api.post(`/api/browse/mkdir?path=${encodeURIComponent(cwd)}`, { name });
      setMkdirOpen(false);
      setMkdirName("");
      load(cwd);
    } catch (err) {
      toast((err as Error).message, "error");
    }
  };

  const crumbs = cwd ? cwd.split("/") : [];
  const entries = (listing?.entries ?? []).filter(
    (e) => !query || e.name.toLowerCase().includes(query.toLowerCase()),
  );

  return (
    <section class="tab-panel">
      <div class="panel-head">
        <h2>Files</h2>
        <div class="panel-tools">
          <input
            class="search"
            type="search"
            placeholder="Filter this folder"
            value={query}
            onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
          />
          <button class="btn" onClick={() => setMkdirOpen(true)}>
            New folder
          </button>
          <button class="btn btn-primary" onClick={() => fileInput.current?.click()}>
            Upload here
          </button>
          {entries.length > 0 && (
            <>
              <a class="btn" href="/api/browse/archive.zip" download>
                Download all (.zip)
              </a>
              <button class="btn btn-danger" onClick={() => setConfirmAll(true)}>
                Delete all here
              </button>
            </>
          )}
          <input
            ref={fileInput}
            type="file"
            multiple
            hidden
            onChange={(e) => {
              uploadHere(Array.from((e.target as HTMLInputElement).files || []));
              (e.target as HTMLInputElement).value = "";
            }}
          />
        </div>
      </div>

      <div class="folder-bar">
        <span class="folder-root" title={serveDir.value}>
          {ephemeral.value ? "📁 (temp)" : "📁"}
        </span>
        <button class="crumb" onClick={() => go("")}>
          root
        </button>
        {crumbs.map((c, i) => (
          <span key={i}>
            <span class="crumb-sep">/</span>
            <button class="crumb" onClick={() => go(crumbs.slice(0, i + 1).join("/"))}>
              {c}
            </button>
          </span>
        ))}
      </div>

      <div
        class="dropzone"
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault();
          uploadHere(Array.from(e.dataTransfer?.files || []));
        }}
      >
        Drop files to upload into <code>{cwd || "the folder root"}</code>.
      </div>

      {error && <div class="state state-error">{error}</div>}
      {!error && listing && entries.length === 0 && (
        <div class="state state-empty">
          <div class="state-icon">📂</div>
          This folder is empty.
        </div>
      )}

      {entries.length > 0 && (
        <div class="file-table">
          <div class="ft-head">
            <span>Name</span>
            <span class="col-size">Size</span>
            <span class="col-time">Modified</span>
            <span class="col-actions" />
          </div>
          {cwd && (
            <div class="ft-row">
              <span class="ft-name">
                <span class="ft-icon">↩</span>
                <button class="ft-link" onClick={() => go(listing!.parent)}>
                  .. (up)
                </button>
              </span>
              <span class="col-size" />
              <span class="col-time" />
              <span class="col-actions" />
            </div>
          )}
          {entries.map((e) => (
            <div key={e.path} class="ft-row">
              <span class="ft-name">
                <span class="ft-icon">{e.is_dir ? "📁" : extIcon(e.mime || "", e.name)}</span>
                {renaming === e.path ? (
                  <input
                    class="rename-input"
                    autoFocus
                    value={renameVal}
                    onInput={(ev) => setRenameVal((ev.target as HTMLInputElement).value)}
                    onBlur={() => rename(e)}
                    onKeyDown={(ev) => {
                      if (ev.key === "Enter") rename(e);
                      if (ev.key === "Escape") setRenaming("");
                    }}
                  />
                ) : e.is_dir ? (
                  <button class="ft-link" onClick={() => go(e.path)}>
                    {e.name}
                  </button>
                ) : (
                  <a class="ft-link" href={rawURL(e.path)} target="_blank" rel="noreferrer">
                    {e.name}
                  </a>
                )}
              </span>
              <span class="col-size mono">{e.is_dir ? "—" : bytes(e.size)}</span>
              <span class="col-time">{ago(e.mod_time)}</span>
              <span class="col-actions">
                {!e.is_dir && (
                  <a class="btn btn-xs" href={rawURL(e.path, true)} download={e.name}>
                    Download
                  </a>
                )}
                <button
                  class="btn btn-xs"
                  onClick={() => {
                    setRenaming(e.path);
                    setRenameVal(e.name);
                  }}
                >
                  Rename
                </button>
                {!e.is_dir && (
                  <button
                    class="btn btn-xs"
                    onClick={() => copyText(location.origin + rawURL(e.path))}
                  >
                    Link
                  </button>
                )}
                {!e.is_dir && (
                  <button
                    class="btn btn-xs"
                    onClick={() => showQR(location.origin + rawURL(e.path), "Scan to open on your phone")}
                  >
                    QR
                  </button>
                )}
                <button class="btn btn-xs btn-danger" onClick={() => setConfirmDel(e)}>
                  Delete
                </button>
              </span>
            </div>
          ))}
        </div>
      )}

      <UploadTray />

      {confirmDel && (
        <ConfirmDialog
          title={confirmDel.is_dir ? "Delete folder" : "Delete file"}
          message={`Permanently delete “${confirmDel.name}”${
            confirmDel.is_dir ? " and everything in it" : ""
          }? This removes the real ${confirmDel.is_dir ? "folder" : "file"} from disk.`}
          onConfirm={() => del(confirmDel)}
          onClose={() => setConfirmDel(null)}
        />
      )}
      {confirmAll && (
        <ConfirmDialog
          title="Delete everything in this folder"
          message={`Permanently delete all ${entries.length} item${
            entries.length === 1 ? "" : "s"
          } in “${cwd || "root"}” from disk? This cannot be undone.`}
          confirmLabel="Delete all"
          onConfirm={deleteAll}
          onClose={() => setConfirmAll(false)}
        />
      )}
      {mkdirOpen && (
        <Modal title="New folder" onClose={() => setMkdirOpen(false)}>
          <input
            class="se-title"
            autoFocus
            placeholder="Folder name"
            value={mkdirName}
            onInput={(e) => setMkdirName((e.target as HTMLInputElement).value)}
            onKeyDown={(e) => e.key === "Enter" && makeDir()}
          />
          <div class="modal-actions">
            <button class="btn" onClick={() => setMkdirOpen(false)}>
              Cancel
            </button>
            <button class="btn btn-primary" onClick={makeDir}>
              Create
            </button>
          </div>
        </Modal>
      )}
    </section>
  );
}
