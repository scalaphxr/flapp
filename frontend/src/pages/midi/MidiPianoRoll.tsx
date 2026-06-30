import React from "react";
import type { MidiNotesResult } from "@/shared/api/types";

const CANVAS_HEIGHT = 80;

interface MidiPianoRollProps {
  data: MidiNotesResult;
  playheadSec?: number;
  totalSec?: number;
  onSeek?: (sec: number) => void;
}

export function MidiPianoRoll({ data, playheadSec = 0, totalSec, onSeek }: MidiPianoRollProps) {
  const canvasRef = React.useRef<HTMLCanvasElement>(null);
  const containerRef = React.useRef<HTMLDivElement>(null);

  React.useEffect(() => {
    const canvas = canvasRef.current;
    const container = containerRef.current;
    if (!canvas || !container) return;

    function renderFrame() {
      const dpr = window.devicePixelRatio || 1;
      const W = container!.offsetWidth;
      if (W === 0) return;
      if (canvas!.width !== W * dpr || canvas!.height !== CANVAS_HEIGHT * dpr) {
        canvas!.width = W * dpr;
        canvas!.height = CANVAS_HEIGHT * dpr;
        canvas!.style.width = W + "px";
        canvas!.style.height = CANVAS_HEIGHT + "px";
      }
      drawPianoRoll(canvas!, data, playheadSec, dpr);
    }

    renderFrame();

    const ro = new ResizeObserver(renderFrame);
    ro.observe(container!);
    return () => ro.disconnect();
  }, [data, playheadSec]);

  function handleClick(e: React.MouseEvent<HTMLDivElement>) {
    if (!onSeek) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const ratio = (e.clientX - rect.left) / rect.width;
    const dur = totalSec ?? (data.durationTicks && data.ticksPerBeat && data.bpm
      ? data.durationTicks / data.ticksPerBeat / data.bpm * 60
      : 0);
    if (dur > 0) onSeek(Math.max(0, Math.min(1, ratio)) * dur);
  }

  return (
    <div
      ref={containerRef}
      onClick={handleClick}
      style={{
        width: "100%",
        height: CANVAS_HEIGHT,
        borderRadius: "var(--radius-sm)",
        overflow: "hidden",
        background: "var(--surface-2)",
        cursor: onSeek ? "pointer" : "default",
      }}
    >
      <canvas ref={canvasRef} style={{ display: "block" }} />
    </div>
  );
}

function drawPianoRoll(
  canvas: HTMLCanvasElement,
  data: MidiNotesResult,
  playheadSec: number,
  dpr: number,
) {
  const ctx = canvas.getContext("2d");
  if (!ctx) return;

  const W = canvas.width;
  const H = canvas.height;

  const rootStyle = getComputedStyle(document.documentElement);
  const surface2 = rootStyle.getPropertyValue("--surface-2").trim() || "#1a1814";
  const accent = rootStyle.getPropertyValue("--accent").trim() || "#c87941";
  const borderSoft = rootStyle.getPropertyValue("--border-soft").trim() || "rgba(255,255,255,0.06)";

  ctx.clearRect(0, 0, W, H);
  ctx.fillStyle = surface2;
  ctx.fillRect(0, 0, W, H);

  const notes = data.notes ?? [];
  if (!notes.length) {
    ctx.font = `${Math.round(11 * dpr)}px system-ui, sans-serif`;
    ctx.fillStyle = borderSoft;
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.fillText("no notes", W / 2, H / 2);
    return;
  }

  // Диапазон питча с отступом
  let minPitch = 127;
  let maxPitch = 0;
  for (const n of notes) {
    if (n.pitch < minPitch) minPitch = n.pitch;
    if (n.pitch > maxPitch) maxPitch = n.pitch;
  }
  minPitch = Math.max(0, minPitch - 2);
  maxPitch = Math.min(127, maxPitch + 2);
  const pitchRange = maxPitch - minPitch + 1;

  const totalTicks =
    data.durationTicks ||
    notes.reduce((m, n) => Math.max(m, n.tick + n.durationTicks), 0) ||
    1;

  const noteH = Math.max(2 * dpr, H / pitchRange);

  function tickToX(tick: number) {
    return (tick / totalTicks) * W;
  }
  function pitchToY(pitch: number) {
    // высокий питч = верх холста
    return H - ((pitch - minPitch + 1) / pitchRange) * H;
  }

  // Направляющие линии на нотах C (кратные 12)
  ctx.strokeStyle = borderSoft;
  ctx.lineWidth = dpr;
  for (let p = minPitch; p <= maxPitch; p++) {
    if (p % 12 === 0) {
      const y = pitchToY(p) + noteH / 2;
      ctx.beginPath();
      ctx.moveTo(0, y);
      ctx.lineTo(W, y);
      ctx.stroke();
    }
  }

  // Ноты
  for (const n of notes) {
    const x = tickToX(n.tick);
    const w = Math.max(2 * dpr, tickToX(n.tick + n.durationTicks) - x - dpr * 0.5);
    const y = pitchToY(n.pitch);
    ctx.globalAlpha = 0.45 + 0.55 * (n.velocity / 127);
    ctx.fillStyle = accent;
    ctx.fillRect(x, y, w, noteH - dpr * 0.5);
  }
  ctx.globalAlpha = 1;

  // Playhead
  if (playheadSec > 0 && data.ticksPerBeat > 0 && data.bpm > 0) {
    const playheadTick = (playheadSec * data.ticksPerBeat * data.bpm) / 60;
    if (playheadTick > 0 && playheadTick < totalTicks) {
      const x = tickToX(playheadTick);
      ctx.strokeStyle = "#fff";
      ctx.globalAlpha = 0.85;
      ctx.lineWidth = 1.5 * dpr;
      ctx.beginPath();
      ctx.moveTo(x, 0);
      ctx.lineTo(x, H);
      ctx.stroke();
      ctx.globalAlpha = 1;
    }
  }
}
