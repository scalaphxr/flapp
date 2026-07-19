import React from "react";
import { Button, Card, CategoryTag, Checkbox, DropZone, Icons, Input, ProgressBar } from "@/shared/ui";
import { MidiSection } from "./MidiSection";
import { SoundTable } from "@/widgets/SoundTable/SoundTable";
import { useT } from "@/shared/i18n";
import { api, type SearchParams } from "@/shared/api/client";
import type { Interpretation, Sample } from "@/shared/api/types";
import { ALL_CATEGORIES } from "@/shared/config/categories";
import { usePlayerStore } from "@/shared/model/player";
import { useJobsStore } from "@/shared/model/jobs";
import { useSettingsStore } from "@/shared/model/settings";
import { formatBytes, formatBPM, formatDuration } from "@/shared/lib/format";
import { fileName, kindOf, onFileDrop, pickFiles, pickFolder } from "@/shared/lib/tauri";

interface QueueItem {
  path: string;
  name: string;
  kind: "zip" | "flp" | "audio" | "folder";
}

const queueIcon: Record<QueueItem["kind"], keyof typeof Icons> = {
  zip: "Zip",
  flp: "Flp",
  audio: "Audio",
  folder: "Folder",
};

export function SamplesPage() {
  const t = useT();
  const { playingId, toggle } = usePlayerStore();
  const jobs = useJobsStore();
  const { settings } = useSettingsStore();

  // ── Sidebar ────────────────────────────────────────────────────────────────
  // "" = все звуки, "midi" = MIDI-секция, любая другая строка = фильтр по категории
  const [activeCategory, setActiveCategory] = React.useState<string>("");
  const [midiCount, setMidiCount] = React.useState(0);

  // ── Library state ──────────────────────────────────────────────────────────
  const [q, setQ] = React.useState("");
  const [favOnly, setFavOnly] = React.useState(false);
  const [smart, setSmart] = React.useState("");
  const [showSmart, setShowSmart] = React.useState(false);
  const [interp, setInterp] = React.useState<Interpretation | null>(null);
  const [items, setItems] = React.useState<Sample[]>([]);
  const [total, setTotal] = React.useState(0);
  const [sortBy, setSortBy] = React.useState<string>("added");
  const [sortOrder, setSortOrder] = React.useState<"asc" | "desc">("desc");
  const [activeId, setActiveId] = React.useState<number | null>(null);
  const [active, setActive] = React.useState<Sample | null>(null);
  const [similar, setSimilar] = React.useState<Sample[]>([]);

  // ── Selection state ────────────────────────────────────────────────────────
  const [selected, setSelected] = React.useState<Set<number>>(new Set());
  const [packName, setPackName] = React.useState("");
  const [group, setGroup] = React.useState(true);
  const [donePath, setDonePath] = React.useState<string | null>(null);
  const [includeMidi, setIncludeMidi] = React.useState(false);
  const [midiGroupMode, setMidiGroupMode] = React.useState<"flat" | "by_project">("flat");
  const [midiClipsCount, setMidiClipsCount] = React.useState<number | null>(null);

  // ── Scan panel state ───────────────────────────────────────────────────────
  const [scanOpen, setScanOpen] = React.useState(false);
  const [queue, setQueue] = React.useState<QueueItem[]>([]);
  const [selectedIdx, setSelectedIdx] = React.useState<number | null>(null);
  const [drumkitsDir, setDrumkitsDir] = React.useState<string | null>(null);
  const [dragHover, setDragHover] = React.useState(false);
  const [opts, setOpts] = React.useState({
    guess: true, extra: false, deep: true, onlyFlp: false, tags: true, extractMidi: true,
  });
  const [jobId, setJobId] = React.useState<string | null>(null);

  // ── Computed ───────────────────────────────────────────────────────────────
  const activeJob = jobs.activeJob();
  const running = !!activeJob && (activeJob.type === "harvest" || activeJob.type === "export_pack");
  const building = activeJob?.type === "export_pack";
  const harvestJob = jobs.latestOfType("harvest");
  const stats = (harvestJob?.result?.stats ?? null) as
    | { uniqueFiles?: number; duplicates?: number; bytesSaved?: number }
    | null;
  const selectedSize = React.useMemo(
    () => items.filter((s) => selected.has(s.id)).reduce((sum, s) => sum + s.size, 0),
    [items, selected]
  );

  // Счётчики категорий — вычисляются локально из items
  const counts = React.useMemo(() => {
    const m: Record<string, number> = { "": items.length };
    for (const s of items) m[s.category] = (m[s.category] || 0) + 1;
    return m;
  }, [items]);

  // Фильтрованный список для таблицы (без API-запроса)
  const displayItems = React.useMemo(() => {
    if (!activeCategory || activeCategory === "midi") return items;
    return items.filter((s) => s.category === activeCategory);
  }, [items, activeCategory]);

  // ── MIDI count ─────────────────────────────────────────────────────────────
  async function loadMidiClips() {
    try {
      const { total: cnt } = await api.midiClips();
      setMidiCount(cnt);
    } catch {
      /* ignore */
    }
  }

  React.useEffect(() => {
    loadMidiClips();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ── Search ─────────────────────────────────────────────────────────────────
  const runSearch = React.useCallback(async () => {
    const params: SearchParams = {
      q: q.trim() || undefined,
      favorite: favOnly || undefined,
      sort: sortBy,
      order: sortOrder,
      limit: 500,
    };
    try {
      const res = await api.searchSamples(params);
      setItems(res.items);
      setTotal(res.total);
      setInterp(null);
    } catch {
      /* ignore */
    }
  }, [q, favOnly, sortBy, sortOrder]);

  React.useEffect(() => {
    const id = setTimeout(runSearch, 220);
    return () => clearTimeout(id);
  }, [runSearch]);

  React.useEffect(() => {
    const off = jobs.onDone((job) => {
      // Обновляем список только по завершении харвеста — не по всем джобам,
      // чтобы завершение export_pack/других операций не перезаписало состояние
      // после ручной очистки библиотеки.
      if (job.type === "harvest" && job.status === "completed") runSearch();
      if (job.type === "export_pack" && job.status === "completed") {
        setDonePath((job.result?.path as string) || null);
      }
      if (job.type === "extract_midi" && job.status === "completed") {
        loadMidiClips();
      }
    });
    return off;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ── OS drag-drop ───────────────────────────────────────────────────────────
  const addPaths = React.useCallback((paths: string[]) => {
    setQueue((prev) => {
      const seen = new Set(prev.map((item) => item.path));
      const next = [...prev];
      for (const p of paths) {
        if (!seen.has(p)) {
          next.push({ path: p, name: fileName(p), kind: kindOf(p) });
          seen.add(p);
        }
      }
      return next;
    });
  }, []);

  React.useEffect(() => {
    let dispose = () => {};
    onFileDrop(addPaths, setDragHover).then((d) => (dispose = d));
    return () => dispose();
  }, [addPaths]);

  // ── Handlers ───────────────────────────────────────────────────────────────
  function handleSort(col: string) {
    if (col === sortBy) {
      setSortOrder((o) => (o === "asc" ? "desc" : "asc"));
    } else {
      setSortBy(col);
      setSortOrder("asc");
    }
  }

  function toggleSelect(id: number) {
    setSelected((prev) => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  }

  async function runSmart() {
    if (!smart.trim()) return;
    try {
      const res = await api.smartSearch(smart, 500);
      setItems(res.items);
      setTotal(res.total);
      setInterp(res.interpretation);
    } catch {
      /* ignore */
    }
  }

  async function openDetail(id: number) {
    setActiveId(id);
    try {
      const s = await api.sample(id);
      setActive(s);
      const sim = await api.similar(id, 6);
      setSimilar(sim.items.filter((x) => x.id !== id));
    } catch {
      /* ignore */
    }
  }

  async function toggleFavorite() {
    if (!active) return;
    const next = !active.favorite;
    setActive({ ...active, favorite: next });
    setItems((list) => list.map((s) => (s.id === active.id ? { ...s, favorite: next } : s)));
    await api.setFavorite(active.id, next).catch(() => {});
  }

  async function setRating(r: number) {
    if (!active) return;
    setActive({ ...active, rating: r });
    await api.setRating(active.id, r).catch(() => {});
  }

  async function changeCategoryById(id: number, cat: string) {
    setItems((list) => list.map((s) => (s.id === id ? { ...s, category: cat } : s)));
    if (active?.id === id) setActive((a) => (a ? { ...a, category: cat } : a));
    await api.setCategory(id, cat).catch(() => {});
  }

  async function changeCategory(cat: string) {
    if (!active) return;
    await changeCategoryById(active.id, cat);
  }

  async function runHarvest() {
    if (queue.length === 0) return;
    try {
      const { jobId: id } = await api.harvest({
        inputs: queue.map((item) => item.path),
        drumkitsDir: drumkitsDir || undefined,
        guess: opts.guess,
        extraFormats: opts.extra,
        deepDedup: opts.deep,
        onlyFromFlp: opts.onlyFlp,
        generateTags: opts.tags,
      });
      setJobId(id);
    } catch (e) {
      console.error(e);
    }
    if (opts.extractMidi) {
      const outputDir = settings?.midiOutputDir ?? "";
      api.midiExtract({
        inputs: queue.map((item) => item.path),
        outputDir: outputDir || undefined,
        ignoreEmptySamplers: true,
      }).catch(console.error);
    }
  }

  async function stopHarvest() {
    if (jobId) await api.cancelJob(jobId).catch(() => {});
  }

  async function exportToFolder() {
    const ids = selected.size ? Array.from(selected) : items.map((s) => s.id);
    if (!ids.length) return;
    const dir = await pickFolder();
    if (!dir) return;
    await api.exportToFolder(ids, dir).catch(() => {});
  }

  async function buildPack() {
    if (selected.size === 0) return;
    setDonePath(null);
    await api
      .buildPack({
        name: packName.trim() || t.packs.title,
        sampleIds: Array.from(selected),
        groupByCategory: group,
        format: "zip",
        includeMidi,
        midiGroupMode,
      })
      .catch(() => {});
  }

  React.useEffect(() => {
    if (!includeMidi) { setMidiClipsCount(null); return; }
    api.midiClips().then(({ total: cnt }) => setMidiClipsCount(cnt)).catch(() => setMidiClipsCount(0));
  }, [includeMidi]);

  async function clearLibrary() {
    if (!items.length) return;
    await api.clearSamples().catch(() => {});
    setItems([]);
    setTotal(0);
    setSelected(new Set());
    setActive(null);
    setActiveId(null);
    // Подтверждаем пустое состояние от сервера (убирает возможную гонку).
    runSearch();
  }

  // ── Render ─────────────────────────────────────────────────────────────────
  return (
    <div style={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0, gap: 14, padding: 0 }}>

      {/* Top controls */}
      <div style={{ display: "flex", flexDirection: "column", gap: 10, marginBottom: 0 }}>
        <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
          {/* Import toggle button */}
          <Button
            variant="primary"
            icon={scanOpen ? <Icons.X /> : <Icons.Wave />}
            onClick={() => setScanOpen((v) => !v)}
          >
            {scanOpen ? t.samples.closeScan : t.samples.scan}
          </Button>
          {/* Wand button */}
          <Checkbox checked={favOnly} onChange={setFavOnly} label={t.library.favOnly} />
          <Button
            variant={showSmart ? "primary" : "ghost"}
            icon={<Icons.Wand />}
            onClick={() => setShowSmart((v) => !v)}
            title={t.library.smartSearch}
          />
        </div>

        {showSmart ? (
          <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
            <Input
              icon={<Icons.Wand />}
              placeholder={t.library.smartPlaceholder}
              value={smart}
              onChange={(e) => setSmart(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && runSmart()}
              style={{ flex: 1 }}
            />
            <Button variant="primary" icon={<Icons.Wand />} onClick={runSmart}>
              {t.library.smartSearch}
            </Button>
          </div>
        ) : null}

        {interp && (interp.categories?.length || interp.tags?.length || interp.minBpm || interp.maxBpm) ? (
          <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap", fontSize: "var(--fs-sm)", color: "var(--text-muted)" }}>
            <span style={{ color: "var(--text-faint)" }}>{t.library.understood}:</span>
            {interp.categories?.map((c) => <CategoryTag key={c} category={c} />)}
            {interp.tags?.map((tag) => <Pill key={tag}>#{tag}</Pill>)}
            {interp.minBpm || interp.maxBpm ? <Pill>{interp.minBpm || 0}–{interp.maxBpm || "∞"} BPM</Pill> : null}
          </div>
        ) : null}
      </div>

      {/* Scan panel */}
      {scanOpen ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 10, flexShrink: 0, marginBottom: 0 }}>
          <div style={{ display: "flex", gap: 10, height: 240, overflowX: "auto" }}>

            {/* Block 1 — Drop zone */}
            <Card padding={0} style={{ flex: 1, minWidth: 210, overflow: "hidden" }}>
              <DropZone
                title={t.harvest.dropTitle}
                subtitle={t.harvest.dropSubtitle}
                active={dragHover}
                onClick={() => pickFiles().then(addPaths)}
                style={{ height: "100%", minHeight: 0 }}
              />
            </Card>

            {/* Block 2 — Project queue */}
            <Card padding={0} style={{ flex: 1, minWidth: 210, display: "flex", flexDirection: "column" }}>
              <div style={{ flex: 1, overflowY: "auto", padding: "10px 10px 0" }}>
                {queue.length === 0 ? (
                  <div style={{ padding: "24px 0", textAlign: "center", color: "var(--text-faint)", fontSize: "var(--fs-sm)" }}>
                    {t.harvest.queueEmpty}
                  </div>
                ) : (
                  queue.map((item, i) => {
                    const QIcon = Icons[queueIcon[item.kind]];
                    const sel = selectedIdx === i;
                    return (
                      <div
                        key={item.path}
                        onClick={() => setSelectedIdx(sel ? null : i)}
                        style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 8px", borderRadius: "var(--radius-md)", cursor: "pointer", background: sel ? "var(--accent-soft)" : "transparent", color: sel ? "var(--accent)" : "var(--text-body)" }}
                      >
                        <span style={{ display: "inline-flex", color: sel ? "var(--accent)" : "var(--text-faint)" }}><QIcon /></span>
                        <span style={{ fontSize: "var(--fs-sm)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{item.name}</span>
                      </div>
                    );
                  })
                )}
              </div>
              <div style={{ display: "flex", gap: 6, padding: "8px 10px", borderTop: "1px solid var(--border-soft)", flexShrink: 0 }}>
                <Button variant="ghost" size="sm" icon={<Icons.X />} disabled={selectedIdx === null} onClick={() => { if (selectedIdx === null) return; setQueue((q) => q.filter((_, i) => i !== selectedIdx)); setSelectedIdx(null); }}>
                  {t.harvest.removeSelected}
                </Button>
                <Button variant="ghost" size="sm" icon={<Icons.Trash />} disabled={queue.length === 0} onClick={() => { setQueue([]); setSelectedIdx(null); }}>
                  {t.harvest.clearQueue}
                </Button>
              </div>
            </Card>

            {/* Block 3 — Options */}
            <Card padding={0} style={{ flex: 1, minWidth: 210, display: "flex", flexDirection: "column", padding: "14px 16px", gap: 10, overflowY: "auto" }}>
              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "8px 20px" }}>
                <Checkbox checked={opts.guess} onChange={(v) => setOpts({ ...opts, guess: v })} label={t.harvest.optGuess} />
                <Checkbox checked={opts.extra} onChange={(v) => setOpts({ ...opts, extra: v })} label={t.harvest.optExtra} />
                <Checkbox checked={opts.deep} onChange={(v) => setOpts({ ...opts, deep: v })} label={t.harvest.optDeep} />
                <Checkbox checked={opts.onlyFlp} onChange={(v) => setOpts({ ...opts, onlyFlp: v })} label={t.harvest.optOnlyFlp} />
                <Checkbox checked={opts.tags} onChange={(v) => setOpts({ ...opts, tags: v })} label={t.harvest.optTags} />
                <Checkbox checked={opts.extractMidi} onChange={(v) => setOpts({ ...opts, extractMidi: v })} label={t.midi.extract} />
              </div>

              <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <Button variant="secondary" size="sm" icon={<Icons.Folder />} onClick={() => pickFolder().then((d) => { if (d) setDrumkitsDir(d); })}>
                  {t.harvest.drumkitsPick}
                </Button>
                <span className="mono" style={{ fontSize: 11, color: drumkitsDir ? "var(--text-muted)" : "var(--text-faint)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {drumkitsDir || t.harvest.drumkitsLabel}
                </span>
              </div>
            </Card>
          </div>

          {/* Run bar — вынесена из карточек, поэтому видна при любом размере окна */}
          {queue.some(item => item.kind === "flp") && !drumkitsDir && (
            <div style={{
              padding: "7px 10px",
              borderRadius: 0,
              background: "transparent",
              border: "1px solid var(--warning)",
              fontSize: "var(--fs-caption)",
              color: "var(--warning)",
              lineHeight: 1.4,
            }}>
              {t.harvest.flpNoDrumkitsWarning}
            </div>
          )}

          {running ? (
            <Button variant="ghost" full icon={<Icons.Stop />} onClick={stopHarvest}>{t.common.stop}</Button>
          ) : (
            <Button variant="primary" full icon={<Icons.Search />} disabled={queue.length === 0} onClick={runHarvest}>
              {t.harvest.run}
            </Button>
          )}
        </div>
      ) : null}

      {/* Progress */}
      {running && activeJob ? (
        <ProgressBar
          value={Math.round((activeJob.progress || 0) * 100)}
          caption={[activeJob.stage, activeJob.detail].filter(Boolean).join(" · ")}
          percent
        />
      ) : null}

      {/* Sidebar + content */}
      <div style={{ display: "flex", flex: 1, minHeight: 0 }}>

        {/* LEFT SIDEBAR */}
        <div style={{
          width: 180,
          flexShrink: 0,
          background: "var(--surface-1)",
          border: "none",
          borderRight: "1px solid var(--border-soft)",
          borderRadius: 0,
          padding: 0,
          overflowY: "auto",
          display: "flex",
          flexDirection: "column",
          boxShadow: "none",
          alignSelf: "stretch",
        }}>
          <SidebarItem
            label={t.common.all}
            count={counts[""] ?? 0}
            active={activeCategory === ""}
            onClick={() => setActiveCategory("")}
          />
          {ALL_CATEGORIES.map((c) => (
            <SidebarItem
              key={c}
              label={c}
              count={counts[c] ?? 0}
              active={activeCategory === c}
              onClick={() => setActiveCategory(c)}
            />
          ))}

          <div style={{ height: 1, background: "var(--border-soft)", margin: "8px 12px" }} />

          <SidebarItem
            label={t.midi.title}
            count={midiCount}
            active={activeCategory === "midi"}
            onClick={() => setActiveCategory("midi")}
            prefix="🎹"
          />
        </div>

        {/* MAIN CONTENT */}
        <div style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", gap: 14, overflow: "hidden" }}>
          {activeCategory === "midi" ? (
            <MidiSection />
          ) : (
            <>
              {/* Table + detail panel */}
              <div style={{ display: "flex", gap: 16, flex: 1, minHeight: 0 }}>
                <Card padding={0} style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", overflow: "hidden", paddingTop: 14 }}>
                  <div style={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column", padding: "0 var(--space-3) var(--space-3)" }}>
                    <div style={{ marginBottom: "var(--space-2)" }}>
                      <Input
                        icon={<Icons.Search />}
                        placeholder={t.common.search}
                        value={q}
                        onChange={(e) => setQ(e.target.value)}
                        style={{ width: "100%" }}
                      />
                    </div>
                    <SoundTable
                      samples={displayItems}
                      playingId={playingId}
                      onPlay={toggle}
                      selectable
                      selected={selected}
                      onToggleSelect={toggleSelect}
                      onRowClick={openDetail}
                      onCategoryChange={changeCategoryById}
                      activeId={activeId}
                      emptyText={total === 0 ? t.samples.noSounds : t.common.nothingFound}
                      showWaveform
                      sortBy={sortBy}
                      sortOrder={sortOrder}
                      onSort={handleSort}
                    />
                  </div>
                </Card>

                {active ? (
                  <DetailPanel
                    active={active}
                    similar={similar}
                    onClose={() => { setActive(null); setActiveId(null); }}
                    onFavorite={toggleFavorite}
                    onRating={setRating}
                    onCategory={changeCategory}
                    onDetail={openDetail}
                  />
                ) : null}
              </div>

              {selected.size > 0 ? (
                <Card padding={0} style={{ flexShrink: 0 }}>
                  <div style={{ padding: "10px 16px", display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
                    <span style={{ fontSize: "var(--fs-sm)", color: "var(--accent)", fontWeight: "var(--fw-semibold)" as any }}>
                      {selected.size} {t.samples.selCount} · {formatBytes(selectedSize)}
                    </span>
                    <Button variant="ghost" size="sm" onClick={() => setSelected(new Set())}>
                      {t.samples.clearSel}
                    </Button>
                    <Button variant="ghost" size="sm" onClick={() => setSelected(new Set(items.map((s) => s.id)))}>
                      {t.samples.selectAll}
                    </Button>
                    <div style={{ flex: 1 }} />
                    <Button variant="secondary" size="sm" icon={<Icons.Save />} onClick={exportToFolder}>
                      {t.samples.exportFolder}
                    </Button>
                    <Input
                      placeholder={t.packs.namePlaceholder}
                      value={packName}
                      onChange={(e) => setPackName(e.target.value)}
                      style={{ width: 180 }}
                    />
                    <Checkbox checked={group} onChange={setGroup} label={t.packs.groupByCategory} />
                    <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                      <Checkbox
                        checked={includeMidi}
                        onChange={setIncludeMidi}
                        label={t.midi.includeMidi}
                      />
                      {includeMidi && midiClipsCount === 0 && (
                        <span
                          title={t.midi.midiNoClipsTooltip}
                          style={{ fontSize: "var(--fs-caption)", color: "var(--text-faint)", cursor: "help" }}
                        >
                          ⚠
                        </span>
                      )}
                      {includeMidi && (
                        <select
                          value={midiGroupMode}
                          onChange={(e) => setMidiGroupMode(e.target.value as "flat" | "by_project")}
                          style={{
                            height: 26, padding: "0 6px",
                            background: "var(--surface-input)", color: "var(--text-body)",
                            border: "1px solid var(--border-medium)", borderRadius: "var(--radius-sm)",
                            fontFamily: "var(--font-sans)", fontSize: "var(--fs-caption)",
                            cursor: "pointer", outline: "none",
                          }}
                        >
                          <option value="flat">{t.midi.midiGroupFlat}</option>
                          <option value="by_project">{t.midi.midiGroupByProject}</option>
                        </select>
                      )}
                    </div>
                    <Button variant="primary" size="sm" icon={<Icons.Box />} disabled={building} onClick={buildPack}>
                      {t.samples.buildZip}
                    </Button>
                  </div>
                </Card>
              ) : (
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "4px 12px", flexShrink: 0 }}>
                  <span style={{ fontSize: "var(--fs-sm)", color: "var(--text-faint)" }}>
                    {total} {t.harvest.statSounds}
                    {stats
                      ? ` · ${stats.duplicates ?? 0} ${t.harvest.statDupes} · ${formatBytes(stats.bytesSaved ?? 0)} ${t.harvest.statSaved}`
                      : ""}
                  </span>
                  <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
                    <Button variant="ghost" size="sm" disabled={total === 0} onClick={() => setSelected(new Set(displayItems.map((s) => s.id)))}>
                      {t.samples.selectAll}
                    </Button>
                    <Button variant="ghost" size="sm" icon={<Icons.Trash />} disabled={total === 0 || running} onClick={clearLibrary}>
                      {t.samples.clearLib}
                    </Button>
                  </div>
                </div>
              )}

              {donePath ? (
                <div style={{ padding: "10px 14px", borderRadius: "var(--radius-md)", background: "color-mix(in srgb, var(--positive) 14%, transparent)", flexShrink: 0 }}>
                  <span style={{ fontSize: "var(--fs-sm)", color: "var(--positive)", fontWeight: "var(--fw-semibold)" as any }}>{t.packs.done}</span>
                  <span className="mono" style={{ fontSize: 11, color: "var(--text-muted)", marginLeft: 12, wordBreak: "break-all" }}>{donePath}</span>
                </div>
              ) : null}
            </>
          )}
        </div>
      </div>
    </div>
  );
}

// ── Sidebar item ───────────────────────────────────────────────────────────────

function SidebarItem({
  label,
  count,
  active,
  onClick,
  prefix,
}: {
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
  prefix?: string;
}) {
  const [hovered, setHovered] = React.useState(false);

  return (
    <button
      onClick={onClick}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        display: "flex",
        alignItems: "center",
        gap: 6,
        padding: "6px 12px 6px",
        paddingLeft: active ? 10 : 12,
        border: "none",
        borderLeft: active ? "2px solid var(--accent)" : "2px solid transparent",
        background: active ? "var(--accent-soft)" : hovered ? "var(--row-hover)" : "transparent",
        borderRadius: 0,
        width: "100%",
        textAlign: "left",
        cursor: "pointer",
        transition: "background 120ms",
        minWidth: 0,
      }}
    >
      {prefix ? <span style={{ fontSize: 12 }}>{prefix}</span> : null}
      <span style={{
        flex: 1,
        minWidth: 0,
        overflow: "hidden",
        textOverflow: "ellipsis",
        whiteSpace: "nowrap",
        fontSize: "var(--fs-sm)",
        fontWeight: active ? "var(--fw-semibold)" : "var(--fw-normal)",
        color: active ? "var(--accent)" : "var(--text-body)",
      }}>
        {label}
      </span>
      <span style={{
        fontSize: "var(--fs-caption)",
        color: active ? "var(--accent)" : "var(--text-faint)",
        fontVariantNumeric: "tabular-nums",
        flexShrink: 0,
      }}>
        {count}
      </span>
    </button>
  );
}

// ── Detail panel ───────────────────────────────────────────────────────────────

function DetailPanel({
  active,
  similar,
  onClose,
  onFavorite,
  onRating,
  onCategory,
  onDetail,
}: {
  active: Sample;
  similar: Sample[];
  onClose: () => void;
  onFavorite: () => void;
  onRating: (v: number) => void;
  onCategory: (cat: string) => void;
  onDetail: (id: number) => void;
}) {
  const t = useT();
  return (
    <Card style={{ width: 320, flexShrink: 0, display: "flex", flexDirection: "column", gap: 16, overflowY: "auto" }}>
      <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: 10 }}>
        <div style={{ minWidth: 0 }}>
          <div style={{ fontSize: "var(--fs-body)", fontWeight: "var(--fw-semibold)" as any, color: "var(--text-strong)", wordBreak: "break-word" }}>
            {active.name}
          </div>
          <div className="mono" style={{ fontSize: 11, color: "var(--text-faint)", marginTop: 4, wordBreak: "break-all" }}>
            {active.sourceLabel || active.path}
          </div>
        </div>
        <button onClick={onClose} style={{ border: "none", background: "transparent", color: "var(--text-faint)", cursor: "pointer", padding: 4 }}>
          <Icons.X />
        </button>
      </div>

      <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
        <div style={{ position: "relative", display: "inline-flex", alignItems: "center" }}>
          <CategoryTag category={active.category} />
          <select
            value={active.category}
            onChange={(e) => onCategory(e.target.value)}
            style={{ position: "absolute", inset: 0, opacity: 0, cursor: "pointer", width: "100%" }}
          >
            {ALL_CATEGORIES.map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
        </div>
        <FavoriteButton on={active.favorite} onClick={onFavorite} label={active.favorite ? t.library.unfavorite : t.library.favorite} />
      </div>

      <StarRating value={active.rating} onChange={onRating} />

      <div style={{ display: "grid", gridTemplateColumns: "auto 1fr", rowGap: 8, columnGap: 14, fontSize: "var(--fs-sm)" }}>
        <Meta label="BPM" value={formatBPM(active.bpm)} />
        <Meta label="Key" value={active.keyName || "—"} />
        <Meta label={t.harvest.colSize} value={formatBytes(active.size)} />
        <Meta label="Dur." value={formatDuration(active.features?.durationSeconds || 0)} />
      </div>

      {active.tags && active.tags.length ? (
        <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
          {active.tags.map((tag) => <Pill key={tag}>#{tag}</Pill>)}
        </div>
      ) : null}

      {similar.length ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          <span className="ds-section-label">Similar</span>
          {similar.map((s) => (
            <div
              key={s.id}
              onClick={() => onDetail(s.id)}
              style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 8px", borderRadius: "var(--radius-md)", cursor: "pointer", background: "var(--surface-3)" }}
            >
              <span style={{ display: "inline-flex", color: "var(--text-faint)" }}><Icons.Music /></span>
              <span style={{ fontSize: "var(--fs-sm)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{s.name}</span>
            </div>
          ))}
        </div>
      ) : null}
    </Card>
  );
}

// ── Small helpers ──────────────────────────────────────────────────────────────

function Pill({ children }: { children: React.ReactNode }) {
  return (
    <span style={{ padding: "3px 10px", borderRadius: "var(--radius-pill)", background: "var(--surface-3)", color: "var(--text-muted)", fontSize: "var(--fs-caption)", whiteSpace: "nowrap" }}>
      {children}
    </span>
  );
}

function Meta({ label, value }: { label: string; value: string }) {
  return (
    <>
      <span style={{ color: "var(--text-faint)" }}>{label}</span>
      <span className="mono" style={{ color: "var(--text-body)", textAlign: "right" }}>{value}</span>
    </>
  );
}

function FavoriteButton({ on, onClick, label }: { on: boolean; onClick: () => void; label: string }) {
  return (
    <button
      onClick={onClick}
      title={label}
      style={{ display: "inline-flex", alignItems: "center", gap: 6, padding: "5px 11px", borderRadius: "var(--radius-pill)", cursor: "pointer", border: "1px solid", borderColor: on ? "transparent" : "var(--border-medium)", background: on ? "var(--accent-soft)" : "transparent", color: on ? "var(--accent)" : "var(--text-muted)", fontSize: "var(--fs-sm)", transition: "var(--transition-base)" }}
    >
      <Icons.Heart />
    </button>
  );
}

function StarRating({ value, onChange }: { value: number; onChange: (v: number) => void }) {
  const [hover, setHover] = React.useState(0);
  return (
    <div style={{ display: "flex", gap: 4 }} onMouseLeave={() => setHover(0)}>
      {[1, 2, 3, 4, 5].map((n) => {
        const lit = (hover || value) >= n;
        return (
          <button
            key={n}
            onMouseEnter={() => setHover(n)}
            onClick={() => onChange(value === n ? 0 : n)}
            style={{ border: "none", background: "transparent", cursor: "pointer", padding: 2, color: lit ? "var(--warning)" : "var(--text-faint)" }}
          >
            <Icons.Star fill={lit ? "currentColor" : "none"} />
          </button>
        );
      })}
    </div>
  );
}
