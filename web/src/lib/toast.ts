import { signal } from "@preact/signals";

export interface Toast {
  id: number;
  kind: "info" | "success" | "error";
  message: string;
  action?: { label: string; run: () => void };
}

export const toasts = signal<Toast[]>([]);
let seq = 1;

export function toast(
  message: string,
  kind: Toast["kind"] = "info",
  action?: Toast["action"],
) {
  const id = seq++;
  toasts.value = [...toasts.value, { id, kind, message, action }];
  const ttl = kind === "error" ? 7000 : 3500;
  setTimeout(() => dismiss(id), ttl);
  return id;
}

export function dismiss(id: number) {
  toasts.value = toasts.value.filter((t) => t.id !== id);
}
