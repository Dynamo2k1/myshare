import { render } from "preact";
import "./styles.css";

const el = document.getElementById("app");

function fatal(message: string) {
  if (!el) return;
  el.innerHTML = "";
  const box = document.createElement("div");
  box.style.cssText =
    "max-width:32rem;margin:12vh auto;padding:24px;font:14px/1.6 system-ui,sans-serif;color:#e6e9ef;background:#14171d;border:1px solid #333a45;border-radius:10px";
  box.innerHTML =
    '<h2 style="margin:0 0 8px;font-size:16px">MyShare failed to start</h2>' +
    '<p style="margin:0 0 12px;color:#98a2b3">The page loaded but the app could not initialise.</p>' +
    '<pre style="white-space:pre-wrap;word-break:break-word;background:#1b1f27;padding:10px;border-radius:6px;margin:0;font-size:12px"></pre>' +
    '<p style="margin:12px 0 0;color:#98a2b3">Try a hard refresh. If it persists, check the browser console and the server log.</p>';
  box.querySelector("pre")!.textContent = message;
  el.appendChild(box);
}

// Surface async startup errors instead of leaving a blank page.
window.addEventListener("error", (e) => {
  if (el && !el.hasChildNodes()) fatal(String(e.message || e.error || e));
});
window.addEventListener("unhandledrejection", (e) => {
  if (el && !el.hasChildNodes()) fatal(String(e.reason));
});

(async () => {
  try {
    const { App } = await import("./app");
    if (el) render(<App />, el);
  } catch (err) {
    fatal(err instanceof Error ? `${err.message}\n\n${err.stack ?? ""}` : String(err));
  }
})();
