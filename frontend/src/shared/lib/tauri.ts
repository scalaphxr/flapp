// Thin wrappers around Tauri's dialog plugin and webview drag-drop events. Each
// degrades gracefully when not running inside Tauri (e.g. browser-only dev),
// returning empty results so the UI never crashes.

export function isTauri(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

const AUDIO_FILTER = {
  name: "Audio, projects & archives",
  extensions: ["wav", "mp3", "flac", "ogg", "aiff", "aif", "m4a", "flp", "zip", "rar", "7z"],
};

// pickFiles opens a native multi-select file dialog and returns chosen paths.
export async function pickFiles(): Promise<string[]> {
  if (!isTauri()) return [];
  try {
    const { open } = await import("@tauri-apps/plugin-dialog");
    const selected = await open({ multiple: true, directory: false, filters: [AUDIO_FILTER] });
    if (!selected) return [];
    return Array.isArray(selected) ? selected : [selected];
  } catch {
    return [];
  }
}

// pickFolder opens a native folder picker and returns the chosen directory.
export async function pickFolder(): Promise<string | null> {
  if (!isTauri()) return null;
  try {
    const { open } = await import("@tauri-apps/plugin-dialog");
    const selected = await open({ multiple: false, directory: true });
    if (!selected) return null;
    return Array.isArray(selected) ? selected[0] : selected;
  } catch {
    return null;
  }
}

// onFileDrop subscribes to OS drag-drop over the window, delivering absolute
// paths. Returns a disposer; a no-op when not in Tauri.
export async function onFileDrop(
  onDrop: (paths: string[]) => void,
  onHover?: (hovering: boolean) => void
): Promise<() => void> {
  if (!isTauri()) return () => {};
  try {
    const { getCurrentWebview } = await import("@tauri-apps/api/webview");
    const unlisten = await getCurrentWebview().onDragDropEvent((event) => {
      const p = event.payload as { type: string; paths?: string[] };
      if (p.type === "over" || p.type === "enter") {
        onHover?.(true);
      } else if (p.type === "drop") {
        onHover?.(false);
        if (p.paths?.length) onDrop(p.paths);
      } else {
        onHover?.(false);
      }
    });
    return unlisten;
  } catch {
    return () => {};
  }
}

// fileName extracts the trailing path segment for display.
export function fileName(path: string): string {
  const parts = path.split(/[\\/]/);
  return parts[parts.length - 1] || path;
}

// kindOf guesses a display kind from a path's extension.
export function kindOf(path: string): "zip" | "flp" | "audio" | "folder" {
  const ext = (path.split(".").pop() || "").toLowerCase();
  if (["zip", "rar", "7z"].includes(ext)) return "zip";
  if (ext === "flp") return "flp";
  if (["wav", "mp3", "flac", "ogg", "aiff", "aif", "m4a"].includes(ext)) return "audio";
  return "folder";
}
