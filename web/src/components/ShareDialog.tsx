import { useEffect, useState } from "preact/hooks";
import { Modal } from "./Modal";
import { api, type FileItem, type Share } from "../lib/api";
import { copyText } from "../lib/clipboard";
import { toast } from "../lib/toast";
import { showQR } from "./QRDialog";

const EXPIRY = [
  { label: "Never", sec: 0 },
  { label: "1 hour", sec: 3600 },
  { label: "24 hours", sec: 86400 },
  { label: "7 days", sec: 604800 },
];

export function ShareDialog({ file, onClose }: { file: FileItem; onClose: () => void }) {
  const [shares, setShares] = useState<Share[]>([]);
  const [expiry, setExpiry] = useState(0);
  const [oneTime, setOneTime] = useState(false);
  const [maxDl, setMaxDl] = useState<string>("");
  const [lastURL, setLastURL] = useState<string>("");
  const [busy, setBusy] = useState(false);

  const load = () =>
    api
      .get<{ items: Share[] }>(`/api/shares?file_id=${file.id}`)
      .then((r) => setShares(r.items || []))
      .catch(() => {});

  useEffect(() => {
    load();
  }, [file.id]);

  const create = async () => {
    setBusy(true);
    try {
      const body: Record<string, unknown> = { file_id: file.id, one_time: oneTime };
      if (expiry > 0) body.expires_in_sec = expiry;
      const n = parseInt(maxDl, 10);
      if (!Number.isNaN(n) && n > 0) body.max_downloads = n;
      const s = await api.post<Share>("/api/shares", body);
      setLastURL(s.url || "");
      if (s.url) await copyText(s.url);
      load();
    } catch (e) {
      toast((e as Error).message, "error");
    } finally {
      setBusy(false);
    }
  };

  const revoke = async (id: string) => {
    await api.del(`/api/shares/${id}`);
    load();
  };

  return (
    <Modal title={`Share “${file.name}”`} onClose={onClose}>
      <div class="share-form">
        <label>
          Expires
          <select value={String(expiry)} onChange={(e) => setExpiry(Number((e.target as HTMLSelectElement).value))}>
            {EXPIRY.map((o) => (
              <option key={o.sec} value={String(o.sec)}>
                {o.label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Max downloads
          <input
            type="number"
            min="0"
            placeholder="unlimited"
            value={maxDl}
            onInput={(e) => setMaxDl((e.target as HTMLInputElement).value)}
          />
        </label>
        <label class="check">
          <input type="checkbox" checked={oneTime} onChange={(e) => setOneTime((e.target as HTMLInputElement).checked)} />
          One-time link (revokes after first download)
        </label>
        <button class="btn btn-primary" onClick={create} disabled={busy}>
          {busy ? "Creating…" : "Create link"}
        </button>
      </div>

      {lastURL && (
        <div class="share-new">
          <code class="share-url">{lastURL}</code>
          <button class="btn btn-sm" onClick={() => copyText(lastURL)}>
            Copy
          </button>
          <button class="btn btn-sm" onClick={() => showQR(lastURL)}>
            QR
          </button>
        </div>
      )}

      {shares.length > 0 && (
        <div class="share-list">
          <h4>Active links</h4>
          {shares.map((s) => (
            <div key={s.id} class="share-row">
              <span class="mono small">
                #{s.id.slice(-6)} · {s.downloads}
                {s.max_downloads ? `/${s.max_downloads}` : ""} dl
                {s.one_time ? " · one-time" : ""}
                {s.expires_at ? ` · expires ${new Date(s.expires_at * 1000).toLocaleString()}` : ""}
              </span>
              <button class="btn btn-sm btn-danger" onClick={() => revoke(s.id)}>
                Revoke
              </button>
            </div>
          ))}
          <p class="hint">The link text is shown only once, when created. Revoke and recreate if you lost it.</p>
        </div>
      )}
    </Modal>
  );
}
