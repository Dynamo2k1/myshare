import { useEffect, useState } from "preact/hooks";
import { api, type Status } from "../lib/api";
import { bytes } from "../lib/format";
import { themeChoice, type ThemeChoice } from "../lib/theme";
import { showQR } from "../components/QRDialog";

export function SettingsTab() {
  const [st, setSt] = useState<Status | null>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    api
      .get<Status>("/api/status")
      .then(setSt)
      .catch((e) => setErr((e as Error).message));
    const iv = setInterval(() => api.get<Status>("/api/status").then(setSt).catch(() => {}), 5000);
    return () => clearInterval(iv);
  }, []);

  const lanURL = st ? `${location.protocol}//${location.hostname}:${st.port}` : location.origin;

  return (
    <section class="tab-panel">
      <div class="panel-head">
        <h2>Settings</h2>
      </div>

      <div class="settings-grid">
        <div class="card">
          <h3>Appearance</h3>
          <div class="seg">
            {(["light", "dark", "system"] as ThemeChoice[]).map((c) => (
              <button
                key={c}
                class={`seg-btn ${themeChoice.value === c ? "is-active" : ""}`}
                onClick={() => (themeChoice.value = c)}
              >
                {c}
              </button>
            ))}
          </div>
        </div>

        <div class="card">
          <h3>Reach this device</h3>
          <p class="mono">{lanURL}</p>
          <button class="btn btn-sm" onClick={() => showQR(lanURL, "Open MyShare on your phone")}>
            Show QR
          </button>
          <p class="hint">
            Other devices can use this address only if the server was started with
            <code> --host 0.0.0.0</code>.
          </p>
        </div>

        {err && <div class="card state-error">{err}</div>}

        {st && (
          <>
            <div class="card">
              <h3>Server</h3>
              <dl class="kv">
                <dt>Version</dt>
                <dd class="mono">{st.version}</dd>
                <dt>Address</dt>
                <dd class="mono">
                  {st.host}:{st.port} {st.tls ? "(TLS)" : ""}
                </dd>
                <dt>Data directory</dt>
                <dd class="mono">{st.data_dir}</dd>
                <dt>Authentication</dt>
                <dd>{st.auth ? "enabled" : "off"}</dd>
                <dt>Connected clients</dt>
                <dd>{st.connected}</dd>
              </dl>
            </div>

            <div class="card">
              <h3>Storage</h3>
              <dl class="kv">
                <dt>Stored data</dt>
                <dd>
                  {bytes(st.stats.blob_bytes || 0)} in {st.stats.blob_count || 0} blobs
                </dd>
                <dt>Filesystem</dt>
                <dd>
                  {bytes(st.disk.used)} used · {bytes(st.disk.free)} free ({st.disk.fs_type})
                </dd>
                {st.max_storage > 0 && (
                  <>
                    <dt>Quota</dt>
                    <dd>
                      {bytes(st.stats.blob_bytes || 0)} / {bytes(st.max_storage)}
                      {st.storage_used_pct != null && ` (${st.storage_used_pct.toFixed(0)}%)`}
                    </dd>
                  </>
                )}
                {st.max_file_size > 0 && (
                  <>
                    <dt>Max file size</dt>
                    <dd>{bytes(st.max_file_size)}</dd>
                  </>
                )}
                {st.disk.unsafe_wal && (
                  <>
                    <dt>Note</dt>
                    <dd class="warn">
                      Data dir is on {st.disk.fs_type}; SQLite is using rollback-journal mode.
                    </dd>
                  </>
                )}
              </dl>
            </div>

            <div class="card">
              <h3>Contents</h3>
              <dl class="kv">
                <dt>Files</dt>
                <dd>{st.stats.files}</dd>
                <dt>Screenshots</dt>
                <dd>{st.stats.screenshots}</dd>
                <dt>Clipboard</dt>
                <dd>{st.stats.clipboard}</dd>
                <dt>Snippets</dt>
                <dd>{st.stats.snippets}</dd>
                <dt>Notes</dt>
                <dd>{st.stats.notes}</dd>
                <dt>Active shares</dt>
                <dd>{st.stats.shares}</dd>
              </dl>
            </div>
          </>
        )}
      </div>
    </section>
  );
}
