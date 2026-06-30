// Small pure formatters shared across pages.

// formatBytes renders a byte count as a compact human string ("612 KB").
export function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const value = bytes / Math.pow(1024, i);
  const rounded = value >= 100 || i === 0 ? Math.round(value) : Math.round(value * 10) / 10;
  return `${rounded} ${units[i]}`;
}

// formatDuration renders seconds as m:ss.
export function formatDuration(seconds: number): string {
  if (!seconds || seconds < 0) return "0:00";
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${m}:${s.toString().padStart(2, "0")}`;
}

// formatBPM renders a tempo, hiding zero/unknown values.
export function formatBPM(bpm: number): string {
  if (!bpm || bpm <= 0) return "—";
  return `${Math.round(bpm)}`;
}

// MIDI note names C-1 … G9 (standard MIDI 0–127).
const NOTE_NAMES = ["C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"];

export function formatMidiKey(key: number): string {
  if (key < 0 || key > 127) return "—";
  const oct = Math.floor(key / 12) - 1;
  return `${NOTE_NAMES[key % 12]}${oct}`;
}

export function formatKeyRange(min: number, max: number): string {
  if (min === max) return formatMidiKey(min);
  return `${formatMidiKey(min)}–${formatMidiKey(max)}`;
}
