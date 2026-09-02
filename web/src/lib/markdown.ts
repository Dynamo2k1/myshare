// Markdown -> safe HTML. `marked` is loaded on demand (only when the Markdown
// tab or a rendered note is first shown) so it stays out of the initial bundle.
//
// The app's CSP already blocks inline scripts and event handlers, so injected
// <script>/onclick can't run; this sanitiser is defence in depth — it strips the
// dangerous elements/attributes before the HTML ever reaches the DOM.

let markedMod: typeof import("marked") | null = null;

export async function renderMarkdown(src: string): Promise<string> {
  if (!markedMod) {
    markedMod = await import("marked");
    markedMod.marked.setOptions({ gfm: true, breaks: true });
  }
  const raw = await markedMod.marked.parse(src ?? "", { async: true });
  return sanitize(raw);
}

const BLOCKED_TAGS =
  /<\/?(?:script|style|iframe|object|embed|form|input|button|link|meta|base)\b[^>]*>/gi;
const EVENT_ATTRS = /\s+on[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi;
const JS_URI = /(href|src)\s*=\s*("|')\s*javascript:[^"']*\2/gi;
const DATA_HTML_URI = /(href|src)\s*=\s*("|')\s*data:text\/html[^"']*\2/gi;

function sanitize(html: string): string {
  return html
    .replace(BLOCKED_TAGS, "")
    .replace(EVENT_ATTRS, "")
    .replace(JS_URI, '$1=$2#$2')
    .replace(DATA_HTML_URI, '$1=$2#$2');
}

// plainText strips markdown to a rough plain-text form for "copy as text".
export function markdownToPlain(src: string): string {
  return src
    .replace(/^#{1,6}\s+/gm, "")
    .replace(/(\*\*|__)(.*?)\1/g, "$2")
    .replace(/(\*|_)(.*?)\1/g, "$2")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/^\s*[-*+]\s+/gm, "• ")
    .replace(/^\s*>\s?/gm, "")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .trim();
}
