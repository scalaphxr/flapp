// Вкладка «Плеер» — автономный модуль.
// Архитектура:
//   - Фронтенд: UI, воспроизведение (HTML5 <audio>), отрисовка волны (<canvas>).
//   - Rust: анализ каждого файла (BPM, тональность, LUFS, пики). Async команды.
//   - Прогресс анализа стримится через Tauri-события «player-analysis-progress».

import React from "react";
import { Icons, Input, Button } from "@/shared/ui";
import { useT } from "@/shared/i18n";
import { formatBytes, formatDuration } from "@/shared/lib/format";
import { fileName, onFileDrop, pickFolder, pickFonts, isTauri } from "@/shared/lib/tauri";
import { fileDragProps } from "@/shared/lib/dragOut";
import { parseAuthors, joinAuthors } from "@/shared/lib/authors";
import { decodeSampleName } from "@/shared/lib/decodeSampleName";
import { buildKeywords, buildHashtags, mergeRoster, parseTypeArtists } from "@/shared/lib/ytKeywords";
import { useSettingsStore } from "@/shared/model/settings";
import { useJobsStore } from "@/shared/model/jobs";
import { api } from "@/shared/api/client";

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
  /** true — быстрые метаданные из заголовка; DSP (BPM/key/пики) ещё считается. */
  partial?: boolean;
}

/** Источник записи: "manual" — добавлена руками (диалог/драг-дроп),
 *  иначе normPath наблюдаемой папки, из которой файл пришёл. Реальный путь
 *  папки всегда содержит «:» или «\», так что коллизии с "manual" нет. */
type Origin = string;

interface FileEntry {
  path: string;
  name: string;
  createdAt?: number; // из FS-метаданных, доступно сразу
  meta: AudioMeta | null; // null = ещё анализируется
  origin?: Origin; // undefined = legacy-запись без источника
}

type SortCol = "name" | "format" | "duration" | "bpm" | "key" | "type" | "size" | "created";

// ── Тайпы (виртуальные папки) ────────────────────────────────────────────────
// Биты раскладываются по «тайпам» — виртуальным папкам, существующим только
// внутри приложения (файлы на диске не двигаются). Список папок, привязка
// путь→тайп и сам плейлист живут в localStorage: переживают перезапуск и
// размонтирование вкладки, а кэш анализа в Rust мгновенно возвращает
// метаданные при восстановлении.

interface TypeFolder {
  id: string;
  name: string;
}

interface TypesState {
  folders: TypeFolder[];
  /** путь файла → id тайпа; отсутствие записи = «без тайпа». */
  assign: Record<string, string>;
}

const PLAYLIST_KEY = "flapp.player.playlist.v1";
const TYPES_KEY = "flapp.player.types.v1";

function newTypeId(): string {
  try {
    return crypto.randomUUID();
  } catch {
    return Math.random().toString(36).slice(2) + Date.now().toString(36);
  }
}

function loadPlaylist(): FileEntry[] {
  try {
    const raw = localStorage.getItem(PLAYLIST_KEY);
    if (!raw) return [];
    const saved = JSON.parse(raw) as { path?: unknown; name?: unknown; createdAt?: unknown; origin?: unknown }[];
    if (!Array.isArray(saved)) return [];
    return saved
      .filter((s): s is { path: string; name?: unknown; createdAt?: unknown; origin?: unknown } => typeof s?.path === "string")
      .map((s) => ({
        path: s.path,
        name: typeof s.name === "string" && s.name ? s.name : fileName(s.path),
        createdAt: typeof s.createdAt === "number" ? s.createdAt : undefined,
        meta: null,
        origin: typeof s.origin === "string" ? s.origin : undefined,
      }));
  } catch {
    return [];
  }
}

/** Сверяет восстановленный плейлист со списком наблюдаемых папок: файлы
 *  снятой с наблюдения папки не должны переживать перезапуск (unwatch мог
 *  не успеть сохраниться — краш, гонка со сканом). Ручные записи остаются;
 *  legacy-записи без источника привязываются к покрывающей папке, а без
 *  покрытия выбрасываются — они могли попасть в список только из папки. */
function reconcilePlaylist(entries: FileEntry[], watched: string[]): FileEntry[] {
  return entries.flatMap((e) => {
    if (e.origin === "manual") return [e];
    const cover = watched.find((d) => isUnderDir(e.path, d));
    if (e.origin) {
      const alive = !!cover || watched.some((d) => normPath(d) === e.origin);
      return alive ? [e] : [];
    }
    return cover ? [{ ...e, origin: normPath(cover) }] : [];
  });
}

function loadTypes(): TypesState {
  try {
    const raw = localStorage.getItem(TYPES_KEY);
    if (!raw) return { folders: [], assign: {} };
    const v = JSON.parse(raw) as { folders?: unknown; assign?: unknown };
    const folders = Array.isArray(v.folders)
      ? (v.folders as unknown[]).filter(
          (f): f is TypeFolder =>
            !!f && typeof (f as TypeFolder).id === "string" && typeof (f as TypeFolder).name === "string",
        )
      : [];
    const assign: Record<string, string> = {};
    if (v.assign && typeof v.assign === "object") {
      for (const [k, val] of Object.entries(v.assign as Record<string, unknown>)) {
        if (typeof val === "string") assign[k] = val;
      }
    }
    return { folders, assign };
  } catch {
    return { folders: [], assign: {} };
  }
}

// ── Папки под live-слежением ─────────────────────────────────────────────────
// «Добавить папку» ставит папку под наблюдение: Rust-вотчер (notify) шлёт
// события "player-fs-change", и новые файлы появляются в списке сами, пока
// приложение открыто. Набор папок живёт в localStorage.

const WATCHED_KEY = "flapp.player.watched.v1";

function loadWatched(): string[] {
  try {
    const raw = localStorage.getItem(WATCHED_KEY);
    if (!raw) return [];
    const v = JSON.parse(raw) as unknown;
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string" && !!x) : [];
  } catch {
    return [];
  }
}

/** Нормализует путь для сравнения (Windows: регистр и слэши не значимы). */
function normPath(p: string): string {
  return p.replace(/\//g, "\\").replace(/\\+$/, "").toLowerCase();
}

function isUnderDir(path: string, dir: string): boolean {
  return normPath(path).startsWith(normPath(dir) + "\\");
}

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
      : (style.getPropertyValue("--wave-dim").trim() || "#666666");
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
        borderRadius: 0,
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
          position: "absolute", width: 2, height: 10, background: "var(--ink)", borderRadius: 0,
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

const VOLUME_KEY = "flapp.player.volume";

export interface PlayerBarHandle {
  /** Play/pause — для горячей клавиши Space на уровне страницы. */
  toggle: () => void;
}

interface PlayerBarProps {
  entry: FileEntry;
  onClose: () => void;
  onPrev: () => void;
  onNext: () => void;
  hasPrev: boolean;
  hasNext: boolean;
  /** Трек доиграл до конца (для автоперехода к следующему). */
  onEnded: () => void;
  isFl?: boolean;
}

const PlayerBar = React.forwardRef<PlayerBarHandle, PlayerBarProps>(function PlayerBar(
  { entry, onClose, onPrev, onNext, hasPrev, hasNext, onEnded, isFl },
  ref,
) {
  const t = useT();
  const [audio] = React.useState(() => new Audio());
  const [playing, setPlaying] = React.useState(false);
  const [current, setCurrent] = React.useState(0);
  const [duration, setDuration] = React.useState(0);
  // Громкость переживает перезапуск приложения.
  const [volume, setVolume] = React.useState(() => {
    const raw = typeof localStorage === "undefined" ? null : localStorage.getItem(VOLUME_KEY);
    const v = raw == null ? NaN : Number(raw);
    return isFinite(v) ? Math.max(0, Math.min(1, v)) : 1;
  });
  const blobUrlRef = React.useRef<string | null>(null);
  const rafRef = React.useRef<number>(0);

  // Загрузка источника при смене файла. cancelled защищает от гонки: при
  // быстром переключении треков поздний результат СТАРОЙ загрузки не должен
  // перезаписать src уже выбранного нового трека.
  React.useEffect(() => {
    let cancelled = false;
    audio.pause();
    setPlaying(false);
    setCurrent(0);
    // Reset duration too — otherwise it keeps the PREVIOUS track's value
    // until the next rAF tick (or until the new file's metadata loads, which
    // can take a moment since it goes through an async Tauri file read).
    // seek() falls back to this state when audio.duration isn't ready yet,
    // so a stale (e.g. much longer) duration made it compute a wildly wrong
    // target position on the new, shorter track. Use the cached analysis
    // duration if we already have it, so seeking works immediately.
    setDuration(entry.meta?.durationS ?? 0);

    const load = async () => {
      let url: string | null = null;
      if (isTauri()) {
        // Читаем файл через Rust-команду → ArrayBuffer → Blob URL.
        // Это обходит ограничения asset-протокола и работает для всех форматов.
        // AIFF Chromium/WebView2 не декодирует — Rust перегоняет его в WAV.
        try {
          const ext = entry.path.split(".").pop()?.toLowerCase() ?? "";
          const transcode = ext === "aiff" || ext === "aif";
          const buf = await invoke<ArrayBuffer>(
            transcode ? "player_decode_to_wav" : "player_read_audio",
            { path: entry.path },
          );
          if (!cancelled) {
            const blob = new Blob([buf], { type: transcode ? "audio/wav" : audioMimeType(entry.path) });
            url = URL.createObjectURL(blob);
          }
        } catch {
          url = null;
        }
      }
      if (cancelled) {
        if (url) URL.revokeObjectURL(url);
        return;
      }
      if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current);
      blobUrlRef.current = url;
      audio.src = url ?? entry.path;
      audio.load();
      // Выбор трека = намерение его услышать: автозапуск.
      audio.play().then(() => { if (!cancelled) setPlaying(true); }).catch(() => {});
    };
    load();

    return () => { cancelled = true; };
  }, [entry.path]);

  // Полная остановка при unmount (закрыли панель крестиком или Esc) — раньше
  // аудио-элемент продолжал играть без каких-либо контролов на экране.
  React.useEffect(() => {
    return () => {
      audio.pause();
      audio.removeAttribute("src");
      audio.load();
      if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current);
      blobUrlRef.current = null;
    };
  }, [audio]);

  // Обновление времени через rAF — плавнее, чем ontimeupdate.
  React.useEffect(() => {
    const tick = () => {
      setCurrent(audio.currentTime);
      // Harvest-computed durationS first: the browser's own audio.duration
      // can be noticeably off for VBR MP3 (no reliable duration in the
      // header), which would desync the waveform/seek bar from playback.
      const audioDur = audio.duration;
      setDuration(entry.meta?.durationS || (isFinite(audioDur) ? audioDur : 0) || 0);
      rafRef.current = requestAnimationFrame(tick);
    };
    rafRef.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafRef.current);
  }, [audio, entry.meta]);

  React.useEffect(() => {
    audio.volume = volume;
    try { localStorage.setItem(VOLUME_KEY, String(volume)); } catch { /* приватный режим — не критично */ }
  }, [volume, audio]);

  // onEnded меняется на каждом рендере страницы — держим в ref, чтобы не
  // пересоздавать обработчик.
  const onEndedRef = React.useRef(onEnded);
  onEndedRef.current = onEnded;
  React.useEffect(() => {
    audio.onended = () => { setPlaying(false); onEndedRef.current(); };
    return () => { audio.onended = null; };
  }, [audio]);

  // audio.paused — источник истины (state может отставать при вызове извне).
  const togglePlay = React.useCallback(() => {
    if (audio.paused) audio.play().then(() => setPlaying(true)).catch(() => {});
    else { audio.pause(); setPlaying(false); }
  }, [audio]);

  React.useImperativeHandle(ref, () => ({ toggle: togglePlay }), [togglePlay]);

  const stop = () => {
    audio.pause();
    audio.currentTime = 0;
    setPlaying(false);
  };

  const seek = (ratio: number) => {
    const audioDur = audio.duration;
    const dur = duration || (isFinite(audioDur) ? audioDur : 0);
    if (!(dur > 0)) return; // not loaded yet (e.g. right after switching tracks) — no-op instead of a bogus jump
    const target = Math.max(0, Math.min(dur, Math.max(0, Math.min(1, ratio)) * dur));
    audio.currentTime = target;
    setCurrent(target);
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
          <span {...fileDragProps(() => [entry.path])} style={{ flex: 1, font: "600 13px var(--font-sans)", color: "var(--ink)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", cursor: "grab" }} title={t.player.hotkeys}>
            {decodeSampleName(entry.name)}
          </span>
          {entry.meta?.error ? (
            <span style={{ font: "400 11px var(--font-mono)", color: "var(--rec, #ff453a)", flexShrink: 0 }} title={entry.meta.error}>
              {t.player.analyzeError}
            </span>
          ) : entry.meta && (
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
          {/* Prev */}
          <FlTBtn onClick={onPrev} disabled={!hasPrev} title={t.player.prevTrack}>
            <Icons.SkipBack width={11} height={11} />
          </FlTBtn>
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
          {/* Next */}
          <FlTBtn onClick={onNext} disabled={!hasNext} title={t.player.nextTrack}>
            <Icons.SkipFwd width={11} height={11} />
          </FlTBtn>

          {/* LCD time */}
          <div style={{ display: "flex", alignItems: "center", gap: 0, background: "var(--lcd-bg)", borderRadius: 0, padding: "3px 10px", boxShadow: "inset 0 2px 6px rgba(0,0,0,.7)" }}>
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
            style={{ flex: 1, height: 12, background: "var(--groove)", borderRadius: 0, cursor: "pointer", position: "relative", overflow: "hidden", border: "1px solid var(--chrome-lo)", boxShadow: "inset 0 1px 3px rgba(0,0,0,.5)" }}
            onClick={(e) => {
              const rect = e.currentTarget.getBoundingClientRect();
              seek((e.clientX - rect.left) / rect.width);
            }}
          >
            <div style={{ position: "absolute", left: 0, top: 0, bottom: 0, width: `${progress * 100}%`, background: "var(--lcd-green)", borderRadius: 0, boxShadow: "0 0 6px rgba(141,255,106,.5)" }} />
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
        <span {...fileDragProps(() => [entry.path])} style={{ flex: 1, fontWeight: "var(--fw-semibold)" as any, fontSize: "var(--fs-body)", color: "var(--text-body)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", cursor: "grab" }} title={t.player.hotkeys}>
          {decodeSampleName(entry.name)}
        </span>
        {entry.meta?.error ? (
          <span style={{ fontSize: 11, color: "var(--rec, #ff453a)", fontFamily: "var(--font-mono)", flexShrink: 0 }} title={entry.meta.error}>
            {t.player.analyzeError}
          </span>
        ) : entry.meta && (
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
        <button onClick={onPrev} disabled={!hasPrev} title={t.player.prevTrack} style={{ ...btnStyle, opacity: hasPrev ? 1 : 0.35, cursor: hasPrev ? "pointer" : "default" }}>
          <Icons.SkipBack />
        </button>
        <button onClick={stop} style={btnStyle}>
          <Icons.Stop />
        </button>
        <button onClick={togglePlay} style={{ ...btnStyle, color: "var(--accent)", fontSize: 20 }}>
          {playing ? <Icons.Pause /> : <Icons.Play />}
        </button>
        <button onClick={onNext} disabled={!hasNext} title={t.player.nextTrack} style={{ ...btnStyle, opacity: hasNext ? 1 : 0.35, cursor: hasNext ? "pointer" : "default" }}>
          <Icons.SkipFwd />
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
});

function FlTBtn({ onClick, active, disabled, title, children }: { onClick: () => void; active?: boolean; disabled?: boolean; title?: string; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      title={title}
      style={{
        width: 30, height: 30, borderRadius: 0, display: "flex", alignItems: "center", justifyContent: "center",
        cursor: disabled ? "default" : "pointer",
        opacity: disabled ? 0.45 : 1,
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
  borderRadius: 0,
};

// ── Таблица файлов ────────────────────────────────────────────────────────────

function FileTable({
  entries,
  emptyText,
  activePath,
  onActivate,
  onRemove,
  sortCol,
  sortAsc,
  onSort,
  typeFolders,
  typeOf,
  onAssign,
  onYt,
  isFl,
}: {
  /** Уже отфильтрованный и отсортированный список (порядок общий с prev/next). */
  entries: FileEntry[];
  /** Что показать при пустом списке (страница различает «пусто»/«не найдено»/«пустой тайп»). */
  emptyText: string;
  activePath: string | null;
  onActivate: (path: string) => void;
  onRemove: (path: string) => void;
  sortCol: SortCol;
  sortAsc: boolean;
  onSort: (col: SortCol) => void;
  typeFolders: TypeFolder[];
  typeOf: (path: string) => string | null;
  onAssign: (path: string, id: string | null) => void;
  onYt: (path: string) => void;
  isFl: boolean;
}) {
  const t = useT();

  // Клавиатурная навигация уводит выбор за пределы видимой области —
  // подкручиваем список к активной строке.
  const activeRowRef = React.useRef<HTMLDivElement | null>(null);
  React.useEffect(() => {
    activeRowRef.current?.scrollIntoView({ block: "nearest" });
  }, [activePath]);

  // Колонка тональности 84px («F#min»), тайпа 100px (select с именем папки),
  // в конце — кнопки YouTube и удаления.
  const cols = "36px minmax(160px,1fr) 68px 68px 52px 84px 100px 74px 120px 28px 28px";

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
    borderRadius: 0,
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
        <Hdr col="type">{t.player.colType}</Hdr>
        <Hdr col="size">{t.player.colSize}</Hdr>
        <Hdr col="created">{t.player.colCreated}</Hdr>
        <span />
        <span />
      </div>

      <div style={{ overflowY: "auto", flex: 1, minHeight: 0 }}>
        {entries.length === 0 && (
          <div style={{ padding: "40px 0", textAlign: "center", color: isFl ? "var(--ink-on-work-dim)" : "var(--text-faint)", fontSize: isFl ? "13px" : "var(--fs-sm)", fontFamily: isFl ? "var(--font-sans)" : undefined }}>
            {emptyText}
          </div>
        )}
        {entries.map((e) => {
          const active = e.path === activePath;
          const m = e.meta;
          const failed = !!m?.error;
          return (
            <div
              key={e.path}
              ref={active ? activeRowRef : undefined}
              onClick={() => onActivate(e.path)}
              {...fileDragProps(() => [e.path])}
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
              <span
                title={failed ? `${t.player.analyzeError}: ${m!.error}` : undefined}
                style={{ display: "flex", alignItems: "center", color: failed ? "var(--rec, #ff453a)" : active ? "var(--accent)" : (isFl ? "var(--ink-on-work-dim)" : "var(--text-faint)") }}
              >
                {failed ? <Icons.Info width={13} height={13} /> : active ? <Icons.Play width={13} height={13} /> : <Icons.Audio width={13} height={13} />}
              </span>
              <span style={{ fontSize: isFl ? "13px" : "var(--fs-body)", fontFamily: isFl ? "var(--font-sans)" : undefined, color: isFl ? "var(--ink-on-work)" : "var(--text-body)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", paddingRight: 8 }}>
                {decodeSampleName(e.name)}
              </span>
              <FlCell muted={!m} isFl={isFl}>{failed ? "—" : m ? m.format : "…"}</FlCell>
              <FlCell muted={!m} isFl={isFl}>{failed ? "—" : m ? formatDuration(m.durationS) : "…"}</FlCell>
              <FlCell muted={!m?.bpm} isFl={isFl}>{m?.bpm ? Math.round(m.bpm) : m?.partial ? "…" : "—"}</FlCell>
              <FlCell muted={!m?.key} isFl={isFl}>
                {m?.key ?? (m?.partial ? "…" : "—")}
              </FlCell>
              {/* Селект тайпа: stopPropagation, чтобы клик не активировал строку. */}
              <span onClick={(ev) => ev.stopPropagation()} style={{ display: "flex", minWidth: 0, paddingRight: 8 }}>
                <select
                  value={typeOf(e.path) ?? ""}
                  onChange={(ev) => onAssign(e.path, ev.target.value || null)}
                  title={t.player.colType}
                  style={{
                    width: "100%",
                    minWidth: 0,
                    background: "transparent",
                    border: `1px solid ${isFl ? "var(--line-work)" : "var(--border)"}`,
                    borderRadius: 0,
                    padding: "2px 3px",
                    fontSize: "var(--fs-sm)",
                    fontFamily: "var(--font-mono)",
                    color: typeOf(e.path)
                      ? (isFl ? "var(--ink-on-work)" : "var(--text-muted)")
                      : (isFl ? "var(--ink-dim)" : "var(--text-faint)"),
                    cursor: "pointer",
                    outline: "none",
                    textOverflow: "ellipsis",
                  }}
                >
                  <option value="">—</option>
                  {typeFolders.map((f) => (
                    <option key={f.id} value={f.id}>{f.name}</option>
                  ))}
                </select>
              </span>
              <FlCell muted={!m} isFl={isFl}>{m ? formatBytes(m.fileSizeBytes) : "—"}</FlCell>
              {(() => { const ts = e.createdAt ?? m?.createdAt; return <FlCell muted={!ts} isFl={isFl}>{ts ? formatDate(ts) : "—"}</FlCell>; })()}
              <YtRowBtn title={t.player.ytUploadOne} isFl={isFl} onClick={(ev) => { ev.stopPropagation(); onYt(e.path); }} />
              <RemoveBtn title={t.player.remove} isFl={isFl} onClick={(ev) => { ev.stopPropagation(); onRemove(e.path); }} />
            </div>
          );
        })}
      </div>
    </div>
  );
}

/** Кнопка «выложить на YouTube» в строке: приглушена, подсвечивается при наведении. */
function YtRowBtn({ onClick, title, isFl }: { onClick: (e: React.MouseEvent) => void; title: string; isFl: boolean }) {
  const [hover, setHover] = React.useState(false);
  return (
    <button
      title={title}
      onClick={onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        background: "none", border: "none", cursor: "pointer",
        padding: 4, display: "flex", alignItems: "center", justifyContent: "center",
        color: hover ? "var(--accent)" : (isFl ? "var(--ink-dim)" : "var(--text-faint)"),
        opacity: hover ? 1 : 0.55,
      }}
    >
      <Icons.Yt width={13} height={13} />
    </button>
  );
}

/** Крестик удаления строки: приглушён, подсвечивается при наведении. */
function RemoveBtn({ onClick, title, isFl }: { onClick: (e: React.MouseEvent) => void; title: string; isFl: boolean }) {
  const [hover, setHover] = React.useState(false);
  return (
    <button
      title={title}
      onClick={onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        background: "none", border: "none", cursor: "pointer",
        padding: 4, display: "flex", alignItems: "center", justifyContent: "center",
        color: hover ? "var(--rec, #ff453a)" : (isFl ? "var(--ink-dim)" : "var(--text-faint)"),
        opacity: hover ? 1 : 0.55,
      }}
    >
      <Icons.X width={12} height={12} />
    </button>
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

// ── Чипы тайпов ───────────────────────────────────────────────────────────────

function TypeChip({
  label,
  count,
  active,
  dropTarget,
  isFl,
  title,
  onClick,
  onDoubleClick,
  onRename,
  renameTitle,
  onDelete,
  deleteTitle,
  chipRef,
}: {
  label: string;
  count?: number;
  active: boolean;
  /** Над чипом висит нативный драг файлов — подсветить как цель дропа. */
  dropTarget?: boolean;
  isFl: boolean;
  title?: string;
  onClick: () => void;
  onDoubleClick?: () => void;
  onRename?: () => void;
  renameTitle?: string;
  onDelete?: () => void;
  deleteTitle?: string;
  chipRef?: (el: HTMLElement | null) => void;
}) {
  // Карандаш/крестик скрыты; показываются только по правому клику (ПКМ) на чип.
  const [revealed, setRevealed] = React.useState(false);
  const showBtns = revealed && !!(onRename || onDelete);
  const style: React.CSSProperties = isFl
    ? {
        display: "inline-flex", alignItems: "center", gap: 6,
        height: 28, padding: showBtns ? "0 5px 0 11px" : "0 11px",
        borderRadius: 0, cursor: "pointer", userSelect: "none", flexShrink: 0,
        font: "600 11.5px var(--font-sans)",
        background: active
          ? "linear-gradient(var(--accent), var(--accent-deep, #e8651e))"
          : "linear-gradient(var(--btn-hi), var(--btn))",
        border: dropTarget ? "1px dashed var(--lcd-green, #8dff6a)" : "1px solid var(--chrome-lo)",
        color: active ? "#fff" : "var(--ink)",
        boxShadow: dropTarget
          ? "0 0 8px rgba(141,255,106,.5)"
          : active
            ? "inset 0 1px 0 rgba(255,255,255,.3), 0 0 8px rgba(255,138,60,.4)"
            : "inset 0 1px 0 rgba(255,255,255,.5), 0 1px 2px rgba(0,0,0,.25)",
      }
    : {
        display: "inline-flex", alignItems: "center", gap: 6,
        height: 26, padding: showBtns ? "0 4px 0 11px" : "0 11px",
        borderRadius: 0, cursor: "pointer", userSelect: "none", flexShrink: 0,
        fontSize: "var(--fs-sm)", fontWeight: "var(--fw-semibold)" as any,
        background: active ? "var(--accent-soft)" : "transparent",
        border: dropTarget
          ? "1px dashed var(--accent)"
          : `1px solid ${active ? "var(--accent)" : "var(--border)"}`,
        color: active || dropTarget ? "var(--accent)" : "var(--text-muted)",
      };
  return (
    <span
      ref={chipRef}
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      onContextMenu={(e) => { if (onRename || onDelete) { e.preventDefault(); setRevealed(true); } }}
      onMouseLeave={() => setRevealed(false)}
      title={title}
      style={style}
    >
      <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: 180 }}>{label}</span>
      {count != null && (
        <span style={{ opacity: 0.6, fontSize: 10, fontFamily: "var(--font-mono)" }}>{count}</span>
      )}
      {revealed && onRename && (
        <span
          title={renameTitle}
          onClick={(e) => { e.stopPropagation(); setRevealed(false); onRename(); }}
          onDoubleClick={(e) => e.stopPropagation()}
          style={{
            display: "flex", alignItems: "center", justifyContent: "center",
            padding: 3, borderRadius: 0, color: "inherit", opacity: 0.9,
          }}
        >
          <Icons.Pencil width={10} height={10} />
        </span>
      )}
      {revealed && onDelete && (
        <span
          title={deleteTitle}
          onClick={(e) => { e.stopPropagation(); setRevealed(false); onDelete(); }}
          style={{
            display: "flex", alignItems: "center", justifyContent: "center",
            padding: 3, borderRadius: 0, color: "inherit", opacity: 0.9,
          }}
        >
          <Icons.X width={10} height={10} />
        </span>
      )}
    </span>
  );
}

// ── Загрузка на YouTube (механика TunesToTube) ───────────────────────────────
// Обложка + бит → ffmpeg-рендер на бэкенде → resumable upload в канал.
// Диалог принимает пачку битов (текущий тайп или один бит), разворачивает
// шаблон названия и ставит по джобе на бит; статусы стримятся через SSE.

interface YtBeat {
  path: string;
  name: string;
  typeName: string;
  bpm?: number;
  key?: string;
}

type PinImage = import("@/shared/api/types").CoverImage;

/** Вычленяет название бита из имени файла по конвенции
 * «НАЗВАНИЕ BPM АВТОР [СОАВТОР]» (например «DEAD BATTERY 156 FLIPGOD SORA»
 * → название «DEAD BATTERY», bpm 156). BPM-токен — число, совпадающее с
 * проанализированным темпом, иначе первое «голое» число 40–240 не в начале
 * имени. Если BPM-токена нет, названием считается всё имя целиком. */
function beatTitleFromStem(stem: string, analyzedBpm?: number): { title: string; bpm?: number } {
  const tokens = stem.trim().split(/\s+/);
  let idx = -1;
  if (analyzedBpm) {
    const r = String(Math.round(analyzedBpm));
    idx = tokens.findIndex((tk, i) => i > 0 && tk === r);
  }
  if (idx < 0) {
    idx = tokens.findIndex((tk, i) => i > 0 && /^\d{2,3}$/.test(tk) && +tk >= 40 && +tk <= 240);
  }
  if (idx <= 0) return { title: stem.trim() };
  return { title: tokens.slice(0, idx).join(" "), bpm: Number(tokens[idx]) };
}

/** Убирает токен-ник продюсера из разобранного названия бита: программа знает
 * ник и не тащит его в {name}. В именах вроде «FLIPGOD 4EVER» или
 * «4EVER FLIPGOD» слово-ник — это тег автора, а не часть названия. Если после
 * вычистки ничего не остаётся (имя было только ником) — возвращаем как есть. */
function stripNick(title: string, nick: string): string {
  const n = nick.trim().toLowerCase();
  if (!n) return title;
  const kept = title.split(/\s+/).filter((w) => w.toLowerCase() !== n);
  return kept.join(" ").trim() || title;
}

/** Подставляет {name}/{type}/{bpm}/{key}/{nick}/{authors}/{year} без чистки —
 * годится и для многострочного описания (переводы строк сохраняются). {name} —
 * только название бита, без BPM, авторов и ника продюсера из имени файла; {bpm}
 * берётся из анализа, а при его отсутствии — из имени файла; {nick} — тег
 * продюсера из настроек; {authors} — все продюсеры бита (свой ник + соавторы);
 * {year} — текущий год (SEO-свежесть). */
function renderYtVars(tpl: string, b: YtBeat, nick: string, authors: string[], roster = ""): string {
  const stem = b.name.replace(/\.[^.]+$/, "");
  const parsed = beatTitleFromStem(stem, b.bpm);
  const bpm = b.bpm ? Math.round(b.bpm) : parsed.bpm;
  // Ключевики/хэштеги — по тайпу, в котором выкладывается бит (напр. «NBA
  // YoungBoy»), а не по авторам из имени файла: именно тайп ищут на YouTube.
  const typeArtists = parseTypeArtists(b.typeName);
  const year = new Date().getFullYear();
  return tpl
    .split("{name}").join(stripNick(parsed.title, nick))
    .split("{type}").join(b.typeName)
    .split("{bpm}").join(bpm ? String(bpm) : "")
    .split("{key}").join(b.key ?? "")
    .split("{nick}").join(nick.trim())
    .split("{authors}").join(joinAuthors(authors))
    .split("{year}").join(String(year))
    .split("{keywords}").join(buildKeywords(typeArtists, roster, year))
    .split("{hashtags}").join(buildHashtags(typeArtists));
}

/** Разворачивает шаблон «{type} type beat — {name}» и подчищает мусорные
 * разделители от пустых подстановок. */
function renderYtTemplate(tpl: string, b: YtBeat, nick: string, authors: string[]): string {
  const stem = b.name.replace(/\.[^.]+$/, "");
  let s = renderYtVars(tpl, b, nick, authors);
  s = s.replace(/\s{2,}/g, " ").replace(/^[\s—\-·,|]+/, "").replace(/[\s—\-·,|]+$/, "");
  return s.trim() || beatTitleFromStem(stem, b.bpm).title;
}

/** Референс-шаблон описания в стиле тайп-бит канала: инфо-блок → разделитель
 * IGNORE → авто-стена {keywords} → строка {hashtags}. Всегда доступен в
 * выпадашке пресетов, чтобы существующие пользователи (у кого сохранён старый
 * шаблон) могли переключиться на новый одним кликом, не теряя свой. Совпадает с
 * defaultDescription в settings.go. */
const REFERENCE_DESC = `{type} Type Beat "{name}" | prod. {authors}
📩 WAV / exclusive: your@email.com  ·  💿 FREE for non-profit (credit "prod. {authors}")

• {bpm} BPM · Key {key}
• Buy / lease: your-beatstars-link.com
• Subscribe for daily {type} type beats 🔔

⚠️ FREE for non-profit use only — you MUST credit "prod. {authors}" in your title.
For-profit / exclusive rights: contact me. Unauthorized use (no lease/exclusive) is
copyright infringement, subject to DMCA takedown.

{hashtags}

IGNORE ↓
________________________________________
{keywords}`;

/** Рекомендованные шаблоны названия в стиле реальных тайп-бит видео (артист
 * первым, год, prod-кредит). Всегда доступны в выпадашке пресетов, не затирая
 * активный шаблон. Совпадает с YtTitleTemplates в settings.go. */
const REFERENCE_TITLES = [
  `[FREE] {type} Type Beat "{name}" (prod. {authors})`,
  `[FREE] {type} Type Beat "{name}" | {year}`,
  `{type} Type Beat ~ "{name}" | {bpm} BPM {key}`,
  `[FREE FOR PROFIT] {type} Type Beat "{name}" | Trap Melodic {year}`,
  `{type} type beat — {name}`,
];

/** Ручная правка одного видео пачки. Незаданное поле наследуется из шаблона —
 * так правка шаблона не затирает то, что уже поправили руками. */
type BeatEdit = { title?: string; desc?: string; tags?: string; privacy?: string };

/** Полный список авторов бита: распознанные из имени (+ свой ник первым) плюс
 * ручные добавления пачки. Псевдонимы/удаления применяются внутри parseAuthors. */
function beatAuthors(b: YtBeat, nick: string, aliases: Record<string, string>, extras: string[]): string[] {
  const stem = b.name.replace(/\.[^.]+$/, "");
  const list = parseAuthors(stem, { nick, aliases, bpm: b.bpm });
  const seen = new Set(list.map((a) => a.toLowerCase()));
  for (const e of extras) {
    const t = e.trim();
    if (t && !seen.has(t.toLowerCase())) { seen.add(t.toLowerCase()); list.push(t); }
  }
  return list;
}

/** Строки текстового наложения на кадр: название бита в кавычках (крупно) и
 * «prod. авторы» помельче. Те же строки уходят и в превью, и в ffmpeg-рендер. */
function overlayLinesFor(b: YtBeat, nick: string, authors: string[]): { title: string; sub: string } {
  const stem = b.name.replace(/\.[^.]+$/, "");
  const name = stripNick(beatTitleFromStem(stem, b.bpm).title, nick);
  const sub = authors.length ? `prod. ${joinAuthors(authors)}` : "";
  return { title: name ? `"${name}"` : "", sub };
}

/** Шрифты наложения: ключ (совпадает с FontFiles в Go) → подпись + CSS-семейство
 * для предпросмотра. Список — только системные шрифты Windows. */
const YT_FONTS: { key: string; label: string; css: string }[] = [
  { key: "arial",     label: "Arial",           css: "Arial, sans-serif" },
  { key: "impact",    label: "Impact",          css: "Impact, sans-serif" },
  { key: "franklin",  label: "Franklin Gothic", css: "'Franklin Gothic Medium', sans-serif" },
  { key: "verdana",   label: "Verdana",         css: "Verdana, sans-serif" },
  { key: "tahoma",    label: "Tahoma",          css: "Tahoma, sans-serif" },
  { key: "trebuchet", label: "Trebuchet MS",    css: "'Trebuchet MS', sans-serif" },
  { key: "segoe",     label: "Segoe UI",        css: "'Segoe UI', sans-serif" },
  { key: "georgia",   label: "Georgia",         css: "Georgia, serif" },
  { key: "times",     label: "Times New Roman", css: "'Times New Roman', serif" },
  { key: "comic",     label: "Comic Sans MS",   css: "'Comic Sans MS', cursive" },
  { key: "courier",   label: "Courier New",     css: "'Courier New', monospace" },
];

function cssFontFor(key: string): string {
  return YT_FONTS.find((f) => f.key === key)?.css ?? "Arial, sans-serif";
}

/** Свой шрифт задаётся путём к файлу, системный — ключом из YT_FONTS. */
function isCustomFont(key: string): boolean {
  return /[\\/]/.test(key);
}

/** Подпись своего шрифта в списке — имя файла без расширения. */
function fontLabel(path: string): string {
  return fileName(path).replace(/\.(ttf|otf)$/i, "");
}

/** Левая панель: все видео пачки сразу — обложка, итоговое название, авторы
 * этого бита, точка «правлено руками» и статус загрузки. Обзор, которого не
 * было: раньше всё это лежало в конце длинного скролла. */
function VideoList({ beats, focus, setFocus, assign, edits, extras, nick, aliases, resolveBeat, statusFor, onPick, onCycle, onReshuffle, busy, isFl }: {
  beats: YtBeat[];
  focus: string;
  setFocus: (p: string) => void;
  assign: Record<string, PinImage>;
  edits: Record<string, BeatEdit>;
  extras: Record<string, string[]>;
  nick: string;
  aliases: Record<string, string>;
  resolveBeat: (b: YtBeat) => { title: string };
  statusFor: (p: string) => { text: string; failed?: boolean; url?: string };
  onPick: (p: string) => void;
  onCycle: (p: string) => void;
  onReshuffle: () => void;
  busy: boolean;
  isFl: boolean;
}) {
  const t = useT();
  const line = isFl ? "var(--line-work)" : "var(--border)";
  return (
    <div style={{ width: 290, flexShrink: 0, borderRight: `1px solid ${line}`, display: "flex", flexDirection: "column", background: isFl ? "var(--work-2)" : "var(--surface-1, transparent)" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 6, padding: "8px 10px", borderBottom: `1px solid ${line}`, flexShrink: 0 }}>
        <span style={{ fontSize: 10, fontWeight: 700, letterSpacing: "1px", textTransform: "uppercase", color: isFl ? "var(--ink-dim)" : "var(--text-faint)" }}>
          {t.player.ytAllVideos}
        </span>
        <button
          onClick={onReshuffle}
          disabled={busy}
          title={t.player.ytCoversHint}
          style={{
            marginLeft: "auto", border: `1px solid ${line}`, borderRadius: 0, background: "transparent",
            color: isFl ? "var(--ink-dim)" : "var(--text-muted)", fontSize: 10, padding: "2px 7px",
            cursor: busy ? "default" : "pointer", opacity: busy ? 0.5 : 1,
          }}
        >🎲 {t.player.ytCoversReshuffle}</button>
      </div>
      <div style={{ flex: 1, overflowY: "auto" }}>
        {beats.map((b) => {
          const st = statusFor(b.path);
          const cover = assign[b.path];
          const sel = focus === b.path;
          const authors = beatAuthors(b, nick, aliases, extras[b.path] ?? []);
          // В списке — короткое имя бита (его и опознают), а не полный шаблонный
          // тайтл: у всех строк одинаковое начало «[FREE] … Type Beat», из-за
          // чего различающее имя обрезалось. Полный итоговый тайтл — в подсказке.
          const beatName = stripNick(beatTitleFromStem(b.name.replace(/\.[^.]+$/, ""), b.bpm).title, nick) || resolveBeat(b).title;
          return (
            <div
              key={b.path}
              title={resolveBeat(b).title}
              onClick={() => setFocus(b.path)}
              style={{
                display: "flex", alignItems: "center", gap: 7, padding: "7px 9px",
                borderBottom: `1px solid ${line}`, cursor: "pointer",
                background: sel ? (isFl ? "var(--work-3)" : "var(--surface-3)") : "transparent",
                boxShadow: sel ? "inset 2px 0 0 var(--accent)" : undefined,
              }}
            >
              {cover?.thumb ? (
                <img
                  src={cover.thumb} alt="" loading="lazy"
                  title={t.player.ytCoverPick}
                  onClick={(e) => { e.stopPropagation(); onPick(b.path); }}
                  style={{ width: 52, height: 34, objectFit: "cover", borderRadius: 0, flexShrink: 0, background: "#000", border: `1px solid ${line}` }}
                />
              ) : (
                <div
                  title={t.player.ytCoverPick}
                  onClick={(e) => { e.stopPropagation(); onPick(b.path); }}
                  style={{ width: 52, height: 34, borderRadius: 0, flexShrink: 0, display: "flex", alignItems: "center", justifyContent: "center", background: isFl ? "var(--groove)" : "var(--surface-3)", border: `1px solid ${line}`, color: isFl ? "var(--ink-dim)" : "var(--text-faint)" }}
                >
                  <Icons.Search width={11} height={11} />
                </div>
              )}
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: "flex", alignItems: "center", gap: 5 }}>
                  {edits[b.path] && (
                    <span title={t.player.ytEdited} style={{ width: 5, height: 5, borderRadius: "50%", background: "var(--accent)", flexShrink: 0 }} />
                  )}
                  <span style={{ flex: 1, fontSize: 13, fontWeight: 600, lineHeight: 1.3, color: isFl ? "var(--ink-on-work)" : "var(--text-body)", overflow: "hidden", display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical" }}>
                    {beatName}
                  </span>
                </div>
                {/* Тип и авторы — по ним видно разницу битов, не открывая каждый */}
                <span style={{ fontSize: 10.5, color: isFl ? "var(--ink-dim)" : "var(--text-faint)", overflow: "hidden", whiteSpace: "nowrap", textOverflow: "ellipsis", display: "block" }}>
                  {b.typeName}{authors.length ? ` · ${authors.join(" × ")}` : ""}
                </span>
                {st.text && (
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 9.5, color: st.failed ? "var(--rec, #ff453a)" : st.url ? "var(--positive, #46d46a)" : (isFl ? "var(--ink-dim)" : "var(--text-faint)") }}>
                    {st.text}
                  </span>
                )}
              </div>
              <button
                onClick={(e) => { e.stopPropagation(); onCycle(b.path); }}
                title={t.player.ytCoverCycle}
                style={{ border: `1px solid ${line}`, borderRadius: 0, background: "transparent", width: 22, height: 22, padding: 0, cursor: "pointer", flexShrink: 0, fontSize: 11 }}
              >🎲</button>
            </div>
          );
        })}
      </div>
    </div>
  );
}

/** Инлайн-редактор выбранного видео пачки. Поля показывают итоговые значения
 * (свои либо из шаблона); правка делает их своими, ↺ возвращает шаблону. */
function BeatEditor({ b, v, e, onEdit, isFl }: {
  b: YtBeat;
  v: { title: string; description: string; tags: string; privacy: string };
  e: BeatEdit;
  onEdit: (path: string, patch: BeatEdit) => void;
  isFl: boolean;
}) {
  const t = useT();
  const line = isFl ? "var(--line-work)" : "var(--border)";
  const field: React.CSSProperties = {
    width: "100%",
    padding: "5px 8px",
    background: isFl ? "var(--groove)" : "var(--surface-input, var(--surface-3))",
    border: `1px solid ${line}`,
    borderRadius: 0,
    color: isFl ? "var(--ink-on-work)" : "var(--text-body)",
    fontFamily: "var(--font-sans)",
    fontSize: 12,
    outline: "none",
  };
  const cap: React.CSSProperties = {
    fontSize: 10, fontWeight: 700, letterSpacing: "0.8px", textTransform: "uppercase",
    color: isFl ? "var(--ink-dim)" : "var(--text-faint)",
  };

  // ↺ активна только когда поле переопределено — иначе сбрасывать нечего.
  const reset = (k: keyof BeatEdit) => (
    <button
      onClick={() => onEdit(b.path, { [k]: undefined })}
      disabled={e[k] === undefined}
      title={t.player.ytResetToTemplate}
      style={{
        border: `1px solid ${line}`, borderRadius: 0, background: "transparent",
        color: isFl ? "var(--ink-dim)" : "var(--text-faint)",
        cursor: e[k] === undefined ? "default" : "pointer",
        opacity: e[k] === undefined ? 0.35 : 1,
        width: 22, height: 20, lineHeight: 1, flexShrink: 0, fontSize: 12,
      }}
    >↺</button>
  );

  const head = (label: string, k: keyof BeatEdit) => (
    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 6 }}>
      <span style={cap}>{label}</span>
      {reset(k)}
    </div>
  );

  return (
    <div
      onClick={(ev) => ev.stopPropagation()}
      style={{ display: "flex", flexDirection: "column", gap: 6, padding: "8px 10px 10px 10px", background: isFl ? "var(--work-2)" : "var(--surface-2, transparent)" }}
    >
      {head(t.player.ytEditTitle, "title")}
      <input value={v.title} onChange={(ev) => onEdit(b.path, { title: ev.target.value })} style={field} spellCheck={false} />

      {head(t.player.ytEditDesc, "desc")}
      <textarea value={v.description} onChange={(ev) => onEdit(b.path, { desc: ev.target.value })} style={{ ...field, resize: "vertical", fontFamily: "var(--font-sans)", minHeight: 150, lineHeight: 1.45 }} spellCheck={false} />

      <div style={{ display: "flex", gap: 8 }}>
        <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 4 }}>
          {head(t.player.ytEditTags, "tags")}
          <input value={v.tags} onChange={(ev) => onEdit(b.path, { tags: ev.target.value })} style={field} spellCheck={false} />
        </div>
        <div style={{ width: 150, display: "flex", flexDirection: "column", gap: 4 }}>
          {head(t.player.ytPrivacy, "privacy")}
          <select value={v.privacy} onChange={(ev) => onEdit(b.path, { privacy: ev.target.value })} style={{ ...field, cursor: "pointer" }}>
            <option value="public">{t.player.ytPrivacyPublic}</option>
            <option value="unlisted">{t.player.ytPrivacyUnlisted}</option>
            <option value="private">{t.player.ytPrivacyPrivate}</option>
          </select>
        </div>
      </div>
    </div>
  );
}

function YtUploadDialog({ beats, isFl, onClose }: { beats: YtBeat[]; isFl: boolean; onClose: () => void }) {
  const t = useT();
  const settings = useSettingsStore((s) => s.settings);
  const updateSettings = useSettingsStore((s) => s.update);
  const jobs = useJobsStore((s) => s.jobs);

  const [image, setImage] = React.useState(settings?.ytDefaultImage ?? "");
  const [nick, setNick] = React.useState(settings?.ytNickname ?? "");
  const [overlay, setOverlay] = React.useState(!(settings?.ytNoTextOverlay));
  const [font, setFont] = React.useState(settings?.ytFont || "arial");
  // Авторы: карта правок (persist в настройки — «память») + ручные добавления
  // пачки (session). Редактирование чипсов пишет в эти два состояния.
  const [aliases, setAliases] = React.useState<Record<string, string>>(settings?.ytAuthorAliases ?? {});
  // Ручные добавления авторов — по биту: авторы распознаются из имени файла,
  // значит это свойство видео. Общим списком они приклеивались ко всем битам
  // пачки сразу.
  const [extras, setExtras] = React.useState<Record<string, string[]>>({});
  // Ручные правки конкретных видео пачки: undefined у поля = наследуется из
  // шаблона, заданное = своё. Поэтому правка шаблона переписывает только те
  // видео, где поле не трогали. Живут в диалоге: они про эту пачку.
  const [edits, setEdits] = React.useState<Record<string, BeatEdit>>({});
  const [editIdx, setEditIdx] = React.useState<number | null>(null);
  const [editVal, setEditVal] = React.useState("");
  const [addVal, setAddVal] = React.useState("");
  const [previewUrl, setPreviewUrl] = React.useState<string | null>(null);
  const [previewBusy, setPreviewBusy] = React.useState(false);
  const [previewErr, setPreviewErr] = React.useState<string | null>(null);
  const [tpl, setTpl] = React.useState(settings?.ytTitleTemplate || '[FREE] {type} Type Beat "{name}" (prod. {authors})');
  const [desc, setDesc] = React.useState(settings?.ytDescription ?? "");
  const [tags, setTags] = React.useState(settings?.ytTags ?? "");
  const [privacy, setPrivacy] = React.useState(settings?.ytPrivacy || "public");
  const [roster, setRoster] = React.useState(settings?.ytKeywordRoster ?? "");
  const [rosterAutoGrow, setRosterAutoGrow] = React.useState(settings?.ytRosterAutoGrow ?? true);
  const [ytOk, setYtOk] = React.useState<boolean | null>(null);
  // ffmpeg: null — ещё проверяем; false — не найден (показываем плашку с
  // объяснением и кнопкой автоскачивания).
  const [ffmpegOk, setFfmpegOk] = React.useState<boolean | null>(null);
  const [ffJobId, setFfJobId] = React.useState<string | null>(null);
  const [err, setErr] = React.useState<string | null>(null);
  // путь бита → id джобы ("" = не удалось поставить в очередь).
  const [jobMap, setJobMap] = React.useState<Record<string, string>>({});
  const [started, setStarted] = React.useState(false);
  // Вкладка правой панели. Имя вкладки — это и есть ответ на вопрос «на что
  // повлияет то, что я тут правлю».

  // Тайп-артисты пачки — источник автоподбора тегов и обложек.
  const artists = React.useMemo(
    () => Array.from(new Set(beats.map((b) => b.typeName.trim()).filter(Boolean))),
    [beats],
  );
  const [tagsBusy, setTagsBusy] = React.useState(false);
  // Pinterest-пикер обложки: null = свёрнут. target — путь бита, которому
  // назначаем обложку (null = единая обложка пачки, старое поведение).
  const [pin, setPin] = React.useState<{
    q: string;
    items: PinImage[];
    loading: boolean;
    err: string | null;
    picking: string | null;
    target?: string | null;
  } | null>(null);

  // Пачка (>1 бита) → разные обложки по тайп-артисту. pools — перемешанный пул
  // на каждый тайп; assign — назначенная (пока НЕ скачанная) обложка бита;
  // focus — бит в большом превью. Скачиваем полную картинку только на загрузке.
  const isBatch = beats.length > 1;
  const [pools, setPools] = React.useState<Record<string, PinImage[]>>({});
  const [assign, setAssign] = React.useState<Record<string, PinImage>>({});
  const [focus, setFocus] = React.useState<string>(beats[0]?.path ?? "");
  const focusBeat = beats.find((b) => b.path === focus) ?? beats[0];

  async function autoTags() {
    if (!artists.length) {
      setErr(t.player.ytTagsNeedType);
      return;
    }
    setTagsBusy(true);
    setErr(null);
    try {
      const { tags: got } = await api.ytTags(artists);
      if (got.length) setTags(got.join(", "));
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setTagsBusy(false);
    }
  }

  // Теги подбираются автоматически при открытии диалога — пользователю не нужно
  // жать «Auto-pick». Один раз за сессию, как только известен тайп.
  const didAutoTag = React.useRef(false);
  React.useEffect(() => {
    if (didAutoTag.current || !artists.length) return;
    didAutoTag.current = true;
    void autoTags();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [artists]);

  async function pinSearch(q: string) {
    const query = q.trim();
    if (!query) {
      setPin((p) => (p ? { ...p, items: [], loading: false, err: t.player.ytPinNeedQuery } : p));
      return;
    }
    setPin((p) => (p ? { ...p, loading: true, err: null } : p));
    try {
      // Лимит — это «аппетит», а не обещание: одна страница Bing даёт ~37
      // картинок, поэтому бэкенд доберёт остальное вариантами запроса.
      const { items } = await api.coversSearch(query, 120);
      setPin((p) => (p ? { ...p, items, loading: false, err: items.length ? null : t.player.ytPinEmpty } : p));
    } catch (e) {
      setPin((p) => (p ? { ...p, items: [], loading: false, err: e instanceof Error ? e.message : String(e) } : p));
    }
  }

  function openPinterest() {
    const q = artists[0] ? `${artists[0]} aesthetic` : "";
    setPin({ q, items: [], loading: false, err: null, picking: null });
    if (q) void pinSearch(q);
  }

  async function pinPick(img: PinImage) {
    // Режим «на конкретный бит»: только запоминаем выбор (без скачивания).
    const target = pin?.target ?? null;
    if (target) {
      setAssign((a) => ({ ...a, [target]: img }));
      setPin(null);
      return;
    }
    setPin((p) => (p ? { ...p, picking: img.full, err: null } : p));
    try {
      const { path } = await api.coversDownload(img.full);
      setImage(path);
      setPin(null);
    } catch (e) {
      setPin((p) => (p ? { ...p, picking: null, err: e instanceof Error ? e.message : String(e) } : p));
    }
  }

  // «Авто»: случайная обложка из верхушки выдачи — даёт вариативность,
  // когда одна и та же пачка выкладывается регулярно.
  function pinAuto() {
    const pool = pin?.items.slice(0, 24) ?? [];
    if (!pool.length) return;
    void pinPick(pool[Math.floor(Math.random() * pool.length)]);
  }

  // ── Разные обложки на пачку по тайп-артисту ────────────────────────────────
  const shuffled = <T,>(a: T[]): T[] => {
    const x = [...a];
    for (let i = x.length - 1; i > 0; i--) { const j = Math.floor(Math.random() * (i + 1)); [x[i], x[j]] = [x[j], x[i]]; }
    return x;
  };

  // Тянет пулы (по одному на уникальный тайп) и раздаёт разные обложки битам.
  // force=true — перекачать выдачу; иначе перетасовать уже загруженные пулы.
  const [assignBusy, setAssignBusy] = React.useState(false);
  async function autoAssign(force = false) {
    const types = Array.from(new Set(beats.map((b) => b.typeName.trim()).filter(Boolean)));
    if (!types.length) return;
    setAssignBusy(true);
    const next: Record<string, PinImage[]> = { ...pools };
    for (const ty of types) {
      if (force || !next[ty] || next[ty].length === 0) {
        try { next[ty] = shuffled((await api.coversSearch(`${ty} aesthetic`, 40)).items); }
        catch { next[ty] = []; }
      } else {
        next[ty] = shuffled(next[ty]);
      }
    }
    const na: Record<string, PinImage> = {};
    const idx: Record<string, number> = {};
    for (const b of beats) {
      const ty = b.typeName.trim();
      const pool = ty ? next[ty] : undefined;
      if (pool && pool.length) {
        const i = idx[ty] ?? 0;
        na[b.path] = pool[i % pool.length];
        idx[ty] = i + 1;
      }
    }
    setPools(next);
    setAssign(na);
    setAssignBusy(false);
  }

  // 🎲 на бите: следующая картинка пула его тайпа (по кругу).
  function cycleBeat(path: string) {
    const b = beats.find((x) => x.path === path);
    const ty = b?.typeName.trim() ?? "";
    const pool = pools[ty] ?? [];
    if (!pool.length) return;
    const cur = assign[path];
    const curIdx = cur ? pool.findIndex((p) => p.full === cur.full) : -1;
    setAssign((a) => ({ ...a, [path]: pool[(curIdx + 1) % pool.length] }));
  }

  // Клик по обложке бита — открыть пикер именно для него.
  function pickForBeat(path: string) {
    const b = beats.find((x) => x.path === path);
    const q = b?.typeName.trim() ? `${b.typeName.trim()} aesthetic` : "";
    setPin({ q, items: [], loading: false, err: null, picking: null, target: path });
    if (q) void pinSearch(q);
  }

  // Автораздача при открытии пачки (один раз).
  React.useEffect(() => {
    if (isBatch) void autoAssign(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Авторы фокус-бита (в пачке — выбранного в списке; иначе единственного) —
  // для большого превью. Чипсы авторов по-прежнему берут первый бит.
  const repAuthors = React.useMemo(
    () => (focusBeat ? beatAuthors(focusBeat, nick, aliases, extras[focusBeat.path] ?? []) : []),
    [focusBeat, nick, aliases, extras],
  );

  // Живое превью развёрнутых {keywords}/{hashtags} для сфокусированного бита —
  // чтобы видеть стену тегов до публикации. Тип-артисты — без своего ника.
  const kwPreview = React.useMemo(() => {
    if (!focusBeat) return { keywords: "", hashtags: "" };
    const ta = parseTypeArtists(focusBeat.typeName);
    return { keywords: buildKeywords(ta, roster, new Date().getFullYear()), hashtags: buildHashtags(ta) };
  }, [focusBeat, roster]);
  // Чипсы = распознанные соавторы (без своего ника) + ручные добавления. У
  // каждого чипа source = исходный токен из имени (для записи правки в алиасы)
  // или null для добавленного вручную.
  // Чипсы авторов — выбранного бита: у каждого свои авторы, из своего имени.
  const chips = React.useMemo(() => {
    const stem = focusBeat?.name.replace(/\.[^.]+$/, "") ?? "";
    const raw = parseAuthors(stem, { bpm: focusBeat?.bpm }); // без ника и алиасов
    const out: { source: string | null; value: string }[] = [];
    const seen = new Set<string>();
    // Свой ник показан отдельной пилюлей слева — в чипсы-соавторы не дублируем.
    const nlc = nick.trim().toLowerCase();
    if (nlc) seen.add(nlc);
    for (const tok of raw) {
      const t = tok.toLowerCase();
      const aliased = Object.prototype.hasOwnProperty.call(aliases, t) ? aliases[t] : tok;
      const v = aliased.trim();
      if (!v || seen.has(v.toLowerCase())) continue;
      seen.add(v.toLowerCase());
      out.push({ source: t, value: v });
    }
    for (const e of extras[focusBeat?.path ?? ""] ?? []) {
      const v = e.trim();
      if (v && !seen.has(v.toLowerCase())) { seen.add(v.toLowerCase()); out.push({ source: null, value: v }); }
    }
    return out;
  }, [focusBeat, nick, aliases, extras]);

  function persistAliases(next: Record<string, string>) {
    setAliases(next);
    void updateSettings({ ytAuthorAliases: next });
  }

  // Правка ручного автора этого бита. Переименование распознанного идёт через
  // aliases — это словарь опечаток, общий по замыслу: «hoodrunah → hoodrunnah»
  // чинится один раз и везде.
  function setBeatExtras(path: string, fn: (xs: string[]) => string[]) {
    setExtras((prev) => {
      const next = fn(prev[path] ?? []);
      if (!next.length) {
        const { [path]: _drop, ...rest } = prev;
        return rest;
      }
      return { ...prev, [path]: next };
    });
  }

  function commitChipEdit(i: number) {
    const c = chips[i];
    const v = editVal.trim();
    setEditIdx(null);
    if (!c || v === c.value || !focusBeat) return;
    if (c.source != null) {
      persistAliases({ ...aliases, [c.source]: v }); // "" = удалить из авторов
    } else {
      setBeatExtras(focusBeat.path, (xs) => xs.flatMap((x) => (x.trim() === c.value ? (v ? [v] : []) : [x])));
    }
  }

  function removeChip(i: number) {
    const c = chips[i];
    if (!c || !focusBeat) return;
    if (c.source != null) persistAliases({ ...aliases, [c.source]: "" });
    else setBeatExtras(focusBeat.path, (xs) => xs.filter((x) => x.trim() !== c.value));
  }

  function addChip() {
    const v = addVal.trim();
    setAddVal("");
    if (!focusBeat) return;
    if (v && !chips.some((c) => c.value.toLowerCase() === v.toLowerCase())) {
      setBeatExtras(focusBeat.path, (xs) => [...xs, v]);
    }
  }

  // Значения конкретного видео: ручная правка, иначе — из шаблона пачки. Один
  // источник правды для списка, превью и самой загрузки: разойдись они, на
  // YouTube уехало бы не то, что показано в диалоге.
  const resolveBeat = React.useCallback((b: YtBeat) => {
    const e = edits[b.path] ?? {};
    const authors = beatAuthors(b, nick, aliases, extras[b.path] ?? []);
    return {
      title: e.title ?? renderYtTemplate(tpl, b, nick, authors),
      description: e.desc ?? renderYtVars(desc, b, nick, authors, roster),
      tags: e.tags ?? tags,
      privacy: e.privacy ?? privacy,
    };
  }, [edits, tpl, desc, tags, privacy, nick, aliases, extras, roster]);

  // Правка поля видео. Значение undefined снимает переопределение — поле снова
  // начинает слушаться шаблона.
  function editBeat(path: string, patch: BeatEdit) {
    setEdits((prev) => {
      const next = { ...(prev[path] ?? {}), ...patch };
      for (const k of Object.keys(next) as (keyof BeatEdit)[]) {
        if (next[k] === undefined) delete next[k];
      }
      if (!Object.keys(next).length) {
        const { [path]: _drop, ...rest } = prev;
        return rest;
      }
      return { ...prev, [path]: next };
    });
  }

  // Свои шрифты живут в настройках, поэтому переживают перезапуск. Выбранный
  // попадает в font как путь к файлу — resolveFont в Go принимает и путь, и ключ.
  const customFonts = settings?.ytCustomFonts ?? [];

  async function addFonts() {
    const picked = await pickFonts();
    if (!picked.length) return;
    await updateSettings({ ytCustomFonts: Array.from(new Set([...customFonts, ...picked])) });
    setFont(picked[picked.length - 1]);
  }

  async function removeFont(path: string) {
    await updateSettings({ ytCustomFonts: customFonts.filter((p) => p !== path) });
    if (font === path) setFont("arial");
  }

  // Локальный путь обложки фокус-бита: в пачке обложка ещё не скачана (в UI
  // висит удалённый thumb), поэтому качаем её на диск. Кэш по URL — иначе
  // дебаунс кадра дёргал бы сеть на каждую правку текста.
  const coverCache = React.useRef<Map<string, string>>(new Map());
  const localCoverPath = React.useCallback(async (beatPath: string): Promise<string> => {
    if (!isBatch) return image;
    const a = assign[beatPath];
    if (!a) return "";
    const hit = coverCache.current.get(a.full);
    if (hit) return hit;
    const { path } = await api.coversDownload(a.full);
    coverCache.current.set(a.full, path);
    return path;
  }, [isBatch, image, assign]);

  // Статичное превью — настоящий кадр из ffmpeg, тем же фильтром, что и итоговое
  // видео (см. buildChain в ffmpeg.go). Рисовать его же вручную на CSS нельзя:
  // копии геометрии неизбежно разъезжаются.
  const [frameUrl, setFrameUrl] = React.useState<string | null>(null);
  const [frameErr, setFrameErr] = React.useState<string | null>(null);

  // Кэш готовых кадров по совокупности параметров рендера. Без него каждое
  // переключение бита заново гоняло ffmpeg (~0.5 с) плюс 400 мс дебаунса —
  // возврат к уже виденному биту тормозил на ровном месте. Ключ включает всё,
  // что влияет на картинку: сам бит, его обложку, наложение и шрифт. Blob-URL
  // из кэша не отзываем при переключении — только при закрытии диалога.
  const frameCache = React.useRef<Map<string, string>>(new Map());
  React.useEffect(() => {
    const cache = frameCache.current;
    return () => { for (const url of cache.values()) URL.revokeObjectURL(url); };
  }, []);

  // Параметры кадра конкретного бита (у каждого свои авторы → своя подпись).
  const overlayFor = React.useCallback((b: YtBeat) => {
    const authors = beatAuthors(b, nick, aliases, extras[b.path] ?? []);
    return overlayLinesFor(b, nick, authors);
  }, [nick, aliases, extras]);
  const coverIdFor = React.useCallback(
    (b: YtBeat) => (isBatch ? (assign[b.path]?.full ?? "") : image),
    [isBatch, assign, image],
  );
  const frameKeyFor = React.useCallback((b: YtBeat) => {
    const { title, sub } = overlayFor(b);
    return `${b.path}|${coverIdFor(b)}|${overlay ? 1 : 0}|${overlay ? title : ""}|${overlay ? sub : ""}|${font}`;
  }, [overlayFor, coverIdFor, overlay, font]);

  // Рендерит кадр бита (или отдаёт из кэша). Возвращает Blob-URL либо null,
  // если рендер прерван/нет обложки. Используется и фокус-эффектом, и предгревом.
  const renderFrameFor = React.useCallback(async (b: YtBeat, alive: () => boolean): Promise<string | null> => {
    const key = frameKeyFor(b);
    const hit = frameCache.current.get(key);
    if (hit) return hit;
    const { title, sub } = overlayFor(b);
    const imagePath = await localCoverPath(b.path);
    if (!alive() || !imagePath) return null;
    const { path } = await api.ytPreviewFrame({
      imagePath, overlay, overlayTitle: title, overlaySub: sub, overlayFont: font,
    });
    const buf = await invoke<ArrayBuffer>("player_read_audio", { path });
    // Кэшируем всегда (пригодится при возврате), даже если фокус уже ушёл.
    const cached = frameCache.current.get(key);
    if (cached) return cached;
    const url = URL.createObjectURL(new Blob([buf], { type: "image/png" }));
    frameCache.current.set(key, url);
    return url;
  }, [frameKeyFor, overlayFor, localCoverPath, overlay, font]);

  // Кадр фокус-бита: попадание в кэш — мгновенно (без дебаунса), промах —
  // короткий дебаунс, чтобы прощёлкивание списка не запускало ffmpeg на каждый
  // промежуточный бит.
  React.useEffect(() => {
    const b = focusBeat;
    if (!b) return;
    const key = frameKeyFor(b);
    const hit = frameCache.current.get(key);
    if (hit) { setFrameUrl(hit); setFrameErr(null); return; }
    let dead = false;
    const timer = setTimeout(() => {
      void (async () => {
        try {
          const url = await renderFrameFor(b, () => !dead);
          if (dead || !url) return;
          setFrameUrl(url);
          setFrameErr(null);
        } catch (e) {
          if (!dead) setFrameErr(e instanceof Error ? e.message : String(e));
        }
      })();
    }, 180);
    return () => { dead = true; clearTimeout(timer); };
  }, [focusBeat, frameKeyFor, renderFrameFor]);

  // Фоновый предгрев: после паузы дорисовываем кадры остальных битов пачки по
  // одному, чтобы клики по списку попадали в кэш и открывались мгновенно.
  // Последовательно и прерываемо — один ffmpeg за раз, без штурма CPU.
  React.useEffect(() => {
    if (!isBatch || beats.length < 2) return;
    let dead = false;
    const timer = setTimeout(() => {
      void (async () => {
        for (const b of beats) {
          if (dead) return;
          if (frameCache.current.has(frameKeyFor(b))) continue;
          if (isBatch && !assign[b.path]) continue; // обложка ещё не выбрана
          try { await renderFrameFor(b, () => !dead); } catch { /* предгрев — тихо */ }
        }
      })();
    }, 700);
    return () => { dead = true; clearTimeout(timer); };
  }, [isBatch, beats, assign, frameKeyFor, renderFrameFor]);

  // Рендер короткого mp4 на бэкенде + проигрывание в webview (Blob).
  async function makePreview() {
    const b = focusBeat;
    if (!b) { setPreviewErr(t.player.ytNeedImage); return; }
    setPreviewBusy(true); setPreviewErr(null);
    try {
      const imagePath = await localCoverPath(b.path);
      if (!imagePath) { setPreviewErr(t.player.ytNeedImage); setPreviewBusy(false); return; }
      const lines = overlayLinesFor(b, nick, repAuthors);
      const { path } = await api.ytPreview({
        audioPath: b.path, imagePath,
        overlay, overlayTitle: lines.title, overlaySub: lines.sub, overlayFont: font,
      });
      const buf = await invoke<ArrayBuffer>("player_read_audio", { path });
      const next = URL.createObjectURL(new Blob([buf], { type: "video/mp4" }));
      setPreviewUrl((prev) => { if (prev) URL.revokeObjectURL(prev); return next; });
    } catch (e) {
      setPreviewErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPreviewBusy(false);
    }
  }

  React.useEffect(() => () => { if (previewUrl) URL.revokeObjectURL(previewUrl); }, [previewUrl]);

  // Пресеты шаблонов живут в настройках; стор обновляется оптимистично,
  // поэтому читаем прямо из него — диалог перерисуется сам. Рекомендованные
  // тайтлы всегда доступны в выпадашке (не затирая активный шаблон).
  const tplPresets = React.useMemo(() => {
    const saved = settings?.ytTitleTemplates ?? [];
    const merged = [...saved];
    for (const t of REFERENCE_TITLES) if (!merged.includes(t)) merged.push(t);
    return merged;
  }, [settings?.ytTitleTemplates]);
  const tplSaved = tplPresets.includes(tpl.trim());

  function savePreset() {
    const v = tpl.trim();
    if (!v || tplPresets.includes(v)) return;
    void updateSettings({ ytTitleTemplates: [...tplPresets, v] });
  }

  function deletePreset() {
    const v = tpl.trim();
    if (!tplPresets.includes(v)) return;
    void updateSettings({ ytTitleTemplates: tplPresets.filter((p) => p !== v) });
  }

  // Пресеты описаний — та же механика, что у шаблонов названия.
  // Референс-шаблон всегда доступен в выпадашке (не затирая активное описание).
  const descPresets = React.useMemo(() => {
    const saved = settings?.ytDescTemplates ?? [];
    return saved.includes(REFERENCE_DESC) ? saved : [...saved, REFERENCE_DESC];
  }, [settings?.ytDescTemplates]);
  const descSaved = descPresets.includes(desc.trim());

  function saveDescPreset() {
    const v = desc.trim();
    if (!v || descPresets.includes(v)) return;
    void updateSettings({ ytDescTemplates: [...descPresets, v] });
  }

  function deleteDescPreset() {
    const v = desc.trim();
    if (!descPresets.includes(v)) return;
    void updateSettings({ ytDescTemplates: descPresets.filter((p) => p !== v) });
  }

  React.useEffect(() => {
    api.ytStatus().then((st) => setYtOk(st.connected)).catch(() => setYtOk(false));
    api.ytFfmpeg().then((f) => setFfmpegOk(f.found)).catch(() => setFfmpegOk(null));
  }, []);

  // Прогресс автоскачивания ffmpeg; по завершении перепроверяем статус.
  const ffJob = ffJobId ? jobs[ffJobId] : null;
  React.useEffect(() => {
    if (!ffJob) return;
    if (ffJob.status === "completed" || ffJob.status === "failed" || ffJob.status === "canceled") {
      if (ffJob.status === "completed") setFfJobId(null);
      api.ytFfmpeg().then((f) => setFfmpegOk(f.found)).catch(() => {});
    }
  }, [ffJob?.status]); // eslint-disable-line react-hooks/exhaustive-deps

  async function ffmpegDownload() {
    try {
      const { jobId } = await api.ytFfmpegDownload();
      setFfJobId(jobId);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }

  // Esc закрывает диалог, не задевая горячие клавиши страницы (capture-фаза).
  React.useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopImmediatePropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", h, true);
    return () => window.removeEventListener("keydown", h, true);
  }, [onClose]);

  async function pickImage() {
    try {
      const { open } = await import("@tauri-apps/plugin-dialog");
      const res = await open({
        multiple: false,
        directory: false,
        filters: [{ name: "Images", extensions: ["png", "jpg", "jpeg", "webp", "bmp"] }],
      });
      if (res) setImage(Array.isArray(res) ? res[0] : res);
    } catch { /* диалог закрыт */ }
  }

  async function start() {
    if (!ytOk) { setErr(t.player.ytNeedSetup); return; }
    if (ffmpegOk === false) { setErr(t.settings.ytFfmpegMissing); return; }
    const anyCover = isBatch ? beats.some((b) => assign[b.path]) : !!image;
    if (!anyCover) { setErr(t.player.ytNeedImage); return; }
    setErr(null);
    setStarted(true);
    // Значения формы становятся дефолтами следующей загрузки. В пачке единую
    // обложку не трогаем (у каждого бита своя) — сохраняем прежний дефолт.
    void updateSettings({ ytDefaultImage: image || (settings?.ytDefaultImage ?? ""), ytNickname: nick, ytNoTextOverlay: !overlay, ytFont: font, ytAuthorAliases: aliases, ytTitleTemplate: tpl, ytDescription: desc, ytTags: tags, ytPrivacy: privacy, ytKeywordRoster: roster, ytRosterAutoGrow: rosterAutoGrow });
    const map: Record<string, string> = {};
    for (const b of beats) {
      try {
        // Обложка бита: в пачке — своя (скачиваем полную сейчас); иначе единая.
        let imagePath = image;
        if (isBatch) {
          const a = assign[b.path];
          if (!a) { map[b.path] = ""; setJobMap({ ...map }); continue; }
          imagePath = (await api.coversDownload(a.full)).path;
        }
        if (!imagePath) { map[b.path] = ""; setJobMap({ ...map }); continue; }
        const authors = beatAuthors(b, nick, aliases, extras[b.path] ?? []);
        const lines = overlayLinesFor(b, nick, authors);
        const v = resolveBeat(b);
        const { jobId } = await api.ytUpload({
          audioPath: b.path,
          imagePath,
          title: v.title,
          description: v.description,
          tags: v.tags.split(",").map((s) => s.trim()).filter(Boolean),
          privacy: v.privacy,
          overlay,
          overlayTitle: lines.title,
          overlaySub: lines.sub,
          overlayFont: font,
        });
        map[b.path] = jobId;
      } catch {
        map[b.path] = "";
      }
      setJobMap({ ...map });
    }
    // Авторост ростера: артисты успешно поставленных в очередь битов пополняют
    // пул для будущих видео. Артисты текущей пачки и так во фронт-слайсе.
    if (rosterAutoGrow) {
      let grown = roster;
      for (const b of beats) {
        if (!map[b.path]) continue; // не ушёл в загрузку
        grown = mergeRoster(grown, parseTypeArtists(b.typeName));
      }
      if (grown !== roster) {
        setRoster(grown);
        void updateSettings({ ytKeywordRoster: grown });
      }
    }
  }

  function openExternal(url: string) {
    void invoke("plugin:shell|open", { path: url }).catch(() => {});
  }

  function statusFor(path: string): { text: string; url?: string; failed?: boolean } {
    const id = jobMap[path];
    if (id === "") return { text: t.player.ytStatusFailed, failed: true };
    const job = id ? jobs[id] : null;
    if (!job) return { text: started ? t.player.ytStatusQueued : "" };
    switch (job.status) {
      case "queued": return { text: t.player.ytStatusQueued };
      case "running": {
        const pct = ` ${Math.round((job.progress || 0) * 100)}%`;
        return { text: (job.stage === "upload" ? t.player.ytStatusUpload : t.player.ytStatusRender) + pct };
      }
      case "completed": return { text: t.player.ytStatusDone, url: (job.result?.url as string) || undefined };
      default: return { text: `${t.player.ytStatusFailed}${job.error ? `: ${job.error}` : ""}`, failed: true };
    }
  }

  const inputStyle: React.CSSProperties = {
    width: "100%",
    height: 32,
    padding: "0 10px",
    background: isFl ? "var(--groove)" : "var(--surface-input, var(--surface-3))",
    border: `1px solid ${isFl ? "var(--line-work)" : "var(--border)"}`,
    borderRadius: 0,
    boxShadow: isFl ? "inset 0 2px 4px rgba(0,0,0,.35)" : undefined,
    color: isFl ? "var(--ink-on-work)" : "var(--text-body)",
    fontFamily: "var(--font-sans)",
    fontSize: 12.5,
    outline: "none",
  };
  const labelStyle: React.CSSProperties = {
    fontSize: isFl ? 10 : "var(--fs-label, 10px)",
    fontWeight: 700,
    letterSpacing: "1px",
    textTransform: "uppercase",
    color: isFl ? "var(--ink-dim)" : "var(--text-faint)",
  };
  const chromeBtn: React.CSSProperties = isFl ? {
    display: "inline-flex", alignItems: "center", gap: 7,
    height: 32, padding: "0 12px",
    background: "linear-gradient(var(--btn-hi),var(--btn))",
    border: "1px solid var(--chrome-lo)", borderRadius: 0,
    color: "var(--ink)", font: "600 12px var(--font-sans)",
    cursor: "pointer",
    boxShadow: "inset 0 1px 0 rgba(255,255,255,.5),0 1px 2px rgba(0,0,0,.25)",
  } : {
    display: "inline-flex", alignItems: "center", gap: 7,
    height: 32, padding: "0 12px",
    background: "transparent",
    border: "1px solid var(--border)", borderRadius: 0,
    color: "var(--text-body)", fontSize: 12.5, fontWeight: 600,
    cursor: "pointer",
  };
  const chipStyle: React.CSSProperties = {
    display: "inline-flex", alignItems: "center", gap: 5,
    height: 24, padding: "0 4px 0 9px", borderRadius: 0,
    background: isFl ? "var(--accent-soft, rgba(255,138,60,.16))" : "var(--surface-3)",
    border: `1px solid ${isFl ? "var(--line-work)" : "var(--border)"}`,
    color: isFl ? "var(--ink-on-work)" : "var(--text-body)",
    fontSize: 12, whiteSpace: "nowrap",
  };
  const chipX: React.CSSProperties = {
    display: "inline-flex", alignItems: "center", justifyContent: "center",
    width: 16, height: 16, borderRadius: "50%", border: "none",
    background: "transparent", color: "inherit", cursor: "pointer",
    fontSize: 14, lineHeight: 1, opacity: 0.65, padding: 0,
  };

  // Общие для рабочей области: цвет разделителей и «шапка колонки» (в пачке
  // над каждой из трёх колонок — список / это видео / общее).
  const line = isFl ? "var(--line-work)" : "var(--border)";
  const colHead: React.CSSProperties = {
    display: "flex", alignItems: "center", gap: 6,
    padding: "9px 16px", flexShrink: 0,
    borderBottom: `1px solid ${line}`,
    fontSize: 10, fontWeight: 700, letterSpacing: "1px",
    textTransform: "uppercase",
    color: isFl ? "var(--ink-dim)" : "var(--text-faint)",
  };

  const allDone = started && beats.every((b) => {
    const id = jobMap[b.path];
    if (id === "") return true;
    const j = id ? jobs[id] : null;
    return !!j && (j.status === "completed" || j.status === "failed" || j.status === "canceled");
  });

  return (
    <div
      // Закрытие по mousedown на самом бэкдропе, а не по click: событие click
      // всплывает к общему предку точек нажатия и отпускания, поэтому
      // выделение текста в поле шаблона с отпусканием мыши за панелью
      // раньше закрывало диалог.
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}
      style={{ position: "fixed", inset: 0, zIndex: 60, background: "rgba(0,0,0,.55)", display: "flex", alignItems: "center", justifyContent: "center" }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          // Пачка живёт в двух панелях (список слева, детали справа), одиночное
          // видео — в одной колонке: там «общее» и «это видео» совпадают.
          width: isBatch ? 1240 : 580, maxWidth: "94vw", height: isBatch ? "88vh" : undefined, maxHeight: "88vh",
          background: isFl ? "linear-gradient(var(--panel-hi), var(--panel))" : "var(--surface-2)",
          border: `1px solid ${isFl ? "var(--panel-lo)" : "var(--border)"}`,
          borderRadius: 0,
          display: "flex", flexDirection: "column",
          overflow: "hidden",
          boxShadow: "0 12px 40px rgba(0,0,0,.5)",
        }}
      >
        {/* Заголовок */}
        <div style={{ display: "flex", alignItems: "center", gap: 10, padding: "14px 18px", borderBottom: `1px solid ${isFl ? "var(--line-work)" : "var(--border)"}`, flexShrink: 0 }}>
          <Icons.Yt width={16} height={16} style={{ color: "var(--accent)" }} />
          <span style={{ flex: 1, font: isFl ? "700 14px var(--font-sans)" : undefined, fontSize: isFl ? undefined : 15, fontWeight: isFl ? undefined : 700, color: isFl ? "var(--ink)" : "var(--text-strong, var(--text-body))" }}>
            {t.player.ytDialogTitle}
          </span>
          <span style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: isFl ? "var(--ink-dim)" : "var(--text-faint)" }}>
            {t.player.ytBeatsCount}: {beats.length}
          </span>
          <button onClick={onClose} style={{ background: "none", border: "none", cursor: "pointer", color: isFl ? "var(--ink-dim)" : "var(--text-faint)", padding: 4, display: "flex" }}>
            <Icons.X width={14} height={14} />
          </button>
        </div>

        {/* Предупреждения над рабочей областью */}
        <div style={{ display: "flex", flexDirection: "column", gap: 8, padding: (ytOk === false || ffmpegOk === false) ? "12px 18px 0 18px" : 0, flexShrink: 0 }}>
        {/* Подключение не настроено */}
        {ytOk === false && (
          <div style={{ padding: "8px 12px", borderRadius: 0, background: "transparent", border: "1px solid var(--danger)", color: "var(--danger)", fontSize: 12.5 }}>
            {t.player.ytNeedSetup}
          </div>
        )}

        {/* ffmpeg не найден: объяснение + автоскачивание с прогрессом */}
        {ffmpegOk === false && (
          <div style={{
            display: "flex", flexDirection: "column", gap: 8,
            padding: "10px 12px", borderRadius: 0, fontSize: 12.5,
            background: "rgba(255,181,91,.10)", border: "1px solid rgba(255,181,91,.45)",
            color: isFl ? "var(--ink)" : "var(--text-body)",
          }}>
            <span style={{ fontWeight: 700 }}>{t.settings.ytFfmpegMissing}</span>
            <span style={{ lineHeight: 1.5, color: isFl ? "var(--ink-dim)" : "var(--text-muted)" }}>
              {t.settings.ytFfmpegWhy}
            </span>
            {ffJob && (ffJob.status === "running" || ffJob.status === "queued") ? (
              <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <div style={{ flex: 1, height: 6, borderRadius: 0, background: isFl ? "var(--groove)" : "var(--surface-3)", overflow: "hidden" }}>
                  <div style={{ width: `${Math.round((ffJob.progress || 0) * 100)}%`, height: "100%", background: "var(--accent)", transition: "width 300ms" }} />
                </div>
                <span style={{ flexShrink: 0, fontFamily: "var(--font-mono)", fontSize: 11 }}>
                  {ffJob.stage === "extract" ? t.settings.ytFfmpegExtracting : `${Math.round((ffJob.progress || 0) * 100)}%`}
                </span>
              </div>
            ) : (
              <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <button onClick={() => void ffmpegDownload()} style={{ ...chromeBtn, flexShrink: 0 }}>
                  <Icons.Download width={13} height={13} />
                  {t.settings.ytFfmpegDownload}
                </button>
                {ffJob?.status === "failed" && (
                  <span style={{ color: "var(--rec, #ff453a)", fontSize: 12 }}>
                    {t.settings.ytFfmpegFailed}{ffJob.error ? `: ${ffJob.error}` : ""}
                  </span>
                )}
              </div>
            )}
          </div>
        )}

        </div>

        {/* Рабочая область: слева все видео, затем «это видео» и «общее».
            В пачке — три колонки в ряд; одиночное видео — одна колонка со
            своим скроллом (списка слева нет). */}
        <div style={{ flex: 1, display: "flex", flexDirection: isBatch ? "row" : "column", alignItems: "stretch", minHeight: 0, overflowY: isBatch ? undefined : "auto" }}>

          {isBatch && (
            <VideoList
              beats={beats} focus={focus} setFocus={setFocus}
              assign={assign} edits={edits} extras={extras}
              nick={nick} aliases={aliases}
              resolveBeat={resolveBeat} statusFor={statusFor}
              onPick={pickForBeat} onCycle={cycleBeat}
              onReshuffle={() => void autoAssign(false)} busy={assignBusy}
              isFl={isFl}
            />
          )}

          {/* Рабочая область в пачке — три независимо скроллящиеся колонки:
              список видео | это видео | общее. Раньше «это видео» и «общее»
              делили одну узкую колонку через вкладки: половина всегда была
              скрыта, а широкий диалог простаивал. Одиночное видео — одна
              колонка (списка слева у него нет). */}
          {/* Колонка «это видео» */}
          <div style={{
            display: "flex", flexDirection: "column", minWidth: 0, minHeight: 0,
            ...(isBatch ? { flex: "0 0 440px", overflowY: "auto", borderRight: `1px solid ${line}` } : {}),
          }}>
            {isBatch && <div style={colHead}>{t.player.ytTabVideo}</div>}
            <div style={{ padding: "14px 16px 16px", display: "flex", flexDirection: "column", gap: 12 }}>


        {/* Обложка: одна (один бит) или разные по тайп-артисту (пачка) */}
        {!isBatch && (
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            <span style={labelStyle}>{t.player.ytCover}</span>
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              <div style={{ ...inputStyle, display: "flex", alignItems: "center", overflow: "hidden", whiteSpace: "nowrap", textOverflow: "ellipsis", fontFamily: "var(--font-mono)", fontSize: 11, color: image ? undefined : (isFl ? "var(--ink-dim)" : "var(--text-faint)") }}>
                {image || t.player.ytNoImage}
              </div>
              <button onClick={pickImage} style={{ ...chromeBtn, flexShrink: 0 }}>
                <Icons.Folder width={12} height={12} />
                {t.player.ytPickImage}
              </button>
              <button onClick={openPinterest} title={t.player.ytPinterestTitle} style={{ ...chromeBtn, flexShrink: 0 }}>
                <Icons.Search width={12} height={12} />
                {t.player.ytPinterest}
              </button>
            </div>
          </div>
        )}

        {/* Pinterest-пикер обложки */}
        {pin && (
          <div style={{
            display: "flex", flexDirection: "column", gap: 8, padding: 10, borderRadius: 0,
            border: `1px solid ${isFl ? "var(--line-work)" : "var(--border)"}`,
            background: isFl ? "var(--work-2)" : "transparent",
          }}>
            <div style={{ display: "flex", gap: 8 }}>
              <input
                value={pin.q}
                onChange={(e) => setPin({ ...pin, q: e.target.value })}
                onKeyDown={(e) => { if (e.key === "Enter") void pinSearch(pin.q); }}
                placeholder={t.player.ytPinQuery}
                style={{ ...inputStyle, flex: 1, minWidth: 0 }}
                spellCheck={false}
              />
              <button onClick={() => void pinSearch(pin.q)} style={{ ...chromeBtn, flexShrink: 0 }}>
                {t.player.ytPinSearch}
              </button>
              <button
                onClick={pinAuto}
                disabled={!pin.items.length || !!pin.picking}
                title={t.player.ytPinAutoTitle}
                style={{ ...chromeBtn, flexShrink: 0, opacity: pin.items.length && !pin.picking ? 1 : 0.45 }}
              >
                🎲 {t.player.ytPinAuto}
              </button>
              <button onClick={() => setPin(null)} style={{ ...chromeBtn, width: 32, padding: 0, justifyContent: "center", flexShrink: 0 }}>
                <Icons.X width={12} height={12} />
              </button>
            </div>
            {pin.loading ? (
              <span style={{ fontSize: 12, color: isFl ? "var(--ink-dim)" : "var(--text-faint)" }}>{t.player.ytPinLoading}</span>
            ) : pin.err ? (
              <span style={{ fontSize: 12, color: "var(--rec, #ff453a)" }}>{pin.err}</span>
            ) : pin.items.length > 0 && (
              <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(92px, 1fr))", gap: 6, maxHeight: 460, overflowY: "auto" }}>
                {pin.items.map((img) => (
                  <img
                    key={img.full}
                    src={img.thumb}
                    loading="lazy"
                    onClick={() => { if (!pin.picking) void pinPick(img); }}
                    style={{
                      width: "100%", aspectRatio: "1 / 1", objectFit: "cover", display: "block",
                      borderRadius: 0, cursor: pin.picking ? "progress" : "pointer",
                      opacity: pin.picking && pin.picking !== img.full ? 0.4 : 1,
                      outline: pin.picking === img.full ? "2px solid var(--accent)" : "none",
                    }}
                  />
                ))}
              </div>
            )}
          </div>
        )}

        {/* Кадр видео и обложка этого бита. В пачке показываем всегда: выбирать
            обложку надо именно отсюда, а пряталось оно вместе с кадром. */}
        {/* Кадр и поля этого видео — рядом. Раньше превью было вверху, а список
            и правки внизу: отсюда и брались «глаза бегают туда-сюда». */}
        <div style={{ display: "flex", flexDirection: "column", gap: 14, alignItems: "stretch" }}>

        {(isBatch || image) && (
          <div style={{ display: "flex", flexDirection: "column", gap: 8, width: "100%", flexShrink: 0 }}>
            <span style={labelStyle}>{t.player.ytPreviewCover}</span>
            <div style={{ position: "relative", width: "100%", aspectRatio: "16 / 9", borderRadius: 0, overflow: "hidden", background: "#000", border: `1px solid ${isFl ? "var(--line-work)" : "var(--border)"}` }}>
              {/* Кадр из ffmpeg — тот же фильтр, что и у итогового видео. */}
              {frameUrl ? (
                <img src={frameUrl} alt="" style={{ position: "absolute", inset: 0, width: "100%", height: "100%", objectFit: "contain" }} />
              ) : (
                // Кадр рендерится не мгновенно (скачать обложку + ffmpeg): без
                // этой надписи чёрный прямоугольник читается как поломка.
                <span style={{ position: "absolute", inset: 0, display: "flex", alignItems: "center", justifyContent: "center", fontSize: 12, color: "var(--text-faint, #777)" }}>
                  {frameErr ? "" : (isBatch && !assign[focus]) ? t.player.ytNoImage : t.player.ytFrameRendering}
                </span>
              )}
            </div>
            <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
              {isBatch && (
                <>
                  <button onClick={() => pickForBeat(focus)} style={{ ...chromeBtn, flexShrink: 0 }}>
                    <Icons.Search width={12} height={12} />
                    {t.player.ytCoverPick}
                  </button>
                  <button onClick={() => cycleBeat(focus)} title={t.player.ytCoverCycle} style={{ ...chromeBtn, width: 32, padding: 0, justifyContent: "center", flexShrink: 0 }}>
                    🎲
                  </button>
                </>
              )}
              <button onClick={() => void makePreview()} disabled={previewBusy} style={{ ...chromeBtn, flexShrink: 0, opacity: previewBusy ? 0.55 : 1 }}>
                <Icons.Play width={12} height={12} />
                {previewBusy ? t.player.ytPreviewRendering : t.player.ytPreviewVideo}
              </button>
              {(previewErr ?? frameErr) && (
                <span style={{ fontSize: 12, color: "var(--rec, #ff453a)" }}>{previewErr ?? frameErr}</span>
              )}
            </div>
            {previewUrl && (
              <video src={previewUrl} controls autoPlay style={{ width: "100%", maxHeight: 280, borderRadius: 0, background: "#000", display: "block" }} />
            )}
          </div>
        )}

        {/* Колонка полей этого видео */}
        <div style={{ flex: 1, minWidth: 0, width: isBatch ? undefined : "100%", display: "flex", flexDirection: "column", gap: 12 }}>

        {/* Авторы бита: авто-распознавание из имени + правки (память псевдонимов).
            Свойство видео: у каждого бита свои авторы, из своего имени файла. */}
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <span style={labelStyle}>{t.player.ytAuthors}</span>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 6, alignItems: "center", padding: 6, borderRadius: 0, border: `1px solid ${isFl ? "var(--line-work)" : "var(--border)"}`, background: isFl ? "var(--work-3)" : "transparent" }}>
            {nick.trim() && (
              <span style={{ ...chipStyle, opacity: 0.8, paddingRight: 9 }} title={t.player.ytAuthorsNickTip}>{nick.trim()}</span>
            )}
            {chips.map((c, i) => (
              editIdx === i ? (
                <input
                  key={`edit-${i}`}
                  autoFocus
                  value={editVal}
                  onChange={(e) => setEditVal(e.target.value)}
                  onBlur={() => commitChipEdit(i)}
                  onKeyDown={(e) => { if (e.key === "Enter") commitChipEdit(i); else if (e.key === "Escape") setEditIdx(null); }}
                  style={{ ...inputStyle, width: 120, height: 24, padding: "0 8px", fontSize: 12 }}
                  spellCheck={false}
                />
              ) : (
                <span key={`${c.value}-${i}`} style={chipStyle}>
                  <span onClick={() => { setEditIdx(i); setEditVal(c.value); }} style={{ cursor: "text" }} title={t.player.ytAuthorsEditTip}>{c.value}</span>
                  <button onClick={() => removeChip(i)} title={t.player.ytAuthorsRemove} style={chipX}>×</button>
                </span>
              )
            ))}
            <input
              value={addVal}
              onChange={(e) => setAddVal(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") addChip(); }}
              onBlur={addChip}
              placeholder={t.player.ytAuthorsAdd}
              style={{ ...inputStyle, width: 110, height: 24, padding: "0 8px", fontSize: 12, flex: "0 0 auto" }}
              spellCheck={false}
            />
          </div>
          <span style={{ fontSize: 11, color: isFl ? "var(--ink-dim)" : "var(--text-faint)" }}>{t.player.ytAuthorsHint}</span>
        </div>

        {/* Поля этого видео: свои значения либо унаследованные из шаблона */}
        {focusBeat && (
          <BeatEditor
            b={focusBeat}
            v={resolveBeat(focusBeat)}
            e={edits[focusBeat.path] ?? {}}
            onEdit={editBeat}
            isFl={isFl}
          />
        )}

        </div>
        </div>

            </div>
          </div>

          {/* Колонка «общее для всех» */}
          <div style={{
            display: "flex", flexDirection: "column", minWidth: 0, minHeight: 0,
            ...(isBatch ? { flex: 1, minWidth: 380, overflowY: "auto" } : {}),
          }}>
            {isBatch && <div style={colHead}>{t.player.ytTabShared}</div>}
            <div style={{ padding: "14px 16px 16px", display: "flex", flexDirection: "column", gap: 12 }}>

        {/* ── Общее для всех ────────────────────────────────────────────── */}

        {/* Ник продюсера: подставляется как {nick} и вычищается из {name} */}
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <span style={labelStyle}>{t.player.ytNickname}</span>
          <input
            value={nick}
            onChange={(e) => setNick(e.target.value)}
            placeholder={t.player.ytNicknamePlaceholder}
            style={inputStyle}
            spellCheck={false}
          />
          <span style={{ fontSize: 11, color: isFl ? "var(--ink-dim)" : "var(--text-faint)" }}>{t.player.ytNicknameHint}</span>
        </div>

        {/* Текст на кадре и шрифт: общие — вшиваются во все видео пачки */}
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <span style={labelStyle}>{t.player.ytOverlaySection}</span>
          <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12, cursor: "pointer", color: isFl ? "var(--ink-dim)" : "var(--text-muted)" }}>
            <input type="checkbox" checked={overlay} onChange={(e) => setOverlay(e.target.checked)} />
            {t.player.ytOverlayToggle}
          </label>
          {overlay && (
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <span style={{ ...labelStyle, flexShrink: 0 }}>{t.player.ytFont}</span>
              <select
                value={font}
                onChange={(e) => setFont(e.target.value)}
                style={{ ...inputStyle, cursor: "pointer", fontFamily: cssFontFor(font) }}
              >
                {YT_FONTS.map((f) => (
                  <option key={f.key} value={f.key} style={{ fontFamily: f.css }}>{f.label}</option>
                ))}
                {/* Свои шрифты: гарнитуру в списке не показать (файл не загружен
                    в вебвью), но её сразу видно на кадре превью. */}
                {customFonts.map((p) => (
                  <option key={p} value={p}>{fontLabel(p)}</option>
                ))}
              </select>
              <button onClick={() => void addFonts()} style={{ ...chromeBtn, flexShrink: 0 }} title={t.player.ytFontAdd}>
                <Icons.Plus width={12} height={12} />
                {t.player.ytFontAdd}
              </button>
              {isCustomFont(font) && (
                <button onClick={() => void removeFont(font)} style={{ ...chromeBtn, flexShrink: 0 }} title={t.player.ytFontRemove}>
                  <Icons.X width={12} height={12} />
                </button>
              )}
            </div>
          )}
        </div>

        {/* Шаблон названия: пресеты + редактируемое поле */}
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <span style={labelStyle}>{t.player.ytTitleTemplate}</span>
          <select
            value={tplSaved ? tpl.trim() : ""}
            onChange={(e) => { if (e.target.value) setTpl(e.target.value); }}
            style={{ ...inputStyle, cursor: "pointer" }}
          >
            {!tplSaved && <option value="">{t.player.ytTplCustom}</option>}
            {tplPresets.map((p) => (
              <option key={p} value={p}>{p}</option>
            ))}
          </select>
          <div style={{ display: "flex", gap: 8 }}>
            <input value={tpl} onChange={(e) => setTpl(e.target.value)} style={{ ...inputStyle, flex: 1, minWidth: 0 }} spellCheck={false} />
            <button
              title={t.player.ytTplSave}
              onClick={savePreset}
              disabled={tplSaved || !tpl.trim()}
              style={{ ...chromeBtn, width: 32, padding: 0, justifyContent: "center", flexShrink: 0, opacity: tplSaved || !tpl.trim() ? 0.4 : 1, cursor: tplSaved || !tpl.trim() ? "default" : "pointer" }}
            >
              <Icons.Save width={13} height={13} />
            </button>
            <button
              title={t.player.ytTplDelete}
              onClick={deletePreset}
              disabled={!tplSaved}
              style={{ ...chromeBtn, width: 32, padding: 0, justifyContent: "center", flexShrink: 0, opacity: tplSaved ? 1 : 0.4, cursor: tplSaved ? "pointer" : "default" }}
            >
              <Icons.Trash width={13} height={13} />
            </button>
          </div>
          <span style={{ fontSize: 11, color: isFl ? "var(--ink-dim)" : "var(--text-faint)" }}>{t.player.ytTemplateHint}</span>
        </div>

        {/* Описание: пресеты + редактируемое поле, те же подстановки, что в названии */}
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <span style={labelStyle}>{t.player.ytDescription}</span>
          {descPresets.length > 0 && (
            <select
              value={descSaved ? desc.trim() : ""}
              onChange={(e) => { if (e.target.value) setDesc(e.target.value); }}
              style={{ ...inputStyle, cursor: "pointer" }}
            >
              {!descSaved && <option value="">{t.player.ytTplCustom}</option>}
              {descPresets.map((p) => (
                <option key={p} value={p}>
                  {(p.split("\n")[0].slice(0, 70) || "…") + (p.includes("{keywords}") ? " — SEO" : "")}
                </option>
              ))}
            </select>
          )}
          <div style={{ display: "flex", gap: 8, alignItems: "stretch" }}>
            <textarea
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              placeholder={t.player.ytDescPlaceholder}
              style={{ ...inputStyle, flex: 1, minWidth: 0, height: "auto", padding: "8px 10px", resize: "vertical", minHeight: 240, fontFamily: "var(--font-sans)", lineHeight: 1.45 }}
            />
            <div style={{ display: "flex", flexDirection: "column", gap: 6, flexShrink: 0 }}>
              <button
                title={t.player.ytTplSave}
                onClick={saveDescPreset}
                disabled={descSaved || !desc.trim()}
                style={{ ...chromeBtn, width: 32, padding: 0, justifyContent: "center", opacity: descSaved || !desc.trim() ? 0.4 : 1, cursor: descSaved || !desc.trim() ? "default" : "pointer" }}
              >
                <Icons.Save width={13} height={13} />
              </button>
              <button
                title={t.player.ytTplDelete}
                onClick={deleteDescPreset}
                disabled={!descSaved}
                style={{ ...chromeBtn, width: 32, padding: 0, justifyContent: "center", opacity: descSaved ? 1 : 0.4, cursor: descSaved ? "pointer" : "default" }}
              >
                <Icons.Trash width={13} height={13} />
              </button>
            </div>
          </div>
          <span style={{ fontSize: 11, color: isFl ? "var(--ink-dim)" : "var(--text-faint)" }}>{t.player.ytTemplateHint}</span>
        </div>
        <div style={{ display: "flex", gap: 10 }}>
          <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 6 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <span style={labelStyle}>{t.player.ytTags}</span>
              <button
                onClick={() => void autoTags()}
                disabled={tagsBusy}
                title={t.player.ytTagsAutoTitle}
                style={{ ...chromeBtn, height: 20, padding: "0 8px", fontSize: 10.5, gap: 4, opacity: tagsBusy ? 0.55 : 1 }}
              >
                ✨ {tagsBusy ? "…" : t.player.ytTagsAuto}
              </button>
            </div>
            <input value={tags} onChange={(e) => setTags(e.target.value)} style={inputStyle} spellCheck={false} />
          </div>
          <div style={{ width: 150, display: "flex", flexDirection: "column", gap: 6 }}>
            <span style={labelStyle}>{t.player.ytPrivacy}</span>
            <select value={privacy} onChange={(e) => setPrivacy(e.target.value)} style={{ ...inputStyle, cursor: "pointer" }}>
              <option value="public">{t.player.ytPrivacyPublic}</option>
              <option value="unlisted">{t.player.ytPrivacyUnlisted}</option>
              <option value="private">{t.player.ytPrivacyPrivate}</option>
            </select>
          </div>
        </div>

        <div style={{ display: "flex", flexDirection: "column", gap: 6, marginTop: 12 }}>
          <span style={labelStyle}>{t.player.ytKeywordsSection}</span>
          <label style={{ fontSize: 12, color: "var(--text-faint)" }}>{t.player.ytRosterLabel}</label>
          <textarea
            value={roster}
            onChange={(e) => setRoster(e.target.value)}
            placeholder={t.player.ytRosterHint}
            spellCheck={false}
            style={{ ...inputStyle, minHeight: 90, resize: "vertical", fontFamily: "var(--font-mono)", fontSize: 12 }}
          />
          <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12, cursor: "pointer" }}>
            <input type="checkbox" checked={rosterAutoGrow} onChange={(e) => setRosterAutoGrow(e.target.checked)} />
            {t.player.ytRosterAutoGrow}
          </label>
          <label style={{ fontSize: 12, color: "var(--text-faint)", marginTop: 4 }}>{t.player.ytKeywordsPreview}</label>
          <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--text-faint)", maxHeight: 90, overflowY: "auto", whiteSpace: "pre-wrap", wordBreak: "break-word", border: "1px solid var(--stroke, #333)", borderRadius: 0, padding: 6 }}>
            {kwPreview.keywords || "—"}
          </div>
          <label style={{ fontSize: 12, color: "var(--text-faint)", marginTop: 4 }}>{t.player.ytHashtagsPreview}</label>
          <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--accent, #4ea1ff)", wordBreak: "break-word" }}>
            {kwPreview.hashtags || "—"}
          </div>
        </div>

        {/* Одиночное видео: статус загрузки — списка слева у него нет */}
        {!isBatch && focusBeat && statusFor(focusBeat.path).text && (
          <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12 }}>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: statusFor(focusBeat.path).failed ? "var(--rec, #ff453a)" : (isFl ? "var(--ink-dim)" : "var(--text-faint)") }}>
              {statusFor(focusBeat.path).text}
            </span>
            {statusFor(focusBeat.path).url && (
              <button onClick={() => openExternal(statusFor(focusBeat.path).url!)} style={{ ...chromeBtn, height: 24, padding: "0 8px", fontSize: 11 }}>
                {t.player.ytOpen}
              </button>
            )}
          </div>
        )}

              </div>
            </div>
          </div>

        {/* Подвал: статус окружения слева, действия справа */}
        <div style={{ display: "flex", alignItems: "center", gap: 10, padding: "10px 16px", borderTop: `1px solid ${isFl ? "var(--line-work)" : "var(--border)"}`, flexShrink: 0 }}>
          <span style={{ fontSize: 11, color: "var(--rec, #ff453a)", overflow: "hidden", whiteSpace: "nowrap", textOverflow: "ellipsis" }}>
            {/* Только ошибки: молчим, когда всё в порядке. Никаких «ffmpeg найден». */}
            {err
              ? err
              : [!ytOk ? t.player.ytFootChannelOff : "", ffmpegOk === false ? t.player.ytFootFfmpegOff : ""].filter(Boolean).join(" · ")}
          </span>
          <div style={{ marginLeft: "auto", display: "flex", gap: 10, flexShrink: 0 }}>
            <button onClick={onClose} style={chromeBtn}>{t.player.ytClose}</button>
            {!allDone && (
              <button
                onClick={start}
                disabled={started}
                style={{
                  ...chromeBtn,
                  opacity: started ? 0.55 : 1,
                  background: isFl ? "linear-gradient(var(--accent), var(--accent-deep, #e8651e))" : "var(--accent)",
                  border: isFl ? "1px solid var(--chrome-lo)" : "1px solid var(--accent)",
                  color: isFl ? "#fff" : "var(--text-on-accent)",
                }}
              >
                <Icons.Yt width={13} height={13} />
                {t.player.ytStart}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// ── PlayerPage ────────────────────────────────────────────────────────────────

export function PlayerPage() {
  const t = useT();
  // Terminal-Core: единая тема. FL-скин (хром-панели, LCD) снят — рендерится
  // только чистая, токен-ориентированная ветка. _theme больше не влияет на скин.
  useSettingsStore((s) => s.settings?.theme);
  const isFl = false;

  // Плейлист восстанавливается из localStorage — переживает перезапуск и
  // переключение вкладок (страница размонтируется при уходе с таба).
  // При восстановлении сверяется с наблюдаемыми папками: файлы папок, уже
  // снятых с наблюдения, отбрасываются.
  const [entries, setEntries] = React.useState<FileEntry[]>(() =>
    reconcilePlaylist(loadPlaylist(), loadWatched()),
  );
  // Активный трек храним по пути, а не по индексу: индексы плывут при
  // удалении строк и смене сортировки.
  const [activePath, setActivePath] = React.useState<string | null>(null);
  const [q, setQ] = React.useState("");
  const [sortCol, setSortCol] = React.useState<SortCol>("created");
  const [sortAsc, setSortAsc] = React.useState(false);
  const [dragHover, setDragHover] = React.useState(false);
  const barRef = React.useRef<PlayerBarHandle>(null);

  // Папки под live-слежением («Добавить папку» ставит на наблюдение).
  const [watched, setWatched] = React.useState<string[]>(loadWatched);
  // Актуальный набор наблюдаемых папок для async-кода: скан папки или событие
  // вотчера может завершиться уже ПОСЛЕ снятия папки с наблюдения (cleanup
  // эффекта срабатывает только после отрисовки) — сверяемся с ref, который
  // unwatchFolder обновляет синхронно, чтобы такие «опоздавшие» файлы не
  // вернулись в список.
  const watchedRef = React.useRef(watched);
  watchedRef.current = watched;

  // Тайпы: папки + привязки; фильтр (null — все, "" — «без тайпа», иначе id);
  // чип под курсором нативного драга; создаваемый/переименовываемый тайп.
  const [types, setTypes] = React.useState<TypesState>(loadTypes);
  const [typeFilter, setTypeFilter] = React.useState<string | null>(null);
  const [dropChip, setDropChip] = React.useState<string | null>(null);
  const [editingType, setEditingType] = React.useState<{ id: string | null } | null>(null);
  const [typeDraft, setTypeDraft] = React.useState("");
  // Рамки чипов для hit-test позиции нативного драга ("" — чип «Без тайпа»).
  const chipRefs = React.useRef(new Map<string, HTMLElement | null>());

  const liveTypeIds = React.useMemo(() => new Set(types.folders.map((f) => f.id)), [types.folders]);
  const typeNameById = React.useMemo(() => new Map(types.folders.map((f) => [f.id, f.name])), [types.folders]);
  // Тайп файла; привязки к удалённым папкам считаются «без тайпа».
  const typeOf = React.useCallback(
    (path: string): string | null => {
      const id = types.assign[path];
      return id && liveTypeIds.has(id) ? id : null;
    },
    [types.assign, liveTypeIds],
  );

  // Отображаемый порядок (фильтры + сортировка) — общий источник для таблицы,
  // prev/next, автоперехода и клавиатурной навигации.
  const visible = React.useMemo(() => {
    const lower = q.toLowerCase();
    const filtered = entries.filter((e) => {
      if (q && !e.name.toLowerCase().includes(lower)) return false;
      if (typeFilter == null) return true;
      const tid = typeOf(e.path);
      return typeFilter === "" ? tid == null : tid === typeFilter;
    });
    return [...filtered].sort((a, b) => {
      const am = a.meta, bm = b.meta;
      let va: number | string = 0, vb: number | string = 0;
      switch (sortCol) {
        case "name":     va = a.name.toLowerCase();         vb = b.name.toLowerCase(); break;
        case "format":   va = am?.format ?? "";             vb = bm?.format ?? ""; break;
        case "duration": va = am?.durationS ?? 0;           vb = bm?.durationS ?? 0; break;
        case "bpm":      va = am?.bpm ?? 0;                 vb = bm?.bpm ?? 0; break;
        case "key":      va = am?.key ?? "";                vb = bm?.key ?? ""; break;
        case "type":     va = typeNameById.get(typeOf(a.path) ?? "") ?? ""; vb = typeNameById.get(typeOf(b.path) ?? "") ?? ""; break;
        case "size":     va = am?.fileSizeBytes ?? 0;       vb = bm?.fileSizeBytes ?? 0; break;
        case "created":  va = a.createdAt ?? am?.createdAt ?? 0; vb = b.createdAt ?? bm?.createdAt ?? 0; break;
      }
      const cmp = typeof va === "string" ? va.localeCompare(vb as string) : (va as number) - (vb as number);
      return sortAsc ? cmp : -cmp;
    });
  }, [entries, q, sortCol, sortAsc, typeFilter, typeOf, typeNameById]);

  const activeEntry = React.useMemo(
    () => (activePath != null ? entries.find((e) => e.path === activePath) ?? null : null),
    [entries, activePath],
  );
  const visIdx = activePath != null ? visible.findIndex((e) => e.path === activePath) : -1;
  const hasPrev = visIdx > 0;
  const hasNext = visIdx >= 0 && visIdx < visible.length - 1;
  const goPrev = () => { if (hasPrev) setActivePath(visible[visIdx - 1].path); };
  const goNext = () => { if (hasNext) setActivePath(visible[visIdx + 1].path); };

  // Сколько файлов ещё анализируется (нет меты или она partial — DSP в пути).
  const pendingCount = entries.reduce((n, e) => n + (!e.meta || e.meta.partial ? 1 : 0), 0);

  // Горячие клавиши: Space — play/pause, ↑↓ — треки, Esc — закрыть панель.
  // Не перехватываем ввод в текстовых полях (поиск).
  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement | null;
      if (el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable)) return;
      if (e.code === "Space") {
        e.preventDefault();
        if (activeEntry) barRef.current?.toggle();
        else if (visible.length) setActivePath(visible[0].path);
      } else if (e.key === "ArrowDown") {
        e.preventDefault();
        if (visIdx < 0) { if (visible.length) setActivePath(visible[0].path); }
        else if (visIdx < visible.length - 1) setActivePath(visible[visIdx + 1].path);
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        if (visIdx < 0) { if (visible.length) setActivePath(visible[visible.length - 1].path); }
        else if (visIdx > 0) setActivePath(visible[visIdx - 1].path);
      } else if (e.key === "Escape") {
        if (activeEntry) setActivePath(null);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [visible, visIdx, activeEntry]);

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
          partial: raw.partial,
        };
        setEntries((prev) =>
          prev.map((e) => {
            if (e.path !== meta.path) return e;
            // Partial-метаданные не должны затирать уже пришедший полный анализ
            // (события двух стадий могут прийти в любом порядке).
            if (meta.partial && e.meta && !e.meta.partial) return e;
            return { ...e, meta };
          })
        );
      }
    ).then((fn) => { unlisten = fn; });

    return () => { unlisten?.(); };
  }, []);

  // Какой чип тайпа находится под точкой дропа (координаты в CSS-пикселях).
  const hitTestChip = React.useCallback((pos?: { x: number; y: number }): string | null => {
    if (!pos) return null;
    for (const [id, el] of chipRefs.current) {
      if (!el) continue;
      const r = el.getBoundingClientRect();
      if (pos.x >= r.left && pos.x <= r.right && pos.y >= r.top && pos.y <= r.bottom) return id;
    }
    return null;
  }, []);

  // Drag & drop: дроп в любое место добавляет файлы, дроп на чип тайпа ещё и
  // кладёт их в эту папку (работает и для строк таблицы — их нативный драг
  // Tauri видит как обычные файлы).
  React.useEffect(() => {
    let unlisten: (() => void) | null = null;
    onFileDrop(
      (paths, pos) => {
        setDropChip(null);
        addPaths(paths, "manual");
        const chip = hitTestChip(pos);
        if (chip != null) {
          for (const p of paths.filter(isAudioPath)) assignType(p, chip === "" ? null : chip);
        }
      },
      (h, pos) => {
        setDragHover(h);
        setDropChip(h ? hitTestChip(pos) : null);
      },
    ).then((fn) => { unlisten = fn; });
    return () => { unlisten?.(); };
  }, []);

  // Добавляем файлы и запускаем батч-анализ. origin — откуда пришли файлы:
  // "manual" или normPath папки-источника (см. Origin).
  const addPaths = React.useCallback(async (paths: string[], origin: Origin) => {
    const audio = paths.filter(isAudioPath);
    if (!audio.length) return;

    const newEntries: FileEntry[] = audio.map((p) => ({ path: p, name: fileName(p), meta: null, origin }));

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
    addPaths(paths, "manual");
  };

  // Папки, уже просканированные в этой сессии. Изменение набора watched не
  // должно заново гонять батч-анализ по старым папкам: каждый такой прогон —
  // это шквал мгновенных событий с пиками по каждому файлу и, для свежих
  // папок, повторный полный DSP-анализ параллельно с уже идущим.
  const syncedRef = React.useRef(new Set<string>());

  const handlePickFolder = async () => {
    const dir = await pickFolder();
    if (!dir) return;
    // Папка попадает под live-слежение; скан и добавление файлов делает
    // эффект по watched — ровно один батч-анализ на папку за сессию.
    const key = normPath(dir);
    syncedRef.current.delete(key); // повторный выбор той же папки = пересинк
    setWatched((prev) => (prev.some((d) => normPath(d) === key) ? [...prev] : [...prev, dir]));
  };

  const removePaths = React.useCallback((paths: string[]) => {
    const gone = new Set(paths);
    setEntries((prev) => prev.filter((e) => !gone.has(e.path)));
    setActivePath((cur) => (cur != null && gone.has(cur) ? null : cur));
  }, []);

  // Убрать папку из слежения вместе с её файлами: чип папки — источник
  // списка, как «Добавить папку» его наполняет. Файлы, покрытые другой
  // отслеживаемой папкой (вложенность), остаются.
  const unwatchFolder = (dir: string) => {
    const key = normPath(dir);
    const rest = watched.filter((x) => normPath(x) !== key);
    // Синхронно, до setState: скан/вотчер, завершившийся между кликом и
    // cleanup'ом эффекта, сверяется с этим ref и не вернёт файлы обратно.
    watchedRef.current = rest;
    setWatched(rest);
    const stillCovered = (p: string) => rest.some((d) => isUnderDir(p, d));
    setEntries((prev) =>
      prev.filter((e) => (!isUnderDir(e.path, dir) && e.origin !== key) || stillCovered(e.path)),
    );
    setActivePath((cur) => (cur != null && isUnderDir(cur, dir) && !stillCovered(cur) ? null : cur));
  };

  React.useEffect(() => {
    try { localStorage.setItem(WATCHED_KEY, JSON.stringify(watched)); } catch { /* не критично */ }
  }, [watched]);

  // Регистрация вотчера + синк содержимого. Синкается только то, что ещё не
  // синкалось в этой сессии: при старте — все папки (догоняем изменения,
  // накопленные пока приложение было закрыто), при добавлении — только новая.
  React.useEffect(() => {
    if (!isTauri()) return;
    invoke("player_watch_folders", { dirs: watched }).catch(() => {});
    const live = new Set(watched.map(normPath));
    for (const k of Array.from(syncedRef.current)) {
      if (!live.has(k)) syncedRef.current.delete(k);
    }
    let cancelled = false;
    (async () => {
      for (const dir of watched) {
        const key = normPath(dir);
        if (syncedRef.current.has(key)) continue;
        const files = await invoke<string[]>("player_scan_folder", { dir }).catch(() => null);
        if (cancelled) return;
        // Пока шёл скан, папку могли снять с наблюдения — файлы не добавляем.
        if (!watchedRef.current.some((d) => normPath(d) === key)) continue;
        if (!files) continue;
        syncedRef.current.add(key);
        if (files.length) addPaths(files, key);
        const have = new Set(files);
        setEntries((prev) =>
          prev.some((e) => isUnderDir(e.path, dir) && !have.has(e.path))
            ? prev.filter((e) => !isUnderDir(e.path, dir) || have.has(e.path))
            : prev,
        );
      }
    })();
    return () => { cancelled = true; };
  }, [watched, addPaths]);

  // Live-события от Rust-вотчера: файлы закинули в папку / удалили из неё.
  // added группируются по покрывающей наблюдаемой папке — она становится
  // origin записи; события по уже снятым с наблюдения папкам игнорируются.
  React.useEffect(() => {
    let unlisten: (() => void) | null = null;
    listenOnce<{ added?: string[]; removed?: string[] }>("player-fs-change", (p) => {
      if (p.added?.length) {
        const byDir = new Map<string, string[]>();
        for (const path of p.added) {
          const cover = watchedRef.current.find((d) => isUnderDir(path, d));
          if (!cover) continue;
          const key = normPath(cover);
          const list = byDir.get(key);
          if (list) list.push(path);
          else byDir.set(key, [path]);
        }
        for (const [key, paths] of byDir) addPaths(paths, key);
      }
      if (p.removed?.length) removePaths(p.removed);
    }).then((fn) => { unlisten = fn; });
    return () => { unlisten?.(); };
  }, [addPaths, removePaths]);

  const handleSort = (col: SortCol) => {
    setSortCol((prev) => {
      if (prev === col) { setSortAsc((a) => !a); return col; }
      setSortAsc(true);
      return col;
    });
  };

  const removeEntry = (path: string) => {
    setEntries((prev) => prev.filter((e) => e.path !== path));
    if (activePath === path) setActivePath(null);
  };

  // Привязки к тайпам при очистке сохраняем: если добавить те же файлы снова,
  // раскладка по папкам восстановится.
  const clearAll = () => {
    setEntries([]);
    setActivePath(null);
    setTypeFilter(null);
  };

  // ── Тайпы: CRUD и персистентность ──────────────────────────────────────────

  const createType = (name: string) => {
    const trimmed = name.trim();
    if (!trimmed) return;
    setTypes((prev) => ({ ...prev, folders: [...prev.folders, { id: newTypeId(), name: trimmed }] }));
  };

  const renameType = (id: string, name: string) => {
    const trimmed = name.trim();
    if (!trimmed) return;
    setTypes((prev) => ({
      ...prev,
      folders: prev.folders.map((f) => (f.id === id ? { ...f, name: trimmed } : f)),
    }));
  };

  const deleteType = (id: string) => {
    setTypes((prev) => {
      const assign = { ...prev.assign };
      for (const p of Object.keys(assign)) if (assign[p] === id) delete assign[p];
      return { folders: prev.folders.filter((f) => f.id !== id), assign };
    });
    setTypeFilter((cur) => (cur === id ? null : cur));
  };

  const assignType = React.useCallback((path: string, id: string | null) => {
    setTypes((prev) => {
      const assign = { ...prev.assign };
      if (id) assign[path] = id;
      else delete assign[path];
      return { ...prev, assign };
    });
  }, []);

  // Enter коммитит и вызывает blur того же инпута — ref-защита от двойного
  // срабатывания (второй вызов увидел бы устаревший editingType из замыкания).
  const editingTypeRef = React.useRef(editingType);
  editingTypeRef.current = editingType;
  const commitTypeEdit = () => {
    const cur = editingTypeRef.current;
    if (!cur) return;
    editingTypeRef.current = null;
    if (cur.id) renameType(cur.id, typeDraft);
    else createType(typeDraft);
    setEditingType(null);
    setTypeDraft("");
  };

  // Счётчики файлов для чипов.
  const typeCounts = React.useMemo(() => {
    const byId = new Map<string, number>();
    let none = 0;
    for (const e of entries) {
      const tid = typeOf(e.path);
      if (tid) byId.set(tid, (byId.get(tid) ?? 0) + 1);
      else none++;
    }
    return { byId, none };
  }, [entries, typeOf]);

  // Сохранение плейлиста. Метаданные анализа сыпятся сотнями событий, поэтому
  // сериализуем только состав (путь/имя/дата) и пишем при реальной смене.
  const lastSavedRef = React.useRef<string | null>(null);
  React.useEffect(() => {
    const s = JSON.stringify(
      entries.map((e) => ({ path: e.path, name: e.name, createdAt: e.createdAt ?? e.meta?.createdAt, origin: e.origin })),
    );
    if (s === lastSavedRef.current) return;
    lastSavedRef.current = s;
    try { localStorage.setItem(PLAYLIST_KEY, s); } catch { /* квота/приватный режим — не критично */ }
  }, [entries]);

  React.useEffect(() => {
    try { localStorage.setItem(TYPES_KEY, JSON.stringify(types)); } catch { /* не критично */ }
  }, [types]);

  // Восстановленный плейлист добираем заново: кэш анализа в Rust отдаёт
  // готовые результаты мгновенно, изменённые/новые файлы пересчитываются.
  React.useEffect(() => {
    if (!isTauri()) return;
    const paths = entries.map((e) => e.path);
    if (!paths.length) return;
    invoke<[string, number | null][]>("player_get_dates", { paths })
      .then((dates) => {
        const map = new Map(dates);
        setEntries((prev) =>
          prev.map((e) => {
            const ts = map.get(e.path);
            return ts != null && e.createdAt == null ? { ...e, createdAt: ts } : e;
          }),
        );
      })
      .catch(() => {});
    invoke("player_analyze_batch", { paths }).catch(() => {});
    // Только при монтировании: entries приходят из lazy-инициализатора.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Что показать под пустой таблицей: «нет файлов» / «пустой тайп» / «не найдено».
  const emptyText =
    entries.length === 0
      ? t.player.noFiles
      : visible.length === 0 && typeFilter != null && typeFilter !== "" && !q
        ? t.player.typeEmpty
        : t.common.nothingFound;

  // ── YouTube: пачка битов для диалога загрузки ──────────────────────────────
  const [ytBeats, setYtBeats] = React.useState<YtBeat[] | null>(null);

  const toYtBeat = React.useCallback((e: FileEntry): YtBeat => {
    const tid = typeOf(e.path);
    return {
      path: e.path,
      name: e.name,
      typeName: tid ? typeNameById.get(tid) ?? "" : "",
      bpm: e.meta?.bpm,
      key: e.meta?.key,
    };
  }, [typeOf, typeNameById]);

  // Кнопка тулбара выкладывает то, что на экране: открытый тайп (+ поиск).
  const openYtForVisible = () => { if (visible.length) setYtBeats(visible.map(toYtBeat)); };
  const openYtForOne = (path: string) => {
    const e = entries.find((x) => x.path === path);
    if (e) setYtBeats([toYtBeat(e)]);
  };

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
            {visible.length > 0 && (
              <button onClick={openYtForVisible} style={flToolBtn} title={t.player.ytDialogTitle}>
                <Icons.Yt width={13} height={13} />
                {t.player.ytUpload}
              </button>
            )}
            {pendingCount > 0 && (
              <span style={{ display: "flex", alignItems: "center", gap: 7, font: "600 11px var(--font-sans)", color: "var(--lcd-amber, #ffb55b)" }}>
                <span style={{ width: 7, height: 7, borderRadius: "50%", background: "currentColor", animation: "fl-led-pulse 0.8s ease-in-out infinite" }} />
                {t.player.analyzing} {entries.length - pendingCount}/{entries.length}
              </span>
            )}
            <div style={{ flex: 1 }} />
            <div style={{ display: "flex", alignItems: "center", gap: 9, height: 36, padding: "0 12px", background: "var(--work-3)", border: "1px solid var(--line-work)", borderRadius: 0, boxShadow: "inset 0 2px 5px rgba(0,0,0,.4)", width: 220 }}>
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
            {visible.length > 0 && (
              <Button size="sm" variant="ghost" onClick={openYtForVisible} title={t.player.ytDialogTitle}>
                <Icons.Yt width={13} height={13} />
                {t.player.ytUpload}
              </Button>
            )}
            {pendingCount > 0 && (
              <span style={{ fontSize: "var(--fs-sm)", color: "var(--text-faint)" }}>
                {t.player.analyzing} {entries.length - pendingCount}/{entries.length}
              </span>
            )}
            <div style={{ flex: 1 }} />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t.player.searchPlaceholder} style={{ width: 220 }} />
            {entries.length > 0 && (
              <Button size="sm" variant="ghost" onClick={clearAll}>{t.player.clearAll}</Button>
            )}
          </>
        )}
      </div>

      {/* Отслеживаемые папки: live-добавление новых файлов */}
      {watched.length > 0 && (
        <div style={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: 6, padding: isFl ? 0 : "0 20px 8px", flexShrink: 0 }}>
          <span style={{
            display: "inline-flex", alignItems: "center", gap: 6,
            font: isFl ? "700 10px var(--font-sans)" : undefined,
            fontSize: isFl ? undefined : 10, fontWeight: isFl ? undefined : 700,
            letterSpacing: "1px", textTransform: "uppercase",
            color: isFl ? "var(--ink-dim)" : "var(--text-faint)",
          }}>
            <span style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--positive, #46d46a)", boxShadow: "0 0 6px rgba(70,212,106,.6)", animation: "fl-led-pulse 1.6s ease-in-out infinite" }} />
            {t.player.watching}
          </span>
          {watched.map((d) => (
            <span
              key={d}
              title={`${d} — ${t.player.watchingHint}`}
              style={{
                display: "inline-flex", alignItems: "center", gap: 6,
                height: isFl ? 24 : 22, padding: "0 4px 0 9px",
                borderRadius: 0,
                border: `1px solid ${isFl ? "var(--line-work)" : "var(--border)"}`,
                color: isFl ? "var(--ink)" : "var(--text-muted)",
                background: isFl ? "linear-gradient(var(--btn-hi), var(--btn))" : "transparent",
                fontSize: isFl ? 11 : "var(--fs-sm)",
                fontFamily: isFl ? "var(--font-sans)" : undefined,
                userSelect: "none", flexShrink: 0,
              }}
            >
              <Icons.Folder width={11} height={11} style={{ opacity: 0.7 }} />
              <span style={{ maxWidth: 170, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{fileName(d)}</span>
              <span
                title={t.player.watchStop}
                onClick={() => unwatchFolder(d)}
                style={{ display: "flex", alignItems: "center", padding: 3, cursor: "pointer", opacity: 0.7 }}
              >
                <Icons.X width={10} height={10} />
              </span>
            </span>
          ))}
        </div>
      )}

      {/* Чипы тайпов — навигация по виртуальным папкам */}
      {(types.folders.length > 0 || entries.length > 0 || editingType != null) && (() => {
        const typeInput = (
          <input
            autoFocus
            value={typeDraft}
            onChange={(e) => setTypeDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitTypeEdit();
              else if (e.key === "Escape") { editingTypeRef.current = null; setEditingType(null); setTypeDraft(""); }
            }}
            onBlur={commitTypeEdit}
            placeholder={t.player.typeNamePlaceholder}
            style={{
              height: isFl ? 28 : 26,
              width: 150,
              padding: "0 10px",
              borderRadius: 0,
              border: "1px solid var(--accent)",
              background: isFl ? "var(--work-3)" : "var(--surface-2)",
              color: isFl ? "var(--ink-on-work)" : "var(--text-body)",
              font: isFl ? "600 11.5px var(--font-sans)" : undefined,
              fontSize: isFl ? undefined : "var(--fs-sm)",
              outline: "none",
              flexShrink: 0,
            }}
          />
        );
        return (
          <div style={{
            display: "flex", alignItems: "center", flexWrap: "wrap", gap: 6,
            padding: isFl ? 0 : "0 20px 10px",
            flexShrink: 0,
          }}>
            <TypeChip
              label={t.common.all}
              count={entries.length}
              active={typeFilter == null}
              isFl={isFl}
              onClick={() => setTypeFilter(null)}
            />
            {types.folders.map((f) =>
              editingType?.id === f.id ? (
                <React.Fragment key={f.id}>{typeInput}</React.Fragment>
              ) : (
                <TypeChip
                  key={f.id}
                  label={f.name}
                  count={typeCounts.byId.get(f.id) ?? 0}
                  active={typeFilter === f.id}
                  dropTarget={dropChip === f.id}
                  isFl={isFl}
                  title={t.player.renameTypeHint}
                  onClick={() => setTypeFilter((cur) => (cur === f.id ? null : f.id))}
                  onDoubleClick={() => { setEditingType({ id: f.id }); setTypeDraft(f.name); }}
                  onRename={() => { setEditingType({ id: f.id }); setTypeDraft(f.name); }}
                  renameTitle={t.player.renameType}
                  onDelete={() => deleteType(f.id)}
                  deleteTitle={t.player.deleteType}
                  chipRef={(el) => { chipRefs.current.set(f.id, el); }}
                />
              ),
            )}
            {types.folders.length > 0 && (
              <TypeChip
                label={t.player.noType}
                count={typeCounts.none}
                active={typeFilter === ""}
                dropTarget={dropChip === ""}
                isFl={isFl}
                onClick={() => setTypeFilter((cur) => (cur === "" ? null : ""))}
                chipRef={(el) => { chipRefs.current.set("", el); }}
              />
            )}
            {editingType != null && editingType.id == null ? (
              typeInput
            ) : (
              <button
                onClick={() => { setEditingType({ id: null }); setTypeDraft(""); }}
                title={t.player.addTypeTitle}
                style={isFl ? {
                  display: "inline-flex", alignItems: "center", gap: 5,
                  height: 28, padding: "0 10px",
                  background: "transparent",
                  border: "1px dashed var(--chrome-lo)", borderRadius: 0,
                  color: "var(--ink-dim)", font: "600 11.5px var(--font-sans)",
                  cursor: "pointer", flexShrink: 0,
                } : {
                  display: "inline-flex", alignItems: "center", gap: 5,
                  height: 26, padding: "0 10px",
                  background: "transparent",
                  border: "1px dashed var(--border)", borderRadius: 0,
                  color: "var(--text-faint)", fontSize: "var(--fs-sm)",
                  fontWeight: "var(--fw-semibold)" as any,
                  cursor: "pointer", flexShrink: 0,
                }}
              >
                <Icons.Plus width={11} height={11} />
                {t.player.addType}
              </button>
            )}
          </div>
        );
      })()}

      {/* Таблица */}
      <FileTable
        entries={visible}
        emptyText={emptyText}
        activePath={activePath}
        onActivate={setActivePath}
        onRemove={removeEntry}
        sortCol={sortCol}
        sortAsc={sortAsc}
        onSort={handleSort}
        typeFolders={types.folders}
        typeOf={typeOf}
        onAssign={assignType}
        onYt={openYtForOne}
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
            borderRadius: 0,
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
        <PlayerBar
          ref={barRef}
          entry={activeEntry}
          onClose={() => setActivePath(null)}
          onPrev={goPrev}
          onNext={goNext}
          hasPrev={hasPrev}
          hasNext={hasNext}
          onEnded={() => { if (hasNext) goNext(); }}
          isFl={isFl}
        />
      )}

      {/* Диалог загрузки на YouTube */}
      {ytBeats && (
        <YtUploadDialog beats={ytBeats} isFl={isFl} onClose={() => setYtBeats(null)} />
      )}
    </div>
  );
}

const flToolBtn: React.CSSProperties = {
  display: "flex", alignItems: "center", gap: 7,
  height: 36, padding: "0 12px",
  background: "linear-gradient(var(--btn-hi),var(--btn))",
  border: "1px solid var(--chrome-lo)", borderRadius: 0,
  color: "var(--ink)", font: "600 12.5px var(--font-sans)",
  cursor: "pointer",
  boxShadow: "inset 0 1px 0 rgba(255,255,255,.5),0 1px 2px rgba(0,0,0,.25)",
};
