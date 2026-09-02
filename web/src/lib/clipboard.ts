import { toast } from "./toast";

// copyText works over plain HTTP on the LAN too: navigator.clipboard requires a
// secure context (https or localhost), so we fall back to a hidden textarea +
// execCommand when it's unavailable.
export async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text);
      toast("Copied", "success");
      return true;
    } catch {
      /* fall through */
    }
  }
  const ok = legacyCopy(text);
  toast(ok ? "Copied" : "Couldn't copy — select and copy manually", ok ? "success" : "error");
  return ok;
}

function legacyCopy(text: string): boolean {
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.style.position = "fixed";
  ta.style.top = "-1000px";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  ta.setSelectionRange(0, ta.value.length);
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  document.body.removeChild(ta);
  return ok;
}

// copyImage tries to place an image on the clipboard (needs a secure context and
// ClipboardItem support). Returns false when unsupported so the caller can offer
// a download instead.
export async function copyImage(blob: Blob): Promise<boolean> {
  if (
    !window.isSecureContext ||
    typeof ClipboardItem === "undefined" ||
    !navigator.clipboard?.write
  ) {
    return false;
  }
  try {
    await navigator.clipboard.write([new ClipboardItem({ [blob.type]: blob })]);
    toast("Image copied", "success");
    return true;
  } catch {
    return false;
  }
}

export const canCopyImage = () =>
  window.isSecureContext &&
  typeof ClipboardItem !== "undefined" &&
  !!navigator.clipboard?.write;
