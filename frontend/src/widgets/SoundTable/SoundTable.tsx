import React from "react";
import { CategoryTag, Checkbox, Icons, PlayButton } from "@/shared/ui";
import { useT } from "@/shared/i18n";
import { formatBytes } from "@/shared/lib/format";
import type { Sample } from "@/shared/api/types";
import { ALL_CATEGORIES } from "@/shared/config/categories";
import { api } from "@/shared/api/client";
import { usePlayerStore } from "@/shared/model/player";
import { fileDragProps } from "@/shared/lib/dragOut";

interface SoundTableProps {
  samples: Sample[];
  playingId: number | null;
  onPlay: (id: number) => void;
  selectable?: boolean;
  selected?: Set<number>;
  onToggleSelect?: (id: number) => void;
  onRowClick?: (id: number) => void;
  onCategoryChange?: (id: number, category: string) => void;
  activeId?: number | null;
  emptyText?: string;
  showWaveform?: boolean;
  sortBy?: string | null;
  sortOrder?: "asc" | "desc";
  onSort?: (col: string) => void;
}

const originIcon: Record<string, keyof typeof Icons> = {
  archive: "Zip",
  project: "Flp",
  both: "Zip",
  folder: "Folder",
};

// Module-level cache пар [min, max]: переживает ре-рендеры, сбрасывается при перезагрузке страницы.
const peaksCache = new Map<number, [number, number][]>();

export function SoundTable({
  samples,
  playingId,
  onPlay,
  selectable = false,
  selected,
  onToggleSelect,
  onRowClick,
  onCategoryChange,
  activeId,
  emptyText,
  showWaveform = false,
  sortBy,
  sortOrder,
  onSort,
}: SoundTableProps) {
  const t = useT();
  // Явная сетка: фикс-чекбокс + фикс-плей + гибкий файл + фикс-тип + фикс-источник + фикс-размер.
  // Одинакова для шапки и строк — гарантирует выравнивание колонок.
  const cols = selectable ? "40px 44px minmax(0,1fr) 140px 162px 90px" : "44px minmax(0,1fr) 140px 162px 90px";

  function SortHeader({ col, children }: { col: string; children: React.ReactNode }) {
    if (!onSort) return <span>{children}</span>;
    const active = sortBy === col;
    return (
      <button
        onClick={() => onSort(col)}
        style={{
          background: "none",
          border: "none",
          cursor: "pointer",
          padding: 0,
          fontSize: "inherit",
          fontWeight: "inherit",
          letterSpacing: "inherit",
          textTransform: "inherit" as any,
          color: active ? "var(--accent)" : "inherit",
          display: "inline-flex",
          alignItems: "center",
          gap: 4,
        }}
      >
        {children}
        {active ? (
          <span style={{ fontSize: 9, lineHeight: 1 }}>{sortOrder === "asc" ? "↑" : "↓"}</span>
        ) : (
          <span style={{ fontSize: 9, lineHeight: 1, opacity: 0.3 }}>↕</span>
        )}
      </button>
    );
  }

  // gap совпадает с gap строк — шапка выравнивается по данным пиксель в пиксель
  const COL_GAP = 10;

  const hdrStyle: React.CSSProperties = {
    display: "grid", gridTemplateColumns: cols, alignItems: "center",
    gap: COL_GAP, padding: "0 14px", height: 36,
    fontSize: "var(--fs-label)", fontWeight: "var(--fw-semibold)" as any,
    letterSpacing: "var(--ls-label)", textTransform: "uppercase" as any,
    color: "var(--text-faint)", flexShrink: 0,
  };

  // ── Виртуализация ─────────────────────────────────────────────────────────
  // Высота строки фиксирована: 44px без волны, 70px с волной.
  const ROW_H = showWaveform ? 70 : 44;
  const BUFFER = 5; // строк вне видимой зоны сверху и снизу

  const scrollRef = React.useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = React.useState(0);
  const [viewH, setViewH] = React.useState(600);

  React.useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    setViewH(el.clientHeight);
    const ro = new ResizeObserver(() => setViewH(el.clientHeight));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const visStart = Math.max(0, Math.floor((scrollTop - BUFFER * ROW_H) / ROW_H));
  const visEnd   = Math.min(samples.length - 1, Math.ceil((scrollTop + viewH + BUFFER * ROW_H) / ROW_H));
  const topPad   = visStart * ROW_H;
  const botPad   = Math.max(0, (samples.length - visEnd - 1) * ROW_H);
  // ──────────────────────────────────────────────────────────────────────────

  // Пути выделенных строк: перетаскивание выделенной строки утаскивает всё
  // выделение (как в проводнике). Невыделенная строка тащится одна.
  const selectedPaths = React.useMemo(
    () =>
      selectable && selected && selected.size > 1
        ? samples.filter((s) => selected.has(s.id)).map((s) => s.path)
        : null,
    [selectable, selected, samples],
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", minHeight: 0, flex: 1 }}>
      <div style={hdrStyle}>
        {selectable ? <span /> : null}
        <span />
        <SortHeader col="name">{t.harvest.colFile}</SortHeader>
        <span>{t.harvest.colType}</span>
        <span>{t.harvest.colSource}</span>
        <span style={{ textAlign: "right" }}>{t.harvest.colSize}</span>
      </div>

      <div
        ref={scrollRef}
        onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)}
        style={{ overflowY: "auto", flex: 1, minHeight: 0, paddingRight: 2 }}
      >
        {topPad > 0 && <div style={{ height: topPad }} aria-hidden="true" />}
        {samples.slice(visStart, visEnd + 1).map((s, i) => (
          <SoundRow
            key={s.id}
            sample={s}
            zebra={(visStart + i) % 2 === 1}
            playing={playingId === s.id}
            onPlay={() => onPlay(s.id)}
            cols={cols}
            selectable={selectable}
            checked={selected?.has(s.id) ?? false}
            onToggle={() => onToggleSelect && onToggleSelect(s.id)}
            onRowClick={onRowClick ? () => onRowClick(s.id) : undefined}
            onCategoryChange={onCategoryChange ? (cat) => onCategoryChange(s.id, cat) : undefined}
            active={activeId === s.id}
            showWaveform={showWaveform}
            dragPaths={selectedPaths && selected?.has(s.id) ? selectedPaths : [s.path]}
          />
        ))}
        {botPad > 0 && <div style={{ height: botPad }} aria-hidden="true" />}
        {samples.length === 0 ? (
          <div style={{ padding: "40px 0", textAlign: "center", color: "var(--text-faint)", fontSize: "var(--fs-sm)" }}>
            {emptyText ?? t.common.nothingFound}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function SoundRow({
  sample,
  zebra,
  playing,
  onPlay,
  cols,
  selectable,
  checked,
  onToggle,
  onRowClick,
  onCategoryChange,
  active,
  showWaveform,
  dragPaths,
}: {
  sample: Sample;
  zebra: boolean;
  playing: boolean;
  onPlay: () => void;
  cols: string;
  selectable: boolean;
  checked: boolean;
  onToggle: () => void;
  onRowClick?: () => void;
  onCategoryChange?: (cat: string) => void;
  active?: boolean;
  showWaveform: boolean;
  dragPaths: string[];
}) {
  const t = useT();
  const [hover, setHover] = React.useState(false);
  const originKey = sample.origin || "archive";
  const OriginIcon = Icons[originIcon[originKey] ?? "Zip"];
  const originLabel = (t.origin as Record<string, string>)[originKey] ?? originKey;

  // COL_GAP дублируем здесь — rows и header должны иметь одинаковый gap
  const COL_GAP = 10;

  const rowBg = playing
    ? "var(--accent-soft)"
    : active
    ? "var(--surface-active)"
    : checked
    ? "var(--accent-soft)"
    : hover
    ? "var(--row-hover)"
    : "transparent";

  // Высота строки фиксирована — виртуализация опирается на константу ROW_H.
  // Без явной высоты контент мог бы растягивать строку и ломать расчёт паддингов.
  const rowHeight = showWaveform ? undefined : 44;
  const rowMinHeight = showWaveform ? 70 : undefined;

  return (
    <div
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      onClick={onRowClick}
      {...fileDragProps(() => dragPaths)}
      style={{
        display: "grid",
        gridTemplateColumns: cols,
        alignItems: "center",
        gap: COL_GAP,
        height: rowHeight,
        minHeight: rowMinHeight,
        padding: showWaveform ? "6px 14px" : "0 14px",
        borderRadius: "var(--radius-row)",
        cursor: onRowClick ? "pointer" : "default",
        background: rowBg,
        transition: "background var(--dur-fast) var(--ease-out)",
      }}
    >
      {selectable ? (
        <span onClick={(e) => e.stopPropagation()} style={{ display: "flex", alignItems: "center", justifyContent: "center" }}>
          <Checkbox checked={checked} onChange={onToggle} />
        </span>
      ) : null}

      <PlayButton playing={playing} size={32} onClick={(e) => { e.stopPropagation(); onPlay(); }} />

      {/* Ячейка «файл + волна»: minWidth:0 предотвращает переполнение 1fr */}
      <div style={{ minWidth: 0, display: "flex", flexDirection: "column", gap: 4 }}>
        <span style={{
          fontSize: "var(--fs-body)",
          color: "var(--text-body)",
          fontWeight: "var(--fw-medium)" as any,
          whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
        }}>
          {sample.name}
        </span>
        {showWaveform ? (
          <WaveformCanvas id={sample.id} playing={playing} durationSec={sample.features?.durationSeconds} />
        ) : (
          <span style={{
            fontSize: "11px",
            color: "var(--text-faint)",
            fontFamily: "var(--font-mono)",
            whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
          }}>
            {sample.sourceLabel || sample.path}
          </span>
        )}
      </div>

      <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
        {onCategoryChange ? (
          <div onClick={(e) => e.stopPropagation()} style={{ position: "relative", display: "inline-flex", alignItems: "center" }}>
            <CategoryTag category={sample.category} />
            <select value={sample.category} onChange={(e) => onCategoryChange(e.target.value)}
              style={{ position: "absolute", inset: 0, opacity: 0, cursor: "pointer", width: "100%" }}>
              {ALL_CATEGORIES.map((c) => <option key={c} value={c}>{c}</option>)}
            </select>
          </div>
        ) : (
          <CategoryTag category={sample.category} />
        )}
        {sample.auto ? (
          <span title={t.harvest.autoTag} style={{ display: "inline-flex", color: "var(--text-faint)" }}>
            <Icons.Info width={13} height={13} />
          </span>
        ) : null}
      </div>

      <div style={{ display: "flex", alignItems: "center", gap: 7, color: "var(--text-muted)", fontSize: "var(--fs-sm)" }}>
        <span style={{ display: "inline-flex", color: "var(--text-faint)" }}>
          <OriginIcon width={13} height={13} />
        </span>
        {originLabel}
      </div>

      <div style={{ textAlign: "right", fontFamily: "var(--font-mono)", fontSize: "var(--fs-sm)", color: "var(--text-muted)" }}>
        {formatBytes(sample.size)}
      </div>
    </div>
  );
}

// ── Waveform ───────────────────────────────────────────────────────────────────

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

// buildOffscreens рисует форму волны в два offscreen-канваса (dim и accent).
// Строится один раз при изменении размера, пиков или цветов. На каждом кадре
// RAF просто блитает нужный слой — тяжёлый цикл по 1500 барам не повторяется.
function buildOffscreens(
  pw: number, ph: number,
  peaks: [number, number][],
  dimColor: string, accentColor: string,
  offDim: React.MutableRefObject<HTMLCanvasElement | null>,
  offAccent: React.MutableRefObject<HTMLCanvasElement | null>,
) {
  const cy = ph / 2;
  const n = peaks.length;
  const pxPerBin = pw / n;

  for (const [ref, color] of [
    [offDim, dimColor],
    [offAccent, accentColor],
  ] as [React.MutableRefObject<HTMLCanvasElement | null>, string][]) {
    if (!ref.current) ref.current = document.createElement("canvas");
    const oc = ref.current;
    oc.width  = pw;
    oc.height = ph;
    const ctx = oc.getContext("2d");
    if (!ctx) continue;
    ctx.clearRect(0, 0, pw, ph);

    // Center baseline
    ctx.fillStyle = setAlpha(color, 0.20);
    ctx.fillRect(0, Math.round(cy) - 1, pw, 1);

    // Pass 1: bar bodies at 55% opacity
    ctx.fillStyle = setAlpha(color, 0.55);
    for (let i = 0; i < n; i++) {
      const [lo, hi] = peaks[i];
      const yTop = Math.round(cy - hi * cy * 0.90);
      const yBot = Math.round(cy - lo * cy * 0.90);
      const x    = Math.floor(i * pxPerBin);
      const bw   = Math.max(1, Math.ceil(pxPerBin));
      const bh   = Math.max(1, yBot - yTop + 1);
      if (bh > 2) ctx.fillRect(x, yTop + 1, bw, bh - 2);
    }

    // Pass 2: peak tips at 100% opacity (FL Studio bright edge)
    ctx.fillStyle = color;
    for (let i = 0; i < n; i++) {
      const [lo, hi] = peaks[i];
      const yTop = Math.round(cy - hi * cy * 0.90);
      const yBot = Math.round(cy - lo * cy * 0.90);
      const x    = Math.floor(i * pxPerBin);
      const bw   = Math.max(1, Math.ceil(pxPerBin));
      ctx.fillRect(x, yTop, bw, 1);
      if (yBot > yTop) ctx.fillRect(x, yBot, bw, 1);
    }
  }
}

// WaveformCanvas — canvas-волна с живым playhead и перемоткой кликом.
// RAF-цикл запускается ТОЛЬКО для воспроизводимой строки.
// Статичная форма волны кешируется в двух offscreen-канвасах (dim + accent)
// и блитается за O(1) вместо перебора 1500 баров на каждый кадр.
function WaveformCanvas({ id, playing, durationSec }: {
  id: number; playing: boolean; durationSec?: number
}) {
  const canvasRef   = React.useRef<HTMLCanvasElement>(null);
  // peaksRef: null = ещё не загружены, [] = формат без PCM (m4a/aac).
  const peaksRef    = React.useRef<[number, number][] | null>(peaksCache.get(id) ?? null);
  const rafRef      = React.useRef<number>(0);
  const drawFnRef   = React.useRef<((progress: number) => void) | null>(null);

  // Два offscreen-канваса: dim (приглушённый) и accent (яркий).
  const offDimRef    = React.useRef<HTMLCanvasElement | null>(null);
  const offAccentRef = React.useRef<HTMLCanvasElement | null>(null);
  // Ключ инвалидации: меняется при изменении размера, пиков или цветов темы.
  const offKeyRef    = React.useRef("");

  // Подписываемся на audio только для воспроизводимой строки.
  const audio = usePlayerStore(s => playing ? s.audio : null);
  const seek  = usePlayerStore(s => s.seek);

  function drawFrame(progress: number) {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const dpr  = Math.max(1, window.devicePixelRatio || 1);
    const rect = canvas.getBoundingClientRect();
    if (!rect.width) return;
    const pw = Math.round(rect.width * dpr);
    const ph = Math.round(rect.height * dpr);
    if (canvas.width !== pw || canvas.height !== ph) {
      canvas.width  = pw;
      canvas.height = ph;
    }

    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    const style  = getComputedStyle(canvas);
    const accent = style.getPropertyValue("--accent").trim() || "#E8845C";
    const dim    = style.getPropertyValue("--surface-3").trim() || "#2E261E";
    const head   = style.getPropertyValue("--text-strong").trim() || "#F4ECE3";

    ctx.clearRect(0, 0, pw, ph);
    const peaks = peaksRef.current;
    const cy    = ph / 2;

    if (!peaks || peaks.length === 0) {
      // Фолбэк для форматов без PCM (m4a/aac): тонкая центральная линия.
      ctx.fillStyle = dim;
      ctx.fillRect(0, Math.round(cy) - 1, pw, 2);
      return;
    }

    // Инвалидируем offscreen-кеш при изменении размера, пиков или цветовой темы.
    const cacheKey = `${pw}x${ph}x${peaks.length}x${dim}x${accent}`;
    if (offKeyRef.current !== cacheKey) {
      buildOffscreens(pw, ph, peaks, dim, accent, offDimRef, offAccentRef);
      offKeyRef.current = cacheKey;
    }

    // Блитаем dim-слой (вся волна приглушённым цветом).
    if (offDimRef.current) ctx.drawImage(offDimRef.current, 0, 0);

    // Поверх dim накладываем accent для проигранной части через clip.
    if (progress > 0 && offAccentRef.current) {
      const playX = Math.round(progress * pw);
      ctx.save();
      ctx.beginPath();
      ctx.rect(0, 0, playX, ph);
      ctx.clip();
      ctx.drawImage(offAccentRef.current, 0, 0);
      ctx.restore();
    }

    // Playhead — вертикальная линия на текущей позиции.
    if (playing && progress > 0 && progress < 1) {
      const px = Math.round(progress * pw);
      ctx.fillStyle = head;
      ctx.fillRect(Math.max(0, px - 1), 0, 2, ph);
    }
  }

  drawFnRef.current = drawFrame;

  // RAF-цикл: только для активной (воспроизводимой) строки.
  React.useEffect(() => {
    cancelAnimationFrame(rafRef.current);
    if (!playing || !audio) {
      requestAnimationFrame(() => drawFnRef.current?.(0));
      return;
    }

    // При уточнении браузером длины VBR MP3 сразу обновляем playhead.
    const onDurationChange = () => {
      const audioDur = audio.duration;
      const dur = (durationSec && durationSec > 0)
        ? durationSec
        : (isFinite(audioDur) && audioDur > 0 ? audioDur : 0);
      drawFnRef.current?.(dur > 0 ? Math.min(1, audio.currentTime / dur) : 0);
    };
    audio.addEventListener("durationchange", onDurationChange);

    function loop() {
      const audioDur = audio!.duration;
      // durationSec из харвеста надёжнее браузерной оценки для VBR MP3.
      const dur = (durationSec && durationSec > 0)
        ? durationSec
        : (isFinite(audioDur) && audioDur > 0 ? audioDur : 0);
      drawFnRef.current?.(dur > 0 ? Math.min(1, audio!.currentTime / dur) : 0);
      rafRef.current = requestAnimationFrame(loop);
    }
    rafRef.current = requestAnimationFrame(loop);
    return () => {
      cancelAnimationFrame(rafRef.current);
      audio.removeEventListener("durationchange", onDurationChange);
    };
  }, [playing, audio, durationSec]);

  // Ленивая загрузка пиков через IntersectionObserver (один раз на id).
  React.useEffect(() => {
    if (peaksRef.current !== null) {
      requestAnimationFrame(() => drawFnRef.current?.(0));
      return;
    }
    const canvas = canvasRef.current;
    if (!canvas) return;
    const obs = new IntersectionObserver((entries) => {
      if (!entries[0].isIntersecting) return;
      obs.disconnect();
      api.samplePeaks(id, 1500)
        .then((res) => {
          peaksRef.current = res.peaks;
          peaksCache.set(id, res.peaks);
          requestAnimationFrame(() => drawFnRef.current?.(0));
        })
        .catch(() => { peaksRef.current = []; });
    }, { threshold: 0 });
    obs.observe(canvas);
    return () => obs.disconnect();
  }, [id]);

  function handleClick(e: React.MouseEvent<HTMLCanvasElement>) {
    if (!playing || !audio) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const ratio = (e.clientX - rect.left) / rect.width;
    // Same duration priority as the playhead loop above: harvest durationSec
    // beats the browser's (VBR-MP3-unreliable) audio.duration estimate.
    const audioDur = audio.duration;
    const dur = (durationSec && durationSec > 0)
      ? durationSec
      : (isFinite(audioDur) && audioDur > 0 ? audioDur : 0);
    seek(ratio, dur);
  }

  return (
    <canvas
      ref={canvasRef}
      onClick={handleClick}
      style={{
        width: "100%",
        height: 40,
        display: "block",
        borderRadius: 3,
        cursor: playing ? "pointer" : "default",
        opacity: 1,
      }}
    />
  );
}
