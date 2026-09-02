import { useEffect, useState } from "preact/hooks";
import { startUpload } from "../lib/uploader";
import { toast } from "../lib/toast";
import { serverMode, currentDir } from "../lib/mode";

// A full-window drop target. Dropping files anywhere uploads them; the overlay
// only shows while a drag is in progress.
export function GlobalDrop() {
  const [active, setActive] = useState(false);

  useEffect(() => {
    let depth = 0;
    const onEnter = (e: DragEvent) => {
      if (!e.dataTransfer?.types.includes("Files")) return;
      depth++;
      setActive(true);
    };
    const onLeave = () => {
      depth = Math.max(0, depth - 1);
      if (depth === 0) setActive(false);
    };
    const onOver = (e: DragEvent) => {
      if (e.dataTransfer?.types.includes("Files")) e.preventDefault();
    };
    const onDrop = (e: DragEvent) => {
      e.preventDefault();
      depth = 0;
      setActive(false);
      const files = Array.from(e.dataTransfer?.files || []);
      if (!files.length) return;
      const dest =
        serverMode.value === "directory"
          ? { endpoint: "/api/browse", dir: currentDir.value }
          : {};
      for (const f of files) {
        const isImg = f.type.startsWith("image/");
        startUpload(f, isImg ? "screenshot" : "file", dest);
      }
      toast(`Uploading ${files.length} file${files.length > 1 ? "s" : ""}…`, "info");
    };

    window.addEventListener("dragenter", onEnter);
    window.addEventListener("dragleave", onLeave);
    window.addEventListener("dragover", onOver);
    window.addEventListener("drop", onDrop);
    return () => {
      window.removeEventListener("dragenter", onEnter);
      window.removeEventListener("dragleave", onLeave);
      window.removeEventListener("dragover", onOver);
      window.removeEventListener("drop", onDrop);
    };
  }, []);

  if (!active) return null;
  return (
    <div class="drop-overlay">
      <div class="drop-inner">
        <div class="drop-icon">⬇</div>
        Drop to upload
      </div>
    </div>
  );
}
