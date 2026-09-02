import { useEffect, useState } from "preact/hooks";
import { api, type Transfer } from "../lib/api";
import { onEvent, onResync } from "../lib/events";
import { bytes, ago } from "../lib/format";
import {
  tasks,
  pauseUpload,
  resumeUpload,
  cancelUpload,
  removeTask,
} from "../lib/uploader";

export function TransfersTab() {
  const [history, setHistory] = useState<Transfer[]>([]);

  const load = () =>
    api
      .get<{ items: Transfer[] }>("/api/transfers?limit=200")
      .then((r) => setHistory(r.items || []))
      .catch(() => {});

  useEffect(() => {
    load();
    const offs = [
      onEvent("transfer.created", load),
      onEvent("transfer.completed", load),
      onEvent("transfer.failed", load),
      onEvent("transfer.removed", load),
      onResync(load),
    ];
    const iv = setInterval(load, 5000);
    return () => {
      offs.forEach((f) => f());
      clearInterval(iv);
    };
  }, []);

  const live = tasks.value;

  const removeHistory = async (id: string) => {
    await api.del(`/api/transfers/${id}`);
    setHistory((xs) => xs.filter((x) => x.id !== id));
  };

  return (
    <section class="tab-panel">
      <div class="panel-head">
        <h2>Transfers</h2>
      </div>

      {live.length > 0 && (
        <>
          <h3 class="subhead">Active</h3>
          <div class="xfer-list">
            {live.map((t) => {
              const pct = t.size ? Math.round((t.uploaded / t.size) * 100) : 0;
              return (
                <div key={t.key} class="xfer-row">
                  <div class="xfer-main">
                    <span class="xfer-name">{t.name}</span>
                    <span class="xfer-sub">
                      {bytes(t.uploaded)} / {bytes(t.size)} · {pct}% ·{" "}
                      <span class={`status status-${t.status}`}>{t.status}</span>
                      {t.error ? ` · ${t.error}` : ""}
                    </span>
                    <div class="progress">
                      <div
                        class={`progress-bar ${t.status === "error" ? "is-error" : ""}`}
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                  </div>
                  <div class="xfer-actions">
                    {t.status === "uploading" && t._upload && (
                      <button class="btn btn-xs" onClick={() => pauseUpload(t.key)}>
                        Pause
                      </button>
                    )}
                    {(t.status === "paused" || t.status === "error") && t._upload && (
                      <button class="btn btn-xs" onClick={() => resumeUpload(t.key)}>
                        {t.status === "error" ? "Retry" : "Resume"}
                      </button>
                    )}
                    <button class="btn btn-xs btn-danger" onClick={() => cancelUpload(t.key)}>
                      Cancel
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        </>
      )}

      <h3 class="subhead">History</h3>
      {history.length === 0 ? (
        <div class="state state-empty">
          <div class="state-icon">↕</div>
          No transfers recorded yet.
        </div>
      ) : (
        <div class="xfer-list">
          {history.map((h) => {
            const pct = h.size ? Math.round((h.offset / h.size) * 100) : 0;
            return (
              <div key={h.id} class="xfer-row">
                <div class="xfer-main">
                  <span class="xfer-name">
                    {h.file_id ? (
                      <a href={`/api/files/${h.file_id}/raw`} target="_blank" rel="noreferrer">
                        {h.name}
                      </a>
                    ) : (
                      h.name
                    )}
                  </span>
                  <span class="xfer-sub">
                    {bytes(h.size)} ·{" "}
                    <span class={`status status-${h.status}`}>{h.status}</span>
                    {h.status === "active" ? ` · ${pct}%` : ""} · {ago(h.updated_at)}
                  </span>
                </div>
                <div class="xfer-actions">
                  <button class="btn btn-xs" onClick={() => removeHistory(h.id)}>
                    Remove
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {live.some((t) => t.status === "done") && (
        <button
          class="btn"
          onClick={() => live.filter((t) => t.status === "done").forEach((t) => removeTask(t.key))}
        >
          Clear finished
        </button>
      )}
    </section>
  );
}
