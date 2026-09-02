import { signal } from "@preact/signals";
import { useEffect, useRef, useState } from "preact/hooks";
import { api, type SearchHit } from "../lib/api";

export const paletteOpen = signal(false);

const ENTITY_TAB: Record<string, string> = {
  file: "files",
  clipboard: "clipboard",
  snippet: "snippets",
  note: "notes",
};
const ENTITY_ICON: Record<string, string> = {
  file: "📁",
  clipboard: "📋",
  snippet: "‹›",
  note: "🗒️",
};

export function CommandPalette() {
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<SearchHit[]>([]);
  const [sel, setSel] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const open = paletteOpen.value;

  useEffect(() => {
    if (open) {
      setQ("");
      setHits([]);
      setSel(0);
      setTimeout(() => inputRef.current?.focus(), 0);
    }
  }, [open]);

  useEffect(() => {
    if (!q.trim()) {
      setHits([]);
      return;
    }
    const id = setTimeout(async () => {
      try {
        const r = await api.get<{ items: SearchHit[] }>(
          `/api/search?q=${encodeURIComponent(q)}&limit=25`,
        );
        setHits(r.items || []);
        setSel(0);
      } catch {
        setHits([]);
      }
    }, 120);
    return () => clearTimeout(id);
  }, [q]);

  if (!open) return null;

  const go = (h: SearchHit) => {
    paletteOpen.value = false;
    const tab = ENTITY_TAB[h.entity] || "files";
    location.hash = `#/${tab}`;
    window.dispatchEvent(
      new CustomEvent("myshare:focus", { detail: { entity: h.entity, id: h.ref_id } }),
    );
  };

  return (
    <div class="palette-backdrop" onClick={() => (paletteOpen.value = false)}>
      <div class="palette" onClick={(e) => e.stopPropagation()}>
        <input
          ref={inputRef}
          class="palette-input"
          placeholder="Search files, clipboard, snippets, notes…"
          value={q}
          onInput={(e) => setQ((e.target as HTMLInputElement).value)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setSel((s) => Math.min(s + 1, hits.length - 1));
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setSel((s) => Math.max(s - 1, 0));
            } else if (e.key === "Enter" && hits[sel]) {
              go(hits[sel]);
            }
          }}
        />
        <div class="palette-results">
          {q.trim() && hits.length === 0 && <div class="palette-empty">No matches</div>}
          {hits.map((h, i) => (
            <button
              key={`${h.entity}:${h.ref_id}`}
              class={`palette-row ${i === sel ? "is-sel" : ""}`}
              onMouseEnter={() => setSel(i)}
              onClick={() => go(h)}
            >
              <span class="palette-icon">{ENTITY_ICON[h.entity] || "•"}</span>
              <span class="palette-title">{h.title || h.snippet || h.ref_id}</span>
              <span class="palette-snippet" dangerouslySetInnerHTML={{ __html: escapeHTML(h.snippet) }} />
            </button>
          ))}
        </div>
        <div class="palette-hint">↑↓ to navigate · ↵ to open · esc to close</div>
      </div>
    </div>
  );
}

function escapeHTML(s: string) {
  const d = document.createElement("div");
  d.textContent = s;
  return d.innerHTML.replace(/\[(.+?)\]/g, "<mark>$1</mark>");
}
