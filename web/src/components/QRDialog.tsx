import { render } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { Modal } from "./Modal";
import { copyText } from "../lib/clipboard";

// showQR mounts a one-off modal. The QR library is imported dynamically so it
// only downloads the first time someone opens a QR code.
export function showQR(text: string, caption = "Scan with your phone") {
  const host = document.createElement("div");
  document.body.appendChild(host);
  const close = () => {
    render(null, host);
    host.remove();
  };
  render(<QRDialog text={text} caption={caption} onClose={close} />, host);
}

function QRDialog({
  text,
  caption,
  onClose,
}: {
  text: string;
  caption: string;
  onClose: () => void;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const QR = (await import("qrcode")).default;
        if (cancelled || !canvasRef.current) return;
        await QR.toCanvas(canvasRef.current, text, { width: 260, margin: 2 });
      } catch (e) {
        setErr("Could not render QR code");
        console.error(e);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [text]);

  return (
    <Modal title="QR code" onClose={onClose}>
      <div class="qr-wrap">
        {err ? <p class="error-text">{err}</p> : <canvas ref={canvasRef} />}
        <p class="qr-caption">{caption}</p>
        <code class="qr-text">{text}</code>
        <button class="btn btn-sm" onClick={() => copyText(text)}>
          Copy link
        </button>
      </div>
    </Modal>
  );
}
