// Resumable upload manager built on tus-js-client. Small files (< threshold) go
// through the plain POST /api/files endpoint; larger ones use tus so they pause,
// resume, survive a refresh, and never restart from zero.

import type * as TusNS from "tus-js-client";
import { signal } from "@preact/signals";
import { clientId } from "./api";
import { toast } from "./toast";

// tus-js-client is only needed for large uploads, so it is loaded on demand.
// Small files use the plain XHR path below and never pull it in.
let tusMod: typeof import("tus-js-client") | null = null;
const loadTus = async () => (tusMod ??= await import("tus-js-client"));

const TUS_THRESHOLD = 8 * 1024 * 1024; // 8 MiB

export interface UploadTask {
  key: string;
  name: string;
  size: number;
  uploaded: number;
  status: "queued" | "uploading" | "paused" | "done" | "error";
  speedBps: number;
  error?: string;
  kind: "file" | "screenshot";
  _upload?: TusNS.Upload;
  _xhr?: XMLHttpRequest;
  _lastT?: number;
  _lastB?: number;
}

export const tasks = signal<UploadTask[]>([]);

function upsert(t: UploadTask) {
  const i = tasks.value.findIndex((x) => x.key === t.key);
  if (i === -1) tasks.value = [t, ...tasks.value];
  else {
    const copy = tasks.value.slice();
    copy[i] = { ...copy[i], ...t };
    tasks.value = copy;
  }
}
function patch(key: string, p: Partial<UploadTask>) {
  const i = tasks.value.findIndex((x) => x.key === key);
  if (i === -1) return;
  const copy = tasks.value.slice();
  copy[i] = { ...copy[i], ...p };
  tasks.value = copy;
}
export function removeTask(key: string) {
  tasks.value = tasks.value.filter((x) => x.key !== key);
}

function trackSpeed(key: string, uploaded: number) {
  const t = tasks.value.find((x) => x.key === key);
  if (!t) return;
  const now = performance.now();
  if (t._lastT != null && t._lastB != null) {
    const dt = (now - t._lastT) / 1000;
    if (dt > 0.4) {
      const bps = (uploaded - t._lastB) / dt;
      patch(key, { speedBps: bps, _lastT: now, _lastB: uploaded, uploaded });
      return;
    }
  } else {
    patch(key, { _lastT: now, _lastB: uploaded, uploaded });
    return;
  }
  patch(key, { uploaded });
}

// dest picks where the upload lands. Default: the personal store. In directory
// mode the Files tab passes { endpoint: "/api/browse", dir: <cwd> } to drop the
// file into a real folder instead.
export interface UploadDest {
  endpoint?: string; // "/api/browse" for directory mode
  dir?: string; // target subdir (directory mode)
}

export function startUpload(
  file: File,
  kind: "file" | "screenshot" = "file",
  dest: UploadDest = {},
) {
  const key = `${file.name}:${file.size}:${file.lastModified}:${Math.random().toString(36).slice(2)}`;
  const base: UploadTask = {
    key,
    name: file.name || (kind === "screenshot" ? "screenshot.png" : "file"),
    size: file.size,
    uploaded: 0,
    status: "uploading",
    speedBps: 0,
    kind,
  };
  upsert(base);

  if (file.size < TUS_THRESHOLD) {
    directUpload(key, file, kind, dest);
  } else {
    tusUpload(key, file, kind, dest);
  }
  return key;
}

function directUpload(key: string, file: File, kind: string, dest: UploadDest) {
  const xhr = new XMLHttpRequest();
  patch(key, { _xhr: xhr });
  const fd = new FormData();
  fd.append("file", file, file.name);
  const url =
    dest.endpoint === "/api/browse"
      ? `/api/browse?path=${encodeURIComponent(dest.dir || "")}`
      : `/api/files?kind=${encodeURIComponent(kind)}`;
  xhr.open("POST", url);
  xhr.setRequestHeader("X-MyShare-Client", clientId);
  xhr.upload.onprogress = (e) => {
    if (e.lengthComputable) trackSpeed(key, e.loaded);
  };
  xhr.onload = () => {
    if (xhr.status >= 200 && xhr.status < 300) {
      patch(key, { status: "done", uploaded: file.size, speedBps: 0 });
      autoClear(key);
    } else {
      let msg = `Upload failed (${xhr.status})`;
      try {
        msg = JSON.parse(xhr.responseText).error || msg;
      } catch {
        /* keep default */
      }
      patch(key, { status: "error", error: msg });
      toast(msg, "error");
    }
  };
  xhr.onerror = () => {
    patch(key, { status: "error", error: "Network error" });
  };
  xhr.send(fd);
}

async function tusUpload(key: string, file: File, kind: string, dest: UploadDest) {
  const tus = await loadTus();
  const meta: Record<string, string> = {
    filename: file.name,
    filetype: file.type || "application/octet-stream",
    kind,
  };
  if (dest.endpoint === "/api/browse") meta.dir = dest.dir || "";
  const upload = new tus.Upload(file, {
    endpoint: "/api/tus/",
    retryDelays: [0, 1000, 3000, 5000, 10000, 20000],
    chunkSize: 16 * 1024 * 1024,
    removeFingerprintOnSuccess: true,
    metadata: meta,
    headers: { "X-MyShare-Client": clientId },
    onError(err) {
      patch(key, { status: "error", error: String((err as Error).message || err) });
      toast(
        "Upload interrupted — it can be resumed",
        "error",
        { label: "Resume", run: () => resumeUpload(key) },
      );
    },
    onProgress(sent) {
      trackSpeed(key, sent);
    },
    onSuccess() {
      patch(key, { status: "done", uploaded: file.size, speedBps: 0 });
      autoClear(key);
    },
  });
  patch(key, { _upload: upload });
  upload.findPreviousUploads().then((prev) => {
    if (prev.length) upload.resumeFromPreviousUpload(prev[0]);
    upload.start();
  });
}

export function pauseUpload(key: string) {
  const t = tasks.value.find((x) => x.key === key);
  if (!t) return;
  t._upload?.abort();
  patch(key, { status: "paused", speedBps: 0 });
}

export function resumeUpload(key: string) {
  const t = tasks.value.find((x) => x.key === key);
  if (!t?._upload) return;
  patch(key, { status: "uploading" });
  t._upload.start();
}

export function cancelUpload(key: string) {
  const t = tasks.value.find((x) => x.key === key);
  if (!t) return;
  if (t._upload) t._upload.abort(true).catch(() => {});
  if (t._xhr) t._xhr.abort();
  removeTask(key);
}

function autoClear(key: string) {
  setTimeout(() => {
    const t = tasks.value.find((x) => x.key === key);
    if (t && t.status === "done") removeTask(key);
  }, 4000);
}
