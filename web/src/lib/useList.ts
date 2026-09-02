import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { api, type Page } from "./api";
import { onEvent, onResync } from "./events";

// useList loads a paginated collection and keeps it fresh from SSE. It exposes
// enough to render list/empty/loading/error states without repeating the wiring
// in every tab.
export function useList<T extends { id: string }>(
  path: (q: string) => string,
  events: string[],
) {
  const [items, setItems] = useState<T[]>([]);
  const [total, setTotal] = useState(0);
  const [cursor, setCursor] = useState<string | undefined>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>("");
  const [query, setQuery] = useState("");
  const qRef = useRef(query);
  qRef.current = query;

  const load = useCallback(
    async (append = false) => {
      setLoading(true);
      setError("");
      try {
        const sep = path(qRef.current).includes("?") ? "&" : "?";
        const url =
          path(qRef.current) + (append && cursor ? `${sep}cursor=${encodeURIComponent(cursor)}` : "");
        const page = await api.get<Page<T>>(url);
        setItems((prev) => (append ? [...prev, ...(page.items || [])] : page.items || []));
        setTotal(page.total);
        setCursor(page.next_cursor);
      } catch (e) {
        setError((e as Error).message || "Failed to load");
      } finally {
        setLoading(false);
      }
    },
    [path, cursor],
  );

  // Reload when the query changes (debounced by the caller's input).
  useEffect(() => {
    const id = setTimeout(() => load(false), 0);
    return () => clearTimeout(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query]);

  useEffect(() => {
    const offs = events.map((ev) => onEvent(ev, () => load(false)));
    offs.push(onResync(() => load(false)));
    return () => offs.forEach((f) => f());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [events.join(",")]);

  return {
    items,
    setItems,
    total,
    hasMore: !!cursor,
    loading,
    error,
    query,
    setQuery,
    reload: () => load(false),
    loadMore: () => load(true),
  };
}
