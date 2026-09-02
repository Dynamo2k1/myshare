// Thin fetch wrapper. Every request carries the per-tab client id header so the
// server can suppress echoing our own mutations back over SSE.

// A per-tab id used only to suppress SSE echoes of our own mutations. It does
// not need to be cryptographically strong, and crypto.randomUUID() is missing
// in non-secure contexts (plain HTTP over the LAN), so we degrade gracefully.
function randomClientId(): string {
  try {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
      return crypto.randomUUID();
    }
    if (typeof crypto !== "undefined" && typeof crypto.getRandomValues === "function") {
      const b = crypto.getRandomValues(new Uint8Array(16));
      return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
    }
  } catch {
    /* fall through */
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}

export const clientId = (() => {
  const k = "myshare_client_id";
  try {
    const existing = sessionStorage.getItem(k);
    if (existing) return existing;
    const v = randomClientId();
    sessionStorage.setItem(k, v);
    return v;
  } catch {
    // sessionStorage can throw in locked-down contexts; a transient id is fine.
    return randomClientId();
  }
})();

export class ApiError extends Error {
  status: number;
  code?: string;
  constructor(status: number, message: string, code?: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { "X-MyShare-Client": clientId };
  let payload: BodyInit | undefined;
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const res = await fetch(path, { method, headers, body: payload });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  const data = text ? JSON.parse(text) : undefined;
  if (!res.ok) {
    const msg = (data && (data.error || data.message)) || `Request failed (${res.status})`;
    throw new ApiError(res.status, msg, data?.code);
  }
  return data as T;
}

export const api = {
  get: <T,>(p: string) => req<T>("GET", p),
  post: <T,>(p: string, b?: unknown) => req<T>("POST", p, b),
  patch: <T,>(p: string, b?: unknown) => req<T>("PATCH", p, b),
  del: <T,>(p: string, b?: unknown) => req<T>("DELETE", p, b),
};

// --- domain types -------------------------------------------------------

export interface FileItem {
  id: string;
  name: string;
  kind: "file" | "screenshot";
  mime: string;
  size: number;
  hash: string;
  created_at: number;
  updated_at: number;
}
export interface ClipboardItem {
  id: string;
  content: string;
  format: string;
  pinned: boolean;
  created_at: number;
  updated_at: number;
}
export interface Snippet {
  id: string;
  title: string;
  content: string;
  language: string;
  pinned: boolean;
  created_at: number;
  updated_at: number;
}
export interface Note {
  id: string;
  title: string;
  content: string;
  pinned: boolean;
  created_at: number;
  updated_at: number;
}
export interface Transfer {
  id: string;
  name: string;
  kind: string;
  mime: string;
  size: number;
  offset: number;
  status: "active" | "completed" | "failed";
  file_id?: string;
  created_at: number;
  updated_at: number;
}
export interface Share {
  id: string;
  file_id: string;
  created_at: number;
  expires_at?: number;
  max_downloads?: number;
  downloads: number;
  one_time: boolean;
  revoked_at?: number;
  url?: string;
}
export interface Page<T> {
  items: T[];
  next_cursor?: string;
  total: number;
}
export interface SearchHit {
  entity: string;
  ref_id: string;
  title: string;
  snippet: string;
}
export interface Status {
  version: string;
  host: string;
  port: number;
  data_dir: string;
  tls: boolean;
  auth: boolean;
  connected: number;
  stats: Record<string, number>;
  disk: { total: number; free: number; used: number; fs_type: string; unsafe_wal: boolean };
  max_file_size: number;
  max_storage: number;
  storage_used_pct?: number;
}

export const rawURL = (id: string, download = false) =>
  `/api/files/${id}/raw${download ? "?dl=1" : ""}`;
