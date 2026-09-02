import { tasks, pauseUpload, resumeUpload, cancelUpload } from "../lib/uploader";
import { bytes, speed, eta } from "../lib/format";

// A compact live tray of in-flight uploads, shown at the bottom of any tab that
// includes it. The full history lives in the Transfers tab.
export function UploadTray() {
  const list = tasks.value.filter((t) => t.status !== "done");
  if (list.length === 0) return null;
  return (
    <div class="upload-tray">
      {list.map((t) => {
        const pct = t.size > 0 ? Math.round((t.uploaded / t.size) * 100) : 0;
        return (
          <div key={t.key} class="tray-item">
            <div class="tray-row">
              <span class="tray-name" title={t.name}>
                {t.name}
              </span>
              <span class="tray-meta">
                {t.status === "error"
                  ? t.error || "failed"
                  : t.status === "paused"
                    ? "paused"
                    : `${bytes(t.uploaded)} / ${bytes(t.size)} · ${speed(t.speedBps)} · ${eta(
                        t.size - t.uploaded,
                        t.speedBps,
                      )}`}
              </span>
            </div>
            <div class="progress">
              <div
                class={`progress-bar ${t.status === "error" ? "is-error" : ""}`}
                style={{ width: `${pct}%` }}
              />
            </div>
            <div class="tray-actions">
              {t.status === "uploading" && t._upload && (
                <button class="btn btn-xs" onClick={() => pauseUpload(t.key)}>
                  Pause
                </button>
              )}
              {t.status === "paused" && (
                <button class="btn btn-xs" onClick={() => resumeUpload(t.key)}>
                  Resume
                </button>
              )}
              {t.status === "error" && t._upload && (
                <button class="btn btn-xs" onClick={() => resumeUpload(t.key)}>
                  Retry
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
  );
}
