// Вкладка «Плеер» — автономный модуль.
// Архитектура:
//   - Фронтенд: UI, воспроизведение (HTML5 <audio>), отрисовка волны (<canvas>).
//   - Rust: анализ каждого файла (BPM, тональность, LUFS, пики). Async команды.
//   - Прогресс анализа стримится через Tauri-события «player-analysis-progress».

import React from "react";
import { Icons, Input, Button } from "@/shared/ui";
import { useT } from "@/shared/i18n";
import { formatBytes, formatDuration } from "@/shared/lib/format";
import { fileName, onFileDrop, pickFiles, pickFolder, isTauri } from "@/shared/lib/tauri";
import { useSettingsStore } from "@/shared/model/settings";

// ── Типы ─────────────────────────────────────────────────────────────────────

interface AudioMeta {
  path: string;
  name: string;
  format: string;
  durationS: number;
  sampleRate: number;
  channels: number;
  bitDepth?: number;
  fileSizeBytes: number;
  lufs?: number;
  peakDbfs?: number;
  bpm?: number;
  key?: string;
  keyConfidence?: number;
  peaks: number[];
  createdAt?: number;
  error?: string;
}

interface FileEntry {
  path: string;
  name: string;
  createdAt?: number; // из FS-метаданных, доступно сразу
  meta: AudioMeta | null; // null = ещё анализируется
}

type SortCol = "name" | "format" | "duration" | "bpm" | "key" | "size" | "created";

// ── Утилиты ───────────────────────────────────────────────────────────────────

function formatDate(ts: number): string {
  const d = new Date(ts * 1000);
  const yyyy = d.getFullYear();
  const mo = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const mi = String(d.getMinutes()).padStart(2, "0");
  return `${yyyy}-${mo}-${dd} ${hh}:${mi}`;
}

function isAudioPath(p: string): boolean {
  const ext = p.split(".").pop()?.toLowerCase() ?? "";
  return ["wav", "mp3", "flac", "ogg", "aiff", "aif", "m4a", "aac"].includes(ext);
}

function audioMimeType(p: string): string {
  const ext = p.split(".").pop()?.toLowerCase() ?? "";
  return ({ wav: "audio/wav", mp3: "audio/mpeg", flac: "audio/flac", ogg: "audio/ogg", m4a: "audio/mp4", aac: "audio/aac", aiff: "audio/aiff", aif: "audio/aiff" } as Record<string, string>)[ext] ?? "audio/octet-stream";
}

async function invoke<T>(cmd: string, args?: Record<string, unknown>): Promise<T> {
  const { invoke: tauriInvoke } = await import("@tauri-apps/api/core");
  return tauriInvoke<T>(cmd, args);
}

async function listenOnce<T>(event: string, cb: (payload: T) => void): Promise<() => void> {
  const { listen } = await import("@tauri-apps/api/event");
  return listen<T>(event, (e) => cb(e.payload));
}

// ── Waveform canvas ───────────────────────────────────────────────────────────

function setAlpha(color: string, alpha: number): string {
  const rgb = color.match(/rgb\((\d+),\s*(\d+),\s*(\d+)\)/);
  if (rgb) return `rgba(${rgb[1]},${rgb[2]},${rgb[3]},${alpha})`;
  if (color.startsWith('#') && color.length === 7) {
    const r = parseInt(color.slice(1, 3), 16);
    const g = parseInt(color.slice(3, 5), 16);
    const b = parseInt(color.slice(5, 7), 16);
    return `rgba(${r},${g},${b},${alpha})`;
  }
  if (color.startsWith('#') && color.length === 4) {
    const r = parseInt(color[1] + color[1], 16);
    const g = parseInt(color[2] + color[2], 16);
    const b = parseInt(color[3] + color[3], 16);
    return `rgba(${r},${g},${b},${alpha})`;
  }
  return color;
}

/** Downsamples peaks array to targetLen by taking max in each bucket. */
function downsamplePeaks(peaks: number[], targetLen: number): number[] {
  if (peaks.length <= targetLen) return peaks;
  const out: number[] = new Array(targetLen);
  const ratio = peaks.length / targetLen;
  for (let i = 0; i < targetLen; i++) {
    const start = Math.floor(i * ratio);
    const end = Math.min(Math.floor((i + 1) * ratio), peaks.length);
    let max = 0;
    for (let j = start; j < end; j++) {
      const v = Math.abs(peaks[j]);
      if (v > max) max = v;
    }
    out[i] = max;
  }
  return out;
}

function WaveformBar({
  peaks,
  progress,
  onSeek,
  isFl,
}: {
  peaks: number[];
  progress: number;
  onSeek: (ratio: number) => void;
  isFl?: boolean;
}) {
  const ref = React.useRef<HTMLCanvasElement>(null);
  const playRef = React.useRef(progress);
  playRef.current = progress;

  const draw = React.useCallback(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    if (!rect.width) return;
    const w = Math.round(rect.width * dpr);
    const h = Math.round(rect.height * dpr);
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    const style = getComputedStyle(canvas);
    const accent = isFl
      ? (style.getPropertyValue("--lcd-green").trim() || "#8dff6a")
      : (style.getPropertyValue("--accent").trim() || "#E8845C");
    const muted = isFl
      ? (style.getPropertyValue("--ink-dim").trim() || "#5d626a")
      : (style.getPropertyValue("--surface-3").trim() || "#2A2118");
    const head = isFl
      ? (style.getPropertyValue("--lcd-amber").trim() || "#ffb55b")
      : (style.getPropertyValue("--text-body").trim() || "#F4ECE3");

    const cy = h / 2;
    const prog = playRef.current;

    if (!peaks.length) {
      ctx.fillStyle = muted;
      ctx.fillRect(0, cy - 1, w, 2);
    } else {
      const barPx = 2;
      const numBars = Math.max(1, Math.floor(w / barPx));
      const dp = downsamplePeaks(peaks, numBars);

      // Center baseline
      ctx.fillStyle = setAlpha(muted, 0.30);
      ctx.fillRect(0, cy - 1, w, 1);

      const accentDim = setAlpha(accent, 0.55);
      const mutedDim  = setAlpha(muted, 0.55);

      // Pass 1: bar bodies at 55% opacity
      for (let i = 0; i < dp.length; i++) {
        const amp    = dp[i];
        const half   = Math.max(1, amp * cy * 0.92);
        const x      = i * barPx;
        const played = i / dp.length < prog;
        ctx.fillStyle = played ? accentDim : mutedDim;
        if (half >= 2) ctx.fillRect(x, cy - half + 1, 1, half * 2 - 2);
      }

      // Pass 2: peak tips at 100% opacity
      for (let i = 0; i < dp.length; i++) {
        const amp    = dp[i];
        const half   = Math.max(1, amp * cy * 0.92);
        const x      = i * barPx;
        const played = i / dp.length < prog;
        ctx.fillStyle = played ? accent : muted;
        ctx.fillRect(x, cy - half, 1, 1);
        if (half > 1) ctx.fillRect(x, cy + half - 1, 1, 1);
      }
    }
    const px = prog * w;
    ctx.fillStyle = head;
    ctx.fillRect(px - 1, 0, 2, h);
  }, [peaks, isFl]);

  React.useEffect(() => { requestAnimationFrame(draw); }, [peaks, progress, draw]);

  function handleClick(e: React.MouseEvent<HTMLCanvasElement>) {
    const rect = e.currentTarget.getBoundingClientRect();
    onSeek((e.clientX - rect.left) / rect.width);
  }

  return (
    <canvas
      ref={ref}
      onClick={handleClick}
      style={{
        width: "100%",
        height: isFl ? 38 : 56,
        display: "block",
        cursor: "pointer",
        borderRadius: isFl ? 3 : 6,
        background: isFl ? "var(--groove)" : undefined,
      }}
    />
  );
}

// ── Drag VOL knob (FL style) ──────────────────────────────────────────────────

function VolKnob({ value, onChange }: { value: number; onChange: (v: number) => void }) {
  const startRef = React.useRef<{ y: number; v: number } | null>(null);

  const handleMouseDown = (e: React.MouseEvent) => {
    e.preventDefault();
    startRef.current = { y: e.clientY, v: value };
    const onMove = (me: MouseEvent) => {
      if (!startRef.current) return;
      const delta = (startRef.current.y - me.clientY) / 80;
      const next = Math.max(0, Math.min(1, startRef.current.v + delta));
      onChange(next);
    };
    const onUp = () => {
      startRef.current = null;
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  };

  const angle = -135 + value * 270;
  return (
    <div
      onMouseDown={handleMouseDown}
      title={`VOL ${Math.round(value * 100)}%`}
      style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 4, cursor: "ns-resize", userSelect: "none" }}
    >
      <div style={{
        width: 34, height: 34, borderRadius: "50%",
        background: "radial-gradient(circle at 40% 35%, var(--btn-hi), var(--btn) 60%, var(--btn-lo))",
        border: "1px solid var(--chrome-lo)",
        boxShadow: "0 2px 6px rgba(0,0,0,.45), inset 0 1px 0 rgba(255,255,255,.4)",
        position: "relative",
        display: "flex", alignItems: "center", justifyContent: "center",
      }}>
        <div style={{
          position: "absolute", width: 2, height: 10, background: "var(--ink)", borderRadius: 1,
          transformOrigin: "50% 100%",
          transform: `translateY(-4px) rotate(${angle}deg)`,
          top: "50%", left: "calc(50% - 1px)",
        }} />
      </div>
      <span style={{ font: "700 9px var(--font-sans)", letterSpacing: "1px", color: "var(--ink-dim)" }}>VOL</span>
    </div>
  );
}

// ── Панель плеера ─────────────────────────────────────────────────────────────

function PlayerBar({ entry, onClose, isFl }: { entry: FileEntry; onClose: () => void; isFl?: boolean }) {
  const [audio] = React.useState(() => new Audio());
  const [playing, setPlaying] = React.useState(false);
  const [current, setCurrent] = React.useState(0);
  const [duration, setDuration] = React.useState(0);
  const [volume, setVolume] = React.useState(1);
  const [blobUrl, setBlobUrl] = React.useState<string | null>(null);
  const rafRef = React.useRef<number>(0);

  // Загрузка источника при смене файла.
  React.useEffect(() => {
    audio.pause();
    setPlaying(false);
    setCurrent(0);

    const load = async () => {
      if (isTauri()) {
        // Читаем файл через Rust-команду → ArrayBuffer → Blob URL.
        // Это обходит ограничения asset-протокола и работает для всех форматов.
        try {
          const buf = await invoke<ArrayBuffer>("player_read_audio", { path: entry.path });
          const blob = new Blob([buf], { type: audioMimeType(entry.path) });
          const url = URL.createObjectURL(blob);
          setBlobUrl((prev) => { if (prev) URL.revokeObjectURL(prev); return url; });
          audio.src = url;
        } catch {
          audio.src = entry.path;
        }
      } else {
        audio.src = entry.path;
      }
      audio.load();
    };
    load();

    return () => {
      cancelAnimationFrame(rafRef.current);
    };
  }, [entry.path]);

  // Обновление времени через rAF — плавнее, чем ontimeupdate.
  React.useEffect(() => {
    const tick = () => {
      setCurrent(audio.currentTime);
      setDuration(audio.duration || entry.meta?.durationS || 0);
      rafRef.current = requestAnimationFrame(tick);
    };
    rafRef.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafRef.current);
  }, [audio, entry.meta]);

  React.useEffect(() => {
    audio.volume = volume;
  }, [volume, audio]);

  React.useEffect(() => {
    audio.onended = () => setPlaying(false);
    return () => { audio.onended = null; };
  }, [audio]);

  const togglePlay = () => {
    if (playing) { audio.pause(); setPlaying(false); }
    else { audio.play().then(() => setPlaying(true)).catch(() => {}); }
  };

  const stop = () => {
    audio.pause();
    audio.currentTime = 0;
    setPlaying(false);
  };

  const seek = (ratio: number) => {
    const dur = audio.duration || duration;
    if (dur > 0) { audio.currentTime = ratio * dur; setCurrent(audio.currentTime); }
  };

  const progress = duration > 0 ? current / duration : 0;
  const peaks = entry.meta?.peaks ?? [];

  if (isFl) {
    return (
      <div style={{
        borderTop: "1px solid var(--line-work)",
        background: "linear-gradient(var(--chrome-hi), var(--chrome))",
        padding: "10px 16px",
        display: "flex",
        flexDirection: "column",
        gap: 8,
        flexShrink: 0,
        boxShadow: "0 -2px 8px rgba(0,0,0,.35)",
      }}>
        {/* Top row: name + meta + close */}
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <span style={{ flex: 1, font: "600 13px var(--font-sans)", color: "var(--ink)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            {entry.name}
          </span>
          {entry.meta && (
            <span style={{ font: "400 11px var(--font-mono)", color: "var(--ink-dim)", flexShrink: 0 }}>
              {entry.meta.format}
              {entry.meta.sampleRate ? ` · ${(entry.meta.sampleRate / 1000).toFixed(1)}k` : ""}
              {entry.meta.bpm ? ` · ${Math.round(entry.meta.bpm)} BPM` : ""}
              {entry.meta.key ? ` · ${entry.meta.key}` : ""}
            </span>
          )}
          <button onClick={onClose} style={{ background: "none", border: "none", cursor: "pointer", color: "var(--ink-dim)", padding: 4, display: "flex" }}>
            <Icons.X width={14} height={14} />
          </button>
        </div>

        {/* Waveform */}
        <WaveformBar peaks={peaks} progress={progress} onSeek={seek} isFl />

        {/* Bottom row: transport + LCD time + seek + VOL knob */}
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          {/* Stop */}
          <FlTBtn onClick={stop} title="Stop">
            <svg width="11" height="11" viewBox="0 0 24 24" fill="currentColor"><rect x="4" y="4" width="16" height="16"/></svg>
          </FlTBtn>
          {/* Play/Pause */}
          <FlTBtn onClick={togglePlay} active={playing} title={playing ? "Pause" : "Play"}>
            <svg width="11" height="11" viewBox="0 0 24 24" fill="currentColor">
              {playing
                ? <><rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/></>
                : <path d="M8 5v14l11-7z"/>
              }
            </svg>
          </FlTBtn>

          {/* LCD time */}
          <div style={{ display: "flex", alignItems: "center", gap: 0, background: "var(--lcd-bg)", borderRadius: 5, padding: "3px 10px", boxShadow: "inset 0 2px 6px rgba(0,0,0,.7)" }}>
            <span style={{ font: "400 15px var(--font-mono)", color: "var(--lcd-amber)", textShadow: "0 0 8px rgba(255,181,91,.5)", letterSpacing: "1px" }}>
              {formatDuration(current)}
            </span>
            <span style={{ font: "400 11px var(--font-mono)", color: "var(--lcd-green)", opacity: 0.6, margin: "0 4px" }}>/</span>
            <span style={{ font: "400 11px var(--font-mono)", color: "var(--lcd-green)", textShadow: "0 0 6px rgba(141,255,106,.4)" }}>
              {formatDuration(duration)}
            </span>
          </div>

          {/* Seek track */}
          <div
            style={{ flex: 1, height: 12, background: "var(--groove)", borderRadius: 3, cursor: "pointer", position: "relative", overflow: "hidden", border: "1px solid var(--chrome-lo)", boxShadow: "inset 0 1px 3px rgba(0,0,0,.5)" }}
            onClick={(e) => {
              const rect = e.currentTarget.getBoundingClientRect();
              seek((e.clientX - rect.left) / rect.width);
            }}
          >
            <div style={{ position: "absolute", left: 0, top: 0, bottom: 0, width: `${progress * 100}%`, background: "var(--lcd-green)", borderRadius: 3, boxShadow: "0 0 6px rgba(141,255,106,.5)" }} />
            <div style={{ position: "absolute", top: 0, bottom: 0, width: 2, background: "var(--lcd-amber)", left: `calc(${progress * 100}% - 1px)` }} />
          </div>

          {/* VOL knob */}
          <VolKnob value={volume} onChange={setVolume} />
        </div>
      </div>
    );
  }

  return (
    <div style={{
      borderTop: "1px solid var(--border)",
      background: "var(--surface-2)",
      padding: "14px 20px 16px",
      display: "flex",
      flexDirection: "column",
      gap: 10,
      flexShrink: 0,
    }}>
      {/* Название + закрыть */}
      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        <span style={{ flex: 1, fontWeight: "var(--fw-semibold)" as any, fontSize: "var(--fs-body)", color: "var(--text-body)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {entry.name}
        </span>
        {entry.meta && (
          <span style={{ fontSize: 11, color: "var(--text-faint)", fontFamily: "var(--font-mono)", flexShrink: 0 }}>
            {entry.meta.format}
            {entry.meta.sampleRate ? ` · ${(entry.meta.sampleRate / 1000).toFixed(1)}k` : ""}
            {entry.meta.bpm ? ` · ${Math.round(entry.meta.bpm)} BPM` : ""}
            {entry.meta.key ? ` · ${entry.meta.key}` : ""}
          </span>
        )}
        <button onClick={onClose} style={{ background: "none", border: "none", cursor: "pointer", color: "var(--text-faint)", padding: 4, display: "flex" }}>
          <Icons.X />
        </button>
      </div>

      {/* Waveform */}
      <WaveformBar peaks={peaks} progress={progress} onSeek={seek} />

      {/* Контролы */}
      <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
        <button onClick={stop} style={btnStyle}>
          <Icons.Stop />
        </button>
        <button onClick={togglePlay} style={{ ...btnStyle, color: "var(--accent)", fontSize: 20 }}>
          {playing ? <Icons.Pause /> : <Icons.Play />}
        </button>

        <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--text-faint)", minWidth: 90 }}>
          {formatDuration(current)} / {formatDuration(duration)}
        </span>

        <input
          type="range"
          min={0}
          max={duration || 1}
          step={0.1}
          value={current}
          onChange={(e) => seek(Number(e.target.value) / (duration || 1))}
          style={{ flex: 1, accentColor: "var(--accent)", cursor: "pointer" }}
        />

        <Icons.Volume style={{ color: "var(--text-faint)", flexShrink: 0 }} />
        <input
          type="range"
          min={0}
          max={1}
          step={0.01}
          value={volume}
          onChange={(e) => setVolume(Number(e.target.value))}
          style={{ width: 80, accentColor: "var(--accent)", cursor: "pointer" }}
        />
      </div>
    </div>
  );
}

function FlTBtn({ onClick, active, title, children }: { onClick: () => void; active?: boolean; title?: string; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      title={title}
      style={{
        width: 30, height: 30, borderRadius: 5, display: "flex", alignItems: "center", justifyContent: "center",
        cursor: "pointer",
        background: active ? "linear-gradient(var(--accent), var(--accent-deep, #e8651e))" : "linear-gradient(var(--btn-hi), var(--btn))",
        border: "1px solid var(--chrome-lo)",
        color: active ? "#fff" : "var(--ink)",
        boxShadow: active ? "inset 0 1px 0 rgba(255,255,255,.3), 0 0 8px rgba(255,138,60,.4)" : "inset 0 1px 0 rgba(255,255,255,.5), 0 1px 2px rgba(0,0,0,.25)",
      }}
    >
      {children}
    </button>
  );
}

const btnStyle: React.CSSProperties = {
  background: "none",
  border: "none",
  cursor: "pointer",
  color: "var(--text-body)",
  padding: 6,
  display: "flex",
  alignItems: "center",
  borderRadius: 6,
};

// ── Таблица файлов ────────────────────────────────────────────────────────────

function FileTable({
  entries,
  activeIdx,
  onActivate,
  sortCol,
  sortAsc,
  onSort,
  q,
  isFl,
}: {
  entries: FileEntry[];
  activeIdx: number | null;
  onActivate: (idx: number) => void;
  sortCol: SortCol;
  sortAsc: boolean;
  onSort: (col: SortCol) => void;
  q: string;
  isFl: boolean;
}) {
  const t = useT();

  const filtered = React.useMemo(() => {
    const lower = q.toLowerCase();
    return entries.map((e, i) => ({ e, i })).filter(({ e }) => !q || e.name.toLowerCase().includes(lower));
  }, [entries, q]);

  const sorted = React.useMemo(() => {
    return [...filtered].sort((a, b) => {
      const am = a.e.meta, bm = b.e.meta;
      let va: number | string = 0, vb: number | string = 0;
      switch (sortCol) {
        case "name":     va = a.e.name.toLowerCase();       vb = b.e.name.toLowerCase(); break;
        case "format":   va = am?.format ?? "";             vb = bm?.format ?? ""; break;
        case "duration": va = am?.durationS ?? 0;           vb = bm?.durationS ?? 0; break;
        case "bpm":      va = am?.bpm ?? 0;                 vb = bm?.bpm ?? 0; break;
        case "key":      va = am?.key ?? "";                vb = bm?.key ?? ""; break;
        case "size":     va = am?.fileSizeBytes ?? 0;       vb = bm?.fileSizeBytes ?? 0; break;
        case "created":  va = a.e.createdAt ?? am?.createdAt ?? 0; vb = b.e.createdAt ?? bm?.createdAt ?? 0; break;
      }
      const cmp = typeof va === "string" ? va.localeCompare(vb as string) : (va as number) - (vb as number);
      return sortAsc ? cmp : -cmp;
    });
  }, [filtered, sortCol, sortAsc]);

  const cols = "36px minmax(160px,1fr) 68px 68px 52px 72px 74px 120px";

  function Hdr({ col, children }: { col: SortCol; children: React.ReactNode }) {
    const active = sortCol === col;
    return (
      <button onClick={() => onSort(col)} style={{
        background: "none", border: "none", cursor: "pointer", padding: 0,
        fontSize: "inherit", fontWeight: "inherit", letterSpacing: "inherit",
        textTransform: "inherit" as any,
        color: active ? (isFl ? "var(--accent)" : "var(--accent)") : "inherit",
        display: "inline-flex", alignItems: "center", gap: 3,
        minWidth: 0, maxWidth: "100%", overflow: "hidden", whiteSpace: "nowrap",
      }}>
        {children}
        <span style={{ fontSize: 9, opacity: active ? 1 : 0.3 }}>
          {active ? (sortAsc ? "↑" : "↓") : "↕"}
        </span>
      </button>
    );
  }

  const containerStyle: React.CSSProperties = isFl ? {
    background: "var(--work-2)",
    border: "1px solid var(--line-work)",
    borderRadius: 9,
    display: "flex",
    flexDirection: "column",
    flex: 1,
    minHeight: 0,
    overflow: "hidden",
  } : {
    display: "flex",
    flexDirection: "column",
    flex: 1,
    minHeight: 0,
  };

  const headerStyle: React.CSSProperties = isFl ? {
    display: "grid", gridTemplateColumns: cols, alignItems: "center",
    padding: "0 14px",
    height: 36,
    background: "linear-gradient(var(--work-3), var(--work-2))",
    borderBottom: "1px solid var(--line-work)",
    font: "700 10px var(--font-sans)",
    letterSpacing: "1.3px", textTransform: "uppercase",
    color: "var(--ink-on-work-dim)", flexShrink: 0,
  } : {
    display: "grid", gridTemplateColumns: cols, alignItems: "center",
    padding: "0 14px 8px",
    fontSize: "var(--fs-label)", fontWeight: "var(--fw-semibold)" as any,
    letterSpacing: "var(--ls-label)", textTransform: "uppercase",
    color: "var(--text-faint)", flexShrink: 0,
  };

  return (
    <div style={containerStyle}>
      <div style={headerStyle}>
        <span />
        <Hdr col="name">{t.player.colName}</Hdr>
        <Hdr col="format">{t.player.colFormat}</Hdr>
        <Hdr col="duration">{t.player.colDuration}</Hdr>
        <Hdr col="bpm">{t.player.colBpm}</Hdr>
        <Hdr col="key">{t.player.colKey}</Hdr>
        <Hdr col="size">{t.player.colSize}</Hdr>
        <Hdr col="created">{t.player.colCreated}</Hdr>
      </div>

      <div style={{ overflowY: "auto", flex: 1, minHeight: 0 }}>
        {sorted.length === 0 && (
          <div style={{ padding: "40px 0", textAlign: "center", color: isFl ? "var(--ink-on-work-dim)" : "var(--text-faint)", fontSize: isFl ? "13px" : "var(--fs-sm)", fontFamily: isFl ? "var(--font-sans)" : undefined }}>
            {entries.length === 0 ? t.player.noFiles : t.common.nothingFound}
          </div>
        )}
        {sorted.map(({ e, i }) => {
          const active = i === activeIdx;
          const m = e.meta;
          return (
            <div
              key={e.path}
              onDoubleClick={() => onActivate(i)}
              onClick={() => onActivate(i)}
              style={{
                display: "grid",
                gridTemplateColumns: cols,
                alignItems: "center",
                height: isFl ? 40 : "var(--row-height)",
                padding: "0 14px",
                borderRadius: isFl ? 0 : "var(--radius-row)",
                borderBottom: isFl ? "1px solid var(--line-work)" : undefined,
                cursor: "pointer",
                background: active
                  ? (isFl ? "rgba(255,138,60,.13)" : "var(--accent-soft)")
                  : "transparent",
                boxShadow: isFl && active ? "inset 3px 0 0 var(--accent)" : undefined,
                transition: isFl ? undefined : "background var(--dur-fast) var(--ease-out)",
                userSelect: "none",
              }}
            >
              <span style={{ display: "flex", alignItems: "center", color: active ? "var(--accent)" : (isFl ? "var(--ink-on-work-dim)" : "var(--text-faint)") }}>
                {active ? <Icons.Play width={13} height={13} /> : <Icons.Audio width={13} height={13} />}
              </span>
              <span style={{ fontSize: isFl ? "13px" : "var(--fs-body)", fontFamily: isFl ? "var(--font-sans)" : undefined, color: isFl ? "var(--ink-on-work)" : "var(--text-body)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", paddingRight: 8 }}>
                {e.name}
              </span>
              <FlCell muted={!m} isFl={isFl}>{m ? m.format : "…"}</FlCell>
              <FlCell muted={!m} isFl={isFl}>{m ? formatDuration(m.durationS) : "…"}</FlCell>
              <FlCell muted={!m?.bpm} isFl={isFl}>{m?.bpm ? Math.round(m.bpm) : "—"}</FlCell>
              <FlCell muted={!m?.key} isFl={isFl}>
                {m?.key
                  ? <>{m.key}{m.keyConfidence != null && <span style={{ fontSize: 9, opacity: 0.6, marginLeft: 3 }}>{Math.round(m.keyConfidence * 100)}%</span>}</>
                  : "—"}
              </FlCell>
              <FlCell muted={!m} isFl={isFl}>{m ? formatBytes(m.fileSizeBytes) : "—"}</FlCell>
              {(() => { const ts = e.createdAt ?? m?.createdAt; return <FlCell muted={!ts} isFl={isFl}>{ts ? formatDate(ts) : "—"}</FlCell>; })()}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function FlCell({ children, muted, isFl }: { children: React.ReactNode; muted?: boolean; isFl: boolean }) {
  return (
    <span style={{
      fontSize: "var(--fs-sm)",
      fontFamily: "var(--font-mono)",
      color: muted
        ? (isFl ? "var(--ink-dim)" : "var(--text-faint)")
        : (isFl ? "var(--ink-on-work-dim)" : "var(--text-muted)"),
      overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap",
    }}>
      {children}
    </span>
  );
}

// ── PlayerPage ────────────────────────────────────────────────────────────────

export function PlayerPage() {
  const t = useT();
  const _theme = useSettingsStore((s) => s.settings?.theme);
  const isFl = _theme === "fl";

  const [entries, setEntries] = React.useState<FileEntry[]>([]);
  const [activeIdx, setActiveIdx] = React.useState<number | null>(null);
  const [q, setQ] = React.useState("");
  const [sortCol, setSortCol] = React.useState<SortCol>("created");
  const [sortAsc, setSortAsc] = React.useState(false);
  const [dragHover, setDragHover] = React.useState(false);

  // Подписка на события прогресса анализа из Rust.
  React.useEffect(() => {
    let unlisten: (() => void) | null = null;

    listenOnce<{ path: string; done: number; total: number; meta?: AudioMeta }>(
      "player-analysis-progress",
      (payload) => {
        if (!payload.meta) return;
        // Маппим snake_case от Tauri в camelCase.
        const raw = payload.meta as any;
        const meta: AudioMeta = {
          path: raw.path,
          name: raw.name,
          format: raw.format,
          durationS: raw.durationS,
          sampleRate: raw.sampleRate,
          channels: raw.channels,
          bitDepth: raw.bitDepth,
          fileSizeBytes: raw.fileSizeBytes,
          lufs: raw.lufs,
          peakDbfs: raw.peakDbfs,
          bpm: raw.bpm,
          key: raw.key,
          keyConfidence: raw.keyConfidence,
          peaks: raw.peaks ?? [],
          createdAt: raw.createdAt,
          error: raw.error,
        };
        setEntries((prev) =>
          prev.map((e) => e.path === meta.path ? { ...e, meta } : e)
        );
      }
    ).then((fn) => { unlisten = fn; });

    return () => { unlisten?.(); };
  }, []);

  // Drag & drop.
  React.useEffect(() => {
    let unlisten: (() => void) | null = null;
    onFileDrop(
      (paths) => addPaths(paths),
      (h) => setDragHover(h),
    ).then((fn) => { unlisten = fn; });
    return () => { unlisten?.(); };
  }, []);

  // Добавляем файлы и запускаем батч-анализ.
  const addPaths = React.useCallback(async (paths: string[]) => {
    const audio = paths.filter(isAudioPath);
    if (!audio.length) return;

    const newEntries: FileEntry[] = audio
      .filter((p) => {
        return true; // дубли по пути фильтруем ниже
      })
      .map((p) => ({ path: p, name: fileName(p), meta: null }));

    setEntries((prev) => {
      const existing = new Set(prev.map((e) => e.path));
      const fresh = newEntries.filter((e) => !existing.has(e.path));
      return [...prev, ...fresh];
    });

    if (isTauri()) {
      // Быстро получаем даты создания из FS — сортировка работает сразу.
      invoke<[string, number | null][]>("player_get_dates", { paths: audio })
        .then((dates) => {
          const map = new Map(dates);
          setEntries((prev) =>
            prev.map((e) => {
              const ts = map.get(e.path);
              return ts != null ? { ...e, createdAt: ts } : e;
            })
          );
        })
        .catch(() => {});

      // Полный анализ идёт параллельно, заполняет метаданные по мере готовности.
      invoke("player_analyze_batch", { paths: audio }).catch(() => {});
    }
  }, []);

  const handlePickFiles = async () => {
    const { open } = await import("@tauri-apps/plugin-dialog");
    const res = await open({
      multiple: true,
      directory: false,
      filters: [{ name: "Audio", extensions: ["wav", "mp3", "flac", "ogg", "aiff", "aif", "m4a", "aac"] }],
    }).catch(() => null);
    if (!res) return;
    const paths = Array.isArray(res) ? res : [res];
    addPaths(paths);
  };

  const handlePickFolder = async () => {
    const dir = await pickFolder();
    if (!dir) return;
    if (isTauri()) {
      const files = await invoke<string[]>("player_scan_folder", { dir }).catch(() => [] as string[]);
      if (files.length) await addPaths(files);
    }
  };

  const handleSort = (col: SortCol) => {
    setSortCol((prev) => {
      if (prev === col) { setSortAsc((a) => !a); return col; }
      setSortAsc(true);
      return col;
    });
  };

  const handleActivate = (i: number) => setActiveIdx(i);

  const clearAll = () => {
    setEntries([]);
    setActiveIdx(null);
  };

  const activeEntry = activeIdx != null ? entries[activeIdx] ?? null : null;

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        flex: 1,
        minHeight: 0,
        position: "relative",
        background: dragHover ? (isFl ? "rgba(255,138,60,.05)" : "var(--accent-softer)") : undefined,
        padding: isFl ? 14 : 0,
        gap: isFl ? 12 : 0,
        transition: "background var(--dur-fast) var(--ease-out)",
      }}
    >
      {/* Тулбар */}
      <div style={{
        display: "flex",
        alignItems: "center",
        gap: isFl ? 8 : 10,
        padding: isFl ? 0 : "16px 20px 10px",
        flexShrink: 0,
      }}>
        {isFl ? (
          <>
            <button onClick={handlePickFiles} style={flToolBtn}>
              <Icons.Plus width={13} height={13} />
              {t.player.addFiles}
            </button>
            <button onClick={handlePickFolder} style={flToolBtn}>
              <Icons.Folder width={13} height={13} />
              {t.player.addFolder}
            </button>
            <div style={{ flex: 1 }} />
            <div style={{ display: "flex", alignItems: "center", gap: 9, height: 36, padding: "0 12px", background: "var(--work-3)", border: "1px solid var(--line-work)", borderRadius: 7, boxShadow: "inset 0 2px 5px rgba(0,0,0,.4)", width: 220 }}>
              <Icons.Search width={13} height={13} style={{ color: "var(--ink-on-work-dim)", flexShrink: 0 }} />
              <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t.player.searchPlaceholder}
                style={{ flex: 1, background: "transparent", border: "none", outline: "none", color: "var(--ink-on-work)", font: "500 13px var(--font-sans)" }} />
            </div>
            {entries.length > 0 && (
              <button onClick={clearAll} style={{ ...flToolBtn, color: "var(--rec)" }}>
                {t.player.clearAll}
              </button>
            )}
          </>
        ) : (
          <>
            <Button size="sm" onClick={handlePickFiles}>
              <Icons.Plus width={13} height={13} />
              {t.player.addFiles}
            </Button>
            <Button size="sm" variant="ghost" onClick={handlePickFolder}>
              <Icons.Folder width={13} height={13} />
              {t.player.addFolder}
            </Button>
            <div style={{ flex: 1 }} />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t.player.searchPlaceholder} style={{ width: 220 }} />
            {entries.length > 0 && (
              <Button size="sm" variant="ghost" onClick={clearAll}>{t.player.clearAll}</Button>
            )}
          </>
        )}
      </div>

      {/* Таблица */}
      <FileTable
        entries={entries}
        activeIdx={activeIdx}
        onActivate={handleActivate}
        sortCol={sortCol}
        sortAsc={sortAsc}
        onSort={handleSort}
        q={q}
        isFl={isFl}
      />

      {/* Подсказка drag-drop когда пусто */}
      {entries.length === 0 && (
        <div style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          pointerEvents: "none",
          marginTop: 60,
        }}>
          <div style={{
            border: isFl ? "1px solid var(--line-work)" : "2px dashed var(--border)",
            borderRadius: isFl ? 9 : 16,
            padding: "40px 60px",
            textAlign: "center",
            color: isFl ? "var(--ink-on-work-dim)" : "var(--text-faint)",
            background: isFl ? "rgba(0,0,0,.15)" : undefined,
          }}>
            <Icons.Audio width={32} height={32} style={{ marginBottom: 12, opacity: 0.5 }} />
            <p style={{ margin: 0, fontSize: isFl ? "13px" : "var(--fs-body)", fontFamily: isFl ? "var(--font-sans)" : undefined }}>{t.player.dropHint}</p>
          </div>
        </div>
      )}

      {/* Плеер-бар */}
      {activeEntry && (
        <PlayerBar entry={activeEntry} onClose={() => setActiveIdx(null)} isFl={isFl} />
      )}
    </div>
  );
}

const flToolBtn: React.CSSProperties = {
  display: "flex", alignItems: "center", gap: 7,
  height: 36, padding: "0 12px",
  background: "linear-gradient(var(--btn-hi),var(--btn))",
  border: "1px solid var(--chrome-lo)", borderRadius: 7,
  color: "var(--ink)", font: "600 12.5px var(--font-sans)",
  cursor: "pointer",
  boxShadow: "inset 0 1px 0 rgba(255,255,255,.5),0 1px 2px rgba(0,0,0,.25)",
};
