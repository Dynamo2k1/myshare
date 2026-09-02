import { signal } from "@preact/signals";
import { api } from "./api";

// Which mode the server is in, fetched once at startup:
//   "hub"       — the persistent personal store (content-addressed blobs)
//   "directory" — browsing a real folder on disk
export const serverMode = signal<"hub" | "directory">("hub");
export const serveDir = signal<string>("");
export const ephemeral = signal<boolean>(false);

// The folder the Files tab is currently showing (directory mode). Global
// drop/paste uploads target this so a file lands where you're looking.
export const currentDir = signal<string>("");

export async function loadMode() {
  try {
    const s = await api.get<{
      mode?: "hub" | "directory";
      serve_dir?: string;
      ephemeral?: boolean;
    }>("/api/status");
    serverMode.value = s.mode === "directory" ? "directory" : "hub";
    serveDir.value = s.serve_dir ?? "";
    ephemeral.value = !!s.ephemeral;
  } catch {
    /* keep defaults */
  }
}
