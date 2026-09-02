// Server-sent events: one EventSource for the whole app. Handlers subscribe by
// event type. On "resync" (server dropped events for a slow client) or on
// reconnect, subscribers are asked to refetch.

type Handler = (data: any) => void;

const handlers = new Map<string, Set<Handler>>();
let es: EventSource | null = null;
let resyncCbs = new Set<() => void>();

const KNOWN = [
  "file.created", "file.updated", "file.deleted",
  "clipboard.created", "clipboard.updated", "clipboard.deleted", "clipboard.cleared",
  "snippet.created", "snippet.updated", "snippet.deleted",
  "note.created", "note.updated", "note.deleted",
  "transfer.created", "transfer.progress", "transfer.completed", "transfer.failed", "transfer.removed",
];

export function connectEvents() {
  if (es) return;
  open();
}

function open() {
  es = new EventSource("/api/events");
  for (const name of KNOWN) {
    es.addEventListener(name, (e) => dispatch(name, (e as MessageEvent).data));
  }
  es.addEventListener("resync", () => resyncCbs.forEach((cb) => cb()));
  es.onerror = () => {
    // EventSource auto-reconnects; when it does, force a resync so we don't
    // miss anything that happened while disconnected.
    if (es && es.readyState === EventSource.CLOSED) {
      es = null;
      setTimeout(() => {
        open();
        resyncCbs.forEach((cb) => cb());
      }, 2000);
    }
  };
  es.addEventListener("hello", () => {
    /* connected */
  });
}

function dispatch(type: string, raw: string) {
  let data: any;
  try {
    data = raw ? JSON.parse(raw) : undefined;
  } catch {
    data = raw;
  }
  handlers.get(type)?.forEach((h) => h(data));
}

export function onEvent(type: string, handler: Handler): () => void {
  let set = handlers.get(type);
  if (!set) {
    set = new Set();
    handlers.set(type, set);
  }
  set.add(handler);
  return () => set!.delete(handler);
}

export function onResync(cb: () => void): () => void {
  resyncCbs.add(cb);
  return () => resyncCbs.delete(cb);
}
