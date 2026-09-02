export function bytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB", "PB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export function ago(unixSec: number): string {
  const s = Math.max(0, Math.floor(Date.now() / 1000 - unixSec));
  if (s < 10) return "just now";
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  if (d < 30) return `${d}d ago`;
  return new Date(unixSec * 1000).toLocaleDateString();
}

export function speed(bytesPerSec: number): string {
  return `${bytes(bytesPerSec)}/s`;
}

export function eta(remaining: number, bytesPerSec: number): string {
  if (bytesPerSec <= 0) return "—";
  const s = Math.round(remaining / bytesPerSec);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const r = s % 60;
  if (m < 60) return `${m}m ${r}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

export function extIcon(mime: string, name: string): string {
  if (mime.startsWith("image/")) return "🖼️";
  if (mime.startsWith("video/")) return "🎬";
  if (mime.startsWith("audio/")) return "🎵";
  if (mime === "application/pdf") return "📄";
  if (mime.startsWith("text/")) return "📝";
  if (/\.(zip|tar|gz|7z|rar|xz|bz2)$/i.test(name)) return "🗜️";
  if (/\.(js|ts|go|py|rs|c|cpp|java|rb|sh|json|yaml|yml|toml)$/i.test(name)) return "💾";
  return "📦";
}
