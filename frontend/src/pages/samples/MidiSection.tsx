import React from "react";
import { Button, Card, Checkbox, Icons, Input, PlayButton } from "@/shared/ui";
import { useT } from "@/shared/i18n";
import { api } from "@/shared/api/client";
import type { MidiClip, MidiNote, MidiNotesResult } from "@/shared/api/types";
import { ALL_MIDI_CATEGORIES } from "@/shared/api/types";
import { useJobsStore } from "@/shared/model/jobs";
import { useSettingsStore } from "@/shared/model/settings";
import { formatDuration } from "@/shared/lib/format";
import { pickFolder } from "@/shared/lib/tauri";
import { fileDragProps } from "@/shared/lib/dragOut";
import { useMidiPlayer } from "../midi/useMidiPlayer";

// ── Category colours ──────────────────────────────────────────────────────────
const CAT_COLOR: Record<string, string> = {
  "808/Bass": "var(--cat-808)",   Melody: "var(--cat-hihat)",
  Kick:       "var(--cat-kick)",  Snare:  "var(--cat-snare)",
  Clap:       "var(--cat-clap)",  "Hi-Hat": "var(--cat-hihat)",
  "Open Hat": "var(--cat-openhat)", Perc: "var(--cat-perc)",
  Drums:      "var(--cat-kick)",  FX:     "var(--cat-fx)",
  Other:      "var(--cat-unsorted)",
};
const CAT_BG: Record<string, string> = {
  "808/Bass": "var(--cat-808-bg)",   Melody: "var(--cat-hihat-bg)",
  Kick:       "var(--cat-kick-bg)",  Snare:  "var(--cat-snare-bg)",
  Clap:       "var(--cat-clap-bg)",  "Hi-Hat": "var(--cat-hihat-bg)",
  "Open Hat": "var(--cat-openhat-bg)", Perc: "var(--cat-perc-bg)",
  Drums:      "var(--cat-kick-bg)",  FX:     "var(--cat-fx-bg)",
  Other:      "var(--cat-unsorted-bg)",
};

// Row height must match the virtualization constant ROW_H below
const ROW_H_STD = 68;

// ── Column grids — identical pattern to SoundTable ────────────────────────────
const COLS_STD = "40px 44px minmax(0,1fr) 132px minmax(0,.9fr) 72px 44px";
const COL_GAP  = 10;

// ── Helpers ───────────────────────────────────────────────────────────────────
function soundName(clip: MidiClip): string {
  if (clip.channelName) return clip.channelName;
  if (clip.samplePath) {
    const b = clip.samplePath.replace(/\\/g, "/").split("/").pop() ?? "";
    const n = b.replace(/\.[^.]+$/, "");
    if (n) return n;
  }
  return `Ch ${clip.channelIndex}`;
}

// ── Main section ──────────────────────────────────────────────────────────────
export function MidiSection() {
  const t = useT();
  const jobs = useJobsStore();
  const { settings, load: loadSettings, update: updateSettings } = useSettingsStore();

  const [clips, setClips] = React.useState<MidiClip[]>([]);
  const [filterCat, setFilterCat] = React.useState("");
  const [searchQ, setSearchQ] = React.useState("");
  const [selected, setSelected] = React.useState<Set<string>>(new Set());
  const [packName, setPackName] = React.useState("");
  const [donePath, setDonePath] = React.useState<string | null>(null);
  const [dedupMsg, setDedupMsg] = React.useState<string | null>(null);
  const [dedupRunning, setDedupRunning] = React.useState(false);

  // virtualization
  const scrollRef = React.useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = React.useState(0);
  const [viewH, setViewH] = React.useState(600);
  const ROW_H = ROW_H_STD;
  const BUFFER = 4;

  React.useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    setViewH(el.clientHeight);
    const ro = new ResizeObserver(() => setViewH(el.clientHeight));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  React.useEffect(() => {
    if (!settings) loadSettings();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  React.useEffect(() => {
    return jobs.onDone((job) => {
      if (job.type === "extract_midi" && job.status === "completed") loadClips();
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function loadClips() {
    try {
      const { items } = await api.midiClips(filterCat || undefined);
      setClips(items);
    } catch (e) { console.error("midiClips:", e); }
  }

  React.useEffect(() => { loadClips(); /* eslint-disable-next-line */ }, [filterCat]);

  async function handleDownload(clip: MidiClip) {
    const url = await api.midiClipFileUrl(clip.id);
    const a = document.createElement("a"); a.href = url; a.download = clip.fileName; a.click();
  }

  async function handlePack() {
    if (selected.size === 0) return;
    let outputDir = settings?.midiOutputDir ?? "";
    if (!outputDir) {
      const chosen = await pickFolder();
      if (!chosen) return;
      outputDir = chosen;
      await updateSettings({ midiOutputDir: outputDir });
    }
    try {
      const { jobId: id } = await api.midiPack([...selected], packName || undefined, outputDir);
      jobs.onDone((job) => {
        if (job.id === id && job.status === "completed") setDonePath((job.result as any)?.path ?? null);
      });
    } catch (e) { console.error("midiPack:", e); }
  }

  async function handleClear() {
    await api.midiClear();
    setClips([]); setSelected(new Set()); setDonePath(null); setDedupMsg(null);
  }

  async function handleDedup() {
    setDedupRunning(true);
    setDedupMsg(null);
    try {
      const result = await api.midiDedup();
      if (result.removed === 0) {
        setDedupMsg("Дубликатов не найдено");
      } else {
        setDedupMsg(`Удалено ${result.removed} дубл. в ${result.groups} группах`);
        await loadClips(); // refresh list
        setSelected(new Set());
      }
    } catch (e) {
      console.error("midiDedup:", e);
    } finally {
      setDedupRunning(false);
    }
  }

  async function handleCategoryChange(clipId: string, cat: string) {
    setClips((p) => p.map((c) => c.id === clipId ? { ...c, category: cat as any, categoryOverride: true } : c));
    try { await api.midiSetClipCategory(clipId, cat); } catch (e) { console.error(e); }
  }

  const filtered = React.useMemo(() => {
    if (!searchQ.trim()) return clips;
    const q = searchQ.toLowerCase();
    return clips.filter((c) =>
      c.patternName.toLowerCase().includes(q) ||
      c.channelName.toLowerCase().includes(q) ||
      (c.sourceName || c.projectName).toLowerCase().includes(q)
    );
  }, [clips, searchQ]);

  const allChecked = selected.size === filtered.length && filtered.length > 0;
  const cols = COLS_STD;

  // Пути .mid файлов выделенных клипов: перетаскивание выделенной строки
  // утаскивает всё выделение. Клипы без файла на диске отфильтрует хелпер.
  const selectedMidPaths = React.useMemo(
    () =>
      selected.size > 1
        ? clips.filter((c) => selected.has(c.id)).map((c) => c.filePath)
        : null,
    [selected, clips],
  );

  const visStart = Math.max(0, Math.floor((scrollTop - BUFFER * ROW_H) / ROW_H));
  const visEnd   = Math.min(filtered.length - 1, Math.ceil((scrollTop + viewH + BUFFER * ROW_H) / ROW_H));
  const topPad   = visStart * ROW_H;
  const botPad   = Math.max(0, (filtered.length - visEnd - 1) * ROW_H);

  const hdrStyle: React.CSSProperties =
    { display: "grid", gridTemplateColumns: cols, alignItems: "center", gap: COL_GAP, padding: "0 14px", height: 36, fontSize: "var(--fs-label)", fontWeight: "var(--fw-semibold)" as any, letterSpacing: "var(--ls-label)", textTransform: "uppercase" as any, color: "var(--text-faint)", flexShrink: 0 };

  if (clips.length === 0 && !filterCat) {
    return (
      <div style={{ flex: 1, display: "flex", alignItems: "center", justifyContent: "center", color: "var(--text-muted)", fontSize: "var(--fs-sm)" }}>
        {t.midi.noClips}
      </div>
    );
  }

  return (
    <div style={{ flex: 1, display: "flex", flexDirection: "column", minHeight: 0, gap: 10, overflow: "hidden" }}>

      {/* ── Top bar ── */}
      <div style={{ display: "flex", alignItems: "center", gap: 10, flexShrink: 0, marginBottom: 0 }}>
        <Input icon={<Icons.Search />} placeholder={t.common.search} value={searchQ} onChange={(e) => setSearchQ(e.target.value)} style={{ flex: 1 }} />
        <Button variant="ghost" icon={<Icons.Dedup />} onClick={handleDedup} disabled={dedupRunning || clips.length === 0}>{t.midi.dedupBtn}</Button>
        <Button variant="ghost" icon={<Icons.Trash />} onClick={handleClear}>{t.midi.clearClips}</Button>
      </div>

      {/* ── Card / table ── */}
      <div style={{ flex: 1, display: "flex", flexDirection: "column", minHeight: 0, overflow: "hidden", borderRadius: "var(--radius-card)", border: "1px solid var(--border-card)", background: "var(--surface-card)" }}>
        {/* Category pills */}
        <div style={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: "var(--space-2)", padding: "8px 14px", borderBottom: "1px solid var(--border-soft)", flexShrink: 0 }}>
          <CatPill label="All" active={!filterCat} color="var(--text-muted)" onClick={() => setFilterCat("")} />
          {ALL_MIDI_CATEGORIES.map((cat) => (
            <CatPill key={cat} label={cat} active={filterCat === cat} color={CAT_COLOR[cat] ?? "var(--text-muted)"} onClick={() => setFilterCat(filterCat === cat ? "" : cat)} />
          ))}
        </div>

        {/* Header */}
        <div style={hdrStyle}>
          <span style={{ display: "flex", alignItems: "center", justifyContent: "center" }}>
            <Checkbox checked={allChecked} onChange={() => {
              if (allChecked) setSelected(new Set());
              else setSelected(new Set(filtered.map((c) => c.id)));
            }} />
          </span>
          <span />
          <span>{t.midi.colName}</span>
          <span>{t.midi.colCategory}</span>
          <span>{t.midi.colProject}</span>
          <span style={{ textAlign: "right" }}>{t.midi.colDuration}</span>
          <span />
        </div>

        {/* Rows (virtualized) */}
        <div ref={scrollRef} onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)} style={{ overflowY: "auto", flex: 1, minHeight: 0 }}>
          {topPad > 0 && <div style={{ height: topPad }} />}
          {filtered.slice(visStart, visEnd + 1).map((clip, i) => (
            <MidiRow
              key={clip.id}
              clip={clip}
              cols={cols}
              rowH={ROW_H}
              zebra={(visStart + i) % 2 === 1}
              selected={selected.has(clip.id)}
              onToggle={() => setSelected((p) => { const n = new Set(p); n.has(clip.id) ? n.delete(clip.id) : n.add(clip.id); return n; })}
              onDownload={() => handleDownload(clip)}
              onCategoryChange={(cat) => handleCategoryChange(clip.id, cat)}
              dragPaths={selectedMidPaths && selected.has(clip.id) ? selectedMidPaths : [clip.filePath]}
            />
          ))}
          {botPad > 0 && <div style={{ height: botPad }} />}
          {filtered.length === 0 && (
            <div style={{ padding: "40px 0", textAlign: "center", color: "var(--text-faint)", fontSize: "var(--fs-sm)" }}>
              {t.common.nothingFound}
            </div>
          )}
        </div>
      </div>

      {/* ── Bottom bar ── */}
      {selected.size > 0 ? (
        <Card padding={0} style={{ flexShrink: 0 }}>
          <div style={{ padding: "10px 16px", display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
            <span style={{ fontSize: "var(--fs-sm)", color: "var(--accent)", fontWeight: "var(--fw-semibold)" as any }}>{selected.size} {t.samples.selCount}</span>
            <Button variant="ghost" size="sm" onClick={() => setSelected(new Set())}>{t.samples.clearSel}</Button>
            <Button variant="ghost" size="sm" onClick={() => setSelected(new Set(filtered.map((c) => c.id)))}>{t.samples.selectAll}</Button>
            <div style={{ flex: 1 }} />
            <Button variant="secondary" size="sm" icon={<Icons.Folder />} onClick={() => pickFolder().then((d) => { if (d) void updateSettings({ midiOutputDir: d }); })}>{t.midi.outputDirPick}</Button>
            <Input placeholder={t.midi.packNamePlaceholder} value={packName} onChange={(e) => setPackName(e.target.value)} style={{ width: 160 }} />
            <Button variant="primary" size="sm" icon={<Icons.Zip />} onClick={handlePack}>{t.midi.packSelected} ({selected.size})</Button>
          </div>
        </Card>
      ) : (
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "4px 12px", height: "auto", background: "transparent", borderTop: "none", flexShrink: 0 }}>
          <span style={{ fontSize: "var(--fs-sm)", color: "var(--text-faint)" }}>{filtered.length} clips</span>
          <Button variant="ghost" size="sm" onClick={() => setSelected(new Set(filtered.map((c) => c.id)))}>{t.samples.selectAll}</Button>
        </div>
      )}

      {donePath && (
        <div style={{ padding: "10px 14px", borderRadius: "var(--radius-md)", background: "color-mix(in srgb, var(--positive) 14%, transparent)", flexShrink: 0 }}>
          <span style={{ fontSize: "var(--fs-sm)", color: "var(--positive)", fontWeight: "var(--fw-semibold)" as any }}>{t.midi.packDone}:</span>
          <span className="mono" style={{ fontSize: 11, color: "var(--text-muted)", marginLeft: 12, wordBreak: "break-all" }}>{donePath}</span>
        </div>
      )}
      {dedupMsg && (
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "8px 14px", borderRadius: "var(--radius-md)", background: "color-mix(in srgb, var(--accent) 12%, transparent)", flexShrink: 0 }}>
          <span style={{ fontSize: "var(--fs-sm)", color: "var(--accent)", fontWeight: "var(--fw-semibold)" as any }}>{dedupMsg}</span>
          <button onClick={() => setDedupMsg(null)} style={{ background: "none", border: "none", cursor: "pointer", color: "var(--text-faint)", fontSize: 16, lineHeight: 1 }}>×</button>
        </div>
      )}
    </div>
  );
}

// ── Category pill ─────────────────────────────────────────────────────────────
function CatPill({ label, active, color, onClick }: { label: string; active: boolean; color: string; onClick: () => void }) {
  const [hov, setHov] = React.useState(false);
  return (
    <button onClick={onClick} onMouseEnter={() => setHov(true)} onMouseLeave={() => setHov(false)} style={{
      padding: "3px 11px", borderRadius: "var(--radius-pill)",
      border: active ? `1.5px solid ${color}` : "1.5px solid var(--border-medium)",
      background: active ? `color-mix(in srgb, ${color} 18%, transparent)` : hov ? "var(--surface-3)" : "transparent",
      color: active ? color : "var(--text-muted)",
      fontSize: "var(--fs-sm)", fontFamily: "var(--font-sans)", fontWeight: active ? 600 : 400,
      cursor: "pointer", transition: "all 100ms", lineHeight: 1,
    }}>{label}</button>
  );
}

// ── Category badge ────────────────────────────────────────────────────────────
function CatBadge({ cat, overridden }: { cat: string; overridden?: boolean }) {
  return (
    <span style={{ color: CAT_COLOR[cat] ?? "var(--cat-unsorted)", background: CAT_BG[cat] ?? "var(--cat-unsorted-bg)", fontWeight: 500, fontSize: "var(--fs-caption)", letterSpacing: "var(--ls-label)", textTransform: "uppercase", padding: "2px 7px", borderRadius: "var(--radius-pill)", whiteSpace: "nowrap", display: "inline-flex", alignItems: "center", gap: 4 }}>
      {cat}
      {overridden && <span style={{ width: 5, height: 5, borderRadius: "50%", background: CAT_COLOR[cat], opacity: 0.7, flexShrink: 0 }} />}
    </span>
  );
}

// ── Inline mini piano roll ────────────────────────────────────────────────────
function MiniRoll({ notes, durationTicks, ticksPerBeat, bpm, playheadSec, totalSec, onSeek }: {
  notes: MidiNote[];
  durationTicks: number;
  ticksPerBeat: number;
  bpm: number;
  playheadSec: number;
  totalSec?: number;
  onSeek?: (sec: number) => void;
}) {
  const canvasRef = React.useRef<HTMLCanvasElement>(null);
  const H = 32;

  React.useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const W = canvas.offsetWidth;
    if (!W) return;
    canvas.width = W * dpr; canvas.height = H * dpr;

    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    const root = getComputedStyle(document.documentElement);
    const accent = root.getPropertyValue("--accent").trim() || "#c87941";
    const bg = "transparent";
    const gridLine = "rgba(255,255,255,.06)";

    ctx.clearRect(0, 0, W * dpr, H * dpr);
    ctx.fillStyle = bg;
    ctx.fillRect(0, 0, W * dpr, H * dpr);

    if (!notes.length) return;

    const totalTicks = durationTicks || notes.reduce((m, n) => Math.max(m, n.tick + n.durationTicks), 0) || 1;
    let minP = 127, maxP = 0;
    for (const n of notes) { if (n.pitch < minP) minP = n.pitch; if (n.pitch > maxP) maxP = n.pitch; }
    minP = Math.max(0, minP - 1); maxP = Math.min(127, maxP + 1);
    const pitchRange = maxP - minP + 1;
    const noteH = Math.max(1.5 * dpr, (H * dpr) / pitchRange);

    // grid lines at C notes
    ctx.strokeStyle = gridLine; ctx.lineWidth = dpr;
    for (let p = minP; p <= maxP; p++) {
      if (p % 12 === 0) {
        const y = H * dpr - ((p - minP + 1) / pitchRange) * H * dpr + noteH / 2;
        ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(W * dpr, y); ctx.stroke();
      }
    }

    // notes
    for (const n of notes) {
      const x = (n.tick / totalTicks) * W * dpr;
      const w = Math.max(1.5 * dpr, (n.durationTicks / totalTicks) * W * dpr - dpr * 0.5);
      const y = H * dpr - ((n.pitch - minP + 1) / pitchRange) * H * dpr;
      ctx.globalAlpha = 0.4 + 0.6 * (n.velocity / 127);
      ctx.fillStyle = accent;
      ctx.fillRect(x, y, w, noteH - dpr * 0.5);
    }
    ctx.globalAlpha = 1;

    // playhead
    if (playheadSec > 0 && ticksPerBeat > 0 && bpm > 0) {
      const playTick = (playheadSec * ticksPerBeat * bpm) / 60;
      if (playTick > 0 && playTick < totalTicks) {
        const x = (playTick / totalTicks) * W * dpr;
        ctx.strokeStyle = "#fff"; ctx.globalAlpha = 0.9; ctx.lineWidth = 1.5 * dpr;
        ctx.beginPath(); ctx.moveTo(x, 0); ctx.lineTo(x, H * dpr); ctx.stroke();
        ctx.globalAlpha = 1;
      }
    }
  }, [notes, durationTicks, ticksPerBeat, bpm, playheadSec, H]);

  const handleSeek = React.useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    if (!onSeek || !totalSec) return;
    e.stopPropagation();
    const rect = e.currentTarget.getBoundingClientRect();
    const frac = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    onSeek(frac * totalSec);
  }, [onSeek, totalSec]);

  return (
    <div
      style={{ width: "100%", height: H, borderRadius: 3, overflow: "hidden", cursor: onSeek ? "pointer" : undefined }}
      onClick={handleSeek}
    >
      <canvas ref={canvasRef} style={{ display: "block", width: "100%", height: H }} />
    </div>
  );
}

// ── MIDI row ──────────────────────────────────────────────────────────────────
function MidiRow({ clip, cols, rowH, zebra, selected, onToggle, onDownload, onCategoryChange, dragPaths }: {
  clip: MidiClip; cols: string; rowH: number; zebra: boolean; selected: boolean;
  onToggle: () => void; onDownload: () => void; onCategoryChange: (cat: string) => void;
  dragPaths: (string | undefined)[];
}) {
  const t = useT();
  const [hover, setHover] = React.useState(false);
  const [editingCat, setEditingCat] = React.useState(false);
  const [notesData, setNotesData] = React.useState<MidiNotesResult | null>(null);
  const notesLoadedRef = React.useRef(false);

  // Lazy-load notes on first render
  React.useEffect(() => {
    if (notesLoadedRef.current) return;
    notesLoadedRef.current = true;
    api.midiClipNotes(clip.id)
      .then((res) => setNotesData(res))
      .catch(() => { /* silent */ });
  }, [clip.id]);

  const hasSample = !!clip.samplePath;
  const selfCut = clip.category !== "Melody";
  const fallbackPiano = !hasSample && clip.category === "Melody";
  const canPlay = hasSample || fallbackPiano;
  const { isPlaying, playheadSec, totalSec, play, stop, seek } = useMidiPlayer(clip.id, notesData, hasSample, selfCut, fallbackPiano);

  const rowBg = isPlaying ? "var(--accent-soft)" : selected ? "var(--accent-softer)" : hover ? "var(--row-hover)" : zebra ? "var(--row-zebra)" : "transparent";

  return (
    <div
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      {...fileDragProps(() => dragPaths)}
      style={{ display: "grid", gridTemplateColumns: cols, alignItems: "center", gap: COL_GAP, height: rowH, padding: "6px 14px", background: rowBg, transition: "background 100ms" }}
    >
      {/* Checkbox */}
      <span onClick={(e) => e.stopPropagation()} style={{ display: "flex", alignItems: "center", justifyContent: "center" }}>
        <Checkbox checked={selected} onChange={onToggle} />
      </span>

      {/* Play button */}
      <span onClick={(e) => e.stopPropagation()}>
        <PlayButton playing={isPlaying} size={32} onClick={() => isPlaying ? stop() : play()} style={{ opacity: canPlay ? 1 : 0.35, cursor: canPlay ? "pointer" : "not-allowed" }} />
      </span>

      {/* Name + inline piano roll */}
      <div style={{ minWidth: 0, display: "flex", flexDirection: "column", gap: 4 }}>
        <span style={{ fontSize: "var(--fs-body)", color: "var(--text-body)", fontWeight: "var(--fw-medium)" as any, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
          {soundName(clip)}
          {clip.patternName && (
            <span style={{ fontWeight: 400, fontSize: "var(--fs-caption)", color: "var(--text-faint)", marginLeft: 6 }}>
              {clip.patternName}
            </span>
          )}
        </span>
        {notesData ? (
          <MiniRoll
            notes={notesData.notes}
            durationTicks={notesData.durationTicks}
            ticksPerBeat={notesData.ticksPerBeat}
            bpm={notesData.bpm}
            playheadSec={playheadSec}
            totalSec={canPlay ? totalSec : undefined}
            onSeek={canPlay ? seek : undefined}
          />
        ) : (
          <div style={{ height: 32, borderRadius: 3, background: "var(--surface-3)", opacity: 0.5 }} />
        )}
      </div>

      {/* Category */}
      <span onClick={(e) => e.stopPropagation()}>
        {editingCat ? (
          <select autoFocus value={clip.category} onChange={(e) => { onCategoryChange(e.target.value); setEditingCat(false); }} onBlur={() => setEditingCat(false)}
            style={{ height: 24, padding: "0 6px", background: "var(--surface-input)", color: "var(--text-body)", border: "1px solid var(--accent)", borderRadius: "var(--radius-sm)", fontFamily: "var(--font-sans)", fontSize: "var(--fs-caption)", cursor: "pointer", outline: "none" }}>
            {ALL_MIDI_CATEGORIES.map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
        ) : (
          <span onClick={() => setEditingCat(true)} title="Click to change" style={{ cursor: "pointer" }}>
            <CatBadge cat={clip.category} overridden={clip.categoryOverride} />
          </span>
        )}
      </span>

      {/* Project */}
      <span style={{ fontSize: "var(--fs-sm)", color: "var(--text-muted)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {clip.sourceName || clip.projectName}
      </span>

      {/* Duration + BPM */}
      <span style={{ textAlign: "right", fontVariantNumeric: "tabular-nums", fontSize: "var(--fs-sm)", color: "var(--text-muted)" }}>
        <span style={{ display: "block" }}>{clip.durationSec > 0 ? formatDuration(clip.durationSec) : "—"}</span>
        {clip.bpm > 0 && <span style={{ fontSize: "var(--fs-caption)", color: "var(--text-faint)" }}>{Math.round(clip.bpm)} BPM</span>}
      </span>

      {/* Download */}
      <span onClick={(e) => { e.stopPropagation(); onDownload(); }} style={{ display: "flex", justifyContent: "flex-end" }}>
        <button title={t.midi.downloadOne} style={{ background: "none", border: "none", cursor: "pointer", color: hover ? "var(--accent)" : "var(--text-faint)", padding: 6, borderRadius: "var(--radius-sm)", display: "flex", alignItems: "center", transition: "color 100ms" }}>
          <Icons.Download />
        </button>
      </span>
    </div>
  );
}
