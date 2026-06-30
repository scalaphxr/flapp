import React from "react";
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from "recharts";
import { Card, CategoryTag } from "@/shared/ui";
import { useT } from "@/shared/i18n";
import { api } from "@/shared/api/client";
import type { Analytics } from "@/shared/api/types";
import { groupHex, groupOf } from "@/shared/config/categories";
import { useJobsStore } from "@/shared/model/jobs";
import { useSettingsStore } from "@/shared/model/settings";
import { formatBytes } from "@/shared/lib/format";

// Читает текущее значение CSS-переменной с documentElement.
// При смене темы компонент перерендерится (подписка через _theme) и
// recharts получит актуальные цвета.
const cv = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim();

export function AnalyticsPage() {
  const t = useT();
  const jobs = useJobsStore();
  const _theme = useSettingsStore((s) => s.settings?.theme);
  const isFl = _theme === "fl";
  const [data, setData] = React.useState<Analytics | null>(null);

  const load = React.useCallback(async () => {
    try {
      setData(await api.analytics());
    } catch {
      /* ignore */
    }
  }, []);

  React.useEffect(() => {
    load();
    const off = jobs.onDone((job) => {
      if (job.status === "completed") load();
    });
    return off;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Last harvest job — contains the authoritative per-scan stats.
  const lastHarvest = jobs.latestOfType("harvest");
  const scanStats = (lastHarvest?.result?.stats ?? null) as {
    uniqueFiles?: number;
    duplicates?: number;
    bytesSaved?: number;
  } | null;
  const hasScan = !!(lastHarvest && lastHarvest.status === "completed");

  const empty = !data || data.samples === 0;

  const catData = (data?.byCategory ?? [])
    .filter((c) => c.count > 0)
    .map((c) => ({ name: c.category, count: c.count, hex: groupHex(groupOf(c.category)) }));

  const bpmData = (data?.topBpm ?? []).map((b) => ({ name: `${b.bpm}`, count: b.count }));

  // Format the scan timestamp.
  const scanTime = hasScan && lastHarvest
    ? new Date(lastHarvest.updatedAt * 1000).toLocaleString("ru-RU", {
        day: "2-digit", month: "2-digit", year: "numeric",
        hour: "2-digit", minute: "2-digit",
      })
    : null;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: isFl ? 14 : "var(--space-6)", flex: 1, minHeight: 0, overflowY: "auto", padding: isFl ? "18px 20px" : 0 }}>
      {!isFl && (
        <div>
          <h1 className="page-title">{t.analytics.title}</h1>
          <p className="page-desc">{t.analytics.desc}</p>
        </div>
      )}

      {/* Last scan results */}
      {hasScan ? (
        isFl ? (
          <FlPanel label="LAST SCAN">
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 12 }}>
              <FlPanelStat label="ADDED" value={String(scanStats?.uniqueFiles ?? "—")} />
              <FlPanelStat label="DUPES SKIPPED" value={String(scanStats?.duplicates ?? 0)} />
              <FlPanelStat label="SAVED" value={formatBytes(scanStats?.bytesSaved ?? 0)} />
            </div>
          </FlPanel>
        ) : (
          <Card>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 16, flexWrap: "wrap", gap: 8 }}>
              <span className="ds-section-label">Last scan</span>
              {scanTime ? <span style={{ fontSize: "var(--fs-caption)", color: "var(--text-faint)", fontFamily: "var(--font-mono)" }}>{scanTime}</span> : null}
            </div>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 12 }}>
              <ScanStat label="Sounds added" value={scanStats?.uniqueFiles ?? "—"} accent />
              <ScanStat label="Dupes skipped" value={scanStats?.duplicates ?? 0} sub="(already in library)" />
              <ScanStat label="Space saved" value={formatBytes(scanStats?.bytesSaved ?? 0)} sub="(deduplication)" />
            </div>
          </Card>
        )
      ) : null}

      {/* Library overview */}
      {empty ? (
        isFl ? (
          <div style={{ padding: "60px 24px", textAlign: "center", color: "var(--ink-on-work-dim)", font: "400 13px var(--font-sans)" }}>
            {t.analytics.empty}
          </div>
        ) : (
          <Card style={{ padding: "60px 24px", textAlign: "center", color: "var(--text-faint)" }}>
            {t.analytics.empty}
          </Card>
        )
      ) : (
        <>
          {/* Top stats */}
          {isFl ? (
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 14 }}>
              <LcdStat label="SOUNDS" value={String(data!.samples)} color="amber" />
              <LcdStat label="TOTAL ON DISK" value={formatBytes(data!.bytesTotal)} color="green" />
              <LcdStat
                label="TOP USES"
                value={data!.topUsed && data!.topUsed.length > 0 ? `${data!.topUsed[0].used}×` : "—"}
                color="amber"
              />
            </div>
          ) : (
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 14 }}>
              <Stat label={t.analytics.samples} value={data!.samples} accent />
              <Stat label={t.analytics.total} value={formatBytes(data!.bytesTotal)} />
              <Stat label="Avg. uses" value={data!.topUsed && data!.topUsed.length > 0 ? `${data!.topUsed[0].used}×` : "—"} />
            </div>
          )}

          {/* Category bar chart */}
          {isFl ? (
            <FlPanel label={t.analytics.byCategory}>
              <div style={{ height: 220, marginTop: 10 }}>
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={catData} margin={{ top: 4, right: 8, bottom: 44, left: 0 }}>
                    <XAxis dataKey="name" angle={-38} textAnchor="end" interval={0} height={70}
                      tick={{ fill: cv("--ink-on-work-dim"), fontSize: 10, fontFamily: cv("--font-sans") }}
                      stroke={cv("--line-work")} />
                    <YAxis allowDecimals={false} tick={{ fill: cv("--ink-on-work-dim"), fontSize: 10 }} stroke={cv("--line-work")} width={32} />
                    <Tooltip cursor={{ fill: "rgba(255,138,60,.08)" }}
                      contentStyle={{ background: cv("--work-2"), border: `1px solid ${cv("--line-work")}`, borderRadius: 6, fontSize: 11 }}
                      labelStyle={{ color: cv("--ink-on-work") }}
                      itemStyle={{ color: cv("--lcd-amber") }}
                      formatter={(v: number) => [`${v}`, ""]} />
                    <Bar dataKey="count" radius={[3, 3, 0, 0]}>
                      {catData.map((d, i) => <Cell key={i} fill={d.hex} />)}
                    </Bar>
                  </BarChart>
                </ResponsiveContainer>
              </div>
            </FlPanel>
          ) : (
            <Card>
              <span className="ds-section-label">{t.analytics.byCategory}</span>
              <div style={{ height: 260, marginTop: 16 }}>
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={catData} margin={{ top: 4, right: 8, bottom: 44, left: 0 }}>
                    <XAxis dataKey="name" angle={-38} textAnchor="end" interval={0} height={70}
                      tick={{ fill: cv("--text-muted"), fontSize: 11 }} stroke={cv("--border-medium")} />
                    <YAxis allowDecimals={false} tick={{ fill: cv("--text-faint"), fontSize: 11 }} stroke={cv("--border-medium")} width={36} />
                    <Tooltip cursor={{ fill: cv("--accent-softer") }}
                      contentStyle={{ background: cv("--surface-2"), border: `1px solid ${cv("--border-medium")}`, borderRadius: 10, fontSize: 12 }}
                      labelStyle={{ color: cv("--text-strong") }} itemStyle={{ color: cv("--text-body") }}
                      formatter={(v: number) => [`${v} sounds`, ""]} />
                    <Bar dataKey="count" radius={[6, 6, 0, 0]}>
                      {catData.map((d, i) => <Cell key={i} fill={d.hex} />)}
                    </Bar>
                  </BarChart>
                </ResponsiveContainer>
              </div>
            </Card>
          )}

          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: isFl ? 14 : 16 }}>
            {/* Most used */}
            {isFl ? (
              <FlPanel label={t.analytics.topUsed}>
                <div style={{ display: "flex", flexDirection: "column", gap: 1, marginTop: 8 }}>
                  {(data!.topUsed ?? []).length === 0 ? <Muted /> : null}
                  {(data!.topUsed ?? []).map((s) => (
                    <div key={s.id} style={{ display: "flex", alignItems: "center", gap: 9, padding: "6px 0", borderBottom: "1px solid var(--line-work)" }}>
                      <span style={{ width: 8, height: 8, borderRadius: "50%", flexShrink: 0, background: groupHex(groupOf(s.cat)), boxShadow: `0 0 5px ${groupHex(groupOf(s.cat))}` }} />
                      <span style={{ flex: 1, minWidth: 0, font: "500 12.5px var(--font-sans)", color: "var(--ink-on-work)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                        {s.name}
                      </span>
                      <span style={{ font: "400 12px var(--font-mono)", color: "var(--lcd-amber)" }}>{s.used}×</span>
                    </div>
                  ))}
                </div>
              </FlPanel>
            ) : (
              <Card>
                <span className="ds-section-label">{t.analytics.topUsed}</span>
                <div style={{ marginTop: 14, display: "flex", flexDirection: "column", gap: 2 }}>
                  {(data!.topUsed ?? []).length === 0 ? <Muted /> : null}
                  {(data!.topUsed ?? []).map((s) => (
                    <div key={s.id} style={{ display: "flex", alignItems: "center", gap: 10, padding: "7px 6px", borderRadius: "var(--radius-md)" }}>
                      <CategoryTag category={s.cat} />
                      <span style={{ flex: 1, minWidth: 0, fontSize: "var(--fs-sm)", color: "var(--text-body)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{s.name}</span>
                      <span className="mono" style={{ fontSize: "var(--fs-sm)", color: "var(--accent)", fontWeight: "var(--fw-semibold)" as any }}>{s.used}×</span>
                    </div>
                  ))}
                </div>
              </Card>
            )}

            {/* BPM chart */}
            {isFl ? (
              <FlPanel label={t.analytics.topBpm}>
                {bpmData.length ? (
                  <div style={{ display: "flex", flexDirection: "column", gap: 7, marginTop: 10 }}>
                    {bpmData.slice(0, 8).map((b) => {
                      const maxCount = Math.max(...bpmData.map((x) => x.count), 1);
                      const pct = (b.count / maxCount) * 100;
                      return (
                        <div key={b.name} style={{ display: "flex", alignItems: "center", gap: 10 }}>
                          <span style={{ width: 36, textAlign: "right", font: "400 11px var(--font-mono)", color: "var(--ink-on-work-dim)", flexShrink: 0 }}>{b.name}</span>
                          <div style={{ flex: 1, height: 10, borderRadius: 3, background: "var(--groove)", overflow: "hidden" }}>
                            <div style={{ width: `${pct}%`, height: "100%", borderRadius: 3, background: "var(--lcd-green)", boxShadow: "0 0 6px rgba(141,255,106,.4)" }} />
                          </div>
                          <span style={{ width: 24, font: "400 11px var(--font-mono)", color: "var(--lcd-amber)", flexShrink: 0 }}>{b.count}</span>
                        </div>
                      );
                    })}
                  </div>
                ) : <Muted />}
              </FlPanel>
            ) : (
              <Card>
                <span className="ds-section-label">{t.analytics.topBpm}</span>
                <div style={{ height: 220, marginTop: 14 }}>
                  {bpmData.length ? (
                    <ResponsiveContainer width="100%" height="100%">
                      <BarChart data={bpmData} margin={{ top: 4, right: 8, bottom: 4, left: 0 }}>
                        <XAxis dataKey="name" tick={{ fill: cv("--text-muted"), fontSize: 11 }} stroke={cv("--border-medium")} />
                        <YAxis allowDecimals={false} tick={{ fill: cv("--text-faint"), fontSize: 11 }} stroke={cv("--border-medium")} width={36} />
                        <Tooltip cursor={{ fill: cv("--accent-softer") }}
                          contentStyle={{ background: cv("--surface-2"), border: `1px solid ${cv("--border-medium")}`, borderRadius: 10, fontSize: 12 }}
                          labelStyle={{ color: cv("--text-strong") }} itemStyle={{ color: cv("--text-body") }}
                          formatter={(v: number) => [`${v} sounds`, ""]} />
                        <Bar dataKey="count" fill="var(--accent)" radius={[6, 6, 0, 0]} />
                      </BarChart>
                    </ResponsiveContainer>
                  ) : <Muted />}
                </div>
              </Card>
            )}
          </div>

          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: isFl ? 14 : 16 }}>
            {/* Top keys */}
            {isFl ? (
              <FlPanel label={t.analytics.topKeys}>
                <div style={{ marginTop: 10, display: "flex", flexWrap: "wrap", gap: 6 }}>
                  {(data!.topKeys ?? []).length === 0 ? <Muted /> : null}
                  {(data!.topKeys ?? []).map((k) => <FlChip key={k.key} label={k.key} count={k.count} />)}
                </div>
              </FlPanel>
            ) : (
              <Card>
                <span className="ds-section-label">{t.analytics.topKeys}</span>
                <div style={{ marginTop: 14, display: "flex", flexWrap: "wrap", gap: 8 }}>
                  {(data!.topKeys ?? []).length === 0 ? <Muted /> : null}
                  {(data!.topKeys ?? []).map((k) => <Chip key={k.key} label={k.key} count={k.count} />)}
                </div>
              </Card>
            )}

            {/* Top tags */}
            {isFl ? (
              <FlPanel label={t.analytics.topTags}>
                <div style={{ marginTop: 10, display: "flex", flexWrap: "wrap", gap: 6 }}>
                  {(data!.topTags ?? []).length === 0 ? <Muted /> : null}
                  {(data!.topTags ?? []).map((tag) => <FlChip key={tag.tag} label={`#${tag.tag}`} count={tag.count} />)}
                </div>
              </FlPanel>
            ) : (
              <Card>
                <span className="ds-section-label">{t.analytics.topTags}</span>
                <div style={{ marginTop: 14, display: "flex", flexWrap: "wrap", gap: 8 }}>
                  {(data!.topTags ?? []).length === 0 ? <Muted /> : null}
                  {(data!.topTags ?? []).map((tag) => <Chip key={tag.tag} label={`#${tag.tag}`} count={tag.count} />)}
                </div>
              </Card>
            )}
          </div>

          {/* Category size breakdown */}
          {data!.byCategory && data!.byCategory.some((c) => c.bytes > 0) ? (
            isFl ? (
              <FlPanel label="SIZE BY CATEGORY">
                <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 10 }}>
                  {data!.byCategory.filter((c) => c.bytes > 0).sort((a, b) => b.bytes - a.bytes).map((c) => {
                    const pct = data!.bytesTotal > 0 ? (c.bytes / data!.bytesTotal) * 100 : 0;
                    return (
                      <div key={c.category} style={{ display: "flex", alignItems: "center", gap: 10 }}>
                        <span style={{ width: 8, height: 8, borderRadius: "50%", flexShrink: 0, background: groupHex(groupOf(c.category)) }} />
                        <span style={{ width: 72, font: "500 11.5px var(--font-sans)", color: "var(--ink-on-work-dim)", flexShrink: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{c.category}</span>
                        <div style={{ flex: 1, height: 6, borderRadius: 3, background: "var(--groove)", overflow: "hidden" }}>
                          <div style={{ width: `${pct}%`, height: "100%", borderRadius: 3, background: groupHex(groupOf(c.category)) }} />
                        </div>
                        <span style={{ width: 60, font: "400 10px var(--font-mono)", color: "var(--ink-on-work-dim)", textAlign: "right", flexShrink: 0 }}>{formatBytes(c.bytes)}</span>
                        <span style={{ width: 28, font: "400 10px var(--font-mono)", color: "var(--lcd-amber)", textAlign: "right", flexShrink: 0 }}>{c.count}</span>
                      </div>
                    );
                  })}
                </div>
              </FlPanel>
            ) : (
              <Card>
                <span className="ds-section-label">{t.analytics.byCategory}</span>
                <div style={{ marginTop: 14, display: "flex", flexDirection: "column", gap: 6 }}>
                  {data!.byCategory.filter((c) => c.bytes > 0).sort((a, b) => b.bytes - a.bytes).map((c) => {
                    const pct = data!.bytesTotal > 0 ? (c.bytes / data!.bytesTotal) * 100 : 0;
                    return (
                      <div key={c.category} style={{ display: "flex", alignItems: "center", gap: 12 }}>
                        <div style={{ width: 100, flexShrink: 0 }}><CategoryTag category={c.category} /></div>
                        <div style={{ flex: 1, height: 6, borderRadius: 3, background: "var(--surface-3)", overflow: "hidden" }}>
                          <div style={{ width: `${pct}%`, height: "100%", borderRadius: 3, background: groupHex(groupOf(c.category)) }} />
                        </div>
                        <span className="mono" style={{ fontSize: "var(--fs-caption)", color: "var(--text-muted)", width: 64, textAlign: "right", flexShrink: 0 }}>{formatBytes(c.bytes)}</span>
                        <span className="mono" style={{ fontSize: "var(--fs-caption)", color: "var(--text-faint)", width: 36, textAlign: "right", flexShrink: 0 }}>{c.count}</span>
                      </div>
                    );
                  })}
                </div>
              </Card>
            )
          ) : null}
        </>
      )}
    </div>
  );
}

// ── FL rack panel container ────────────────────────────────────────────────────

function FlPanel({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{
      background: "linear-gradient(var(--work-2), var(--work-3))",
      border: "1px solid var(--line-work)",
      borderRadius: 9,
      padding: "14px 16px",
      boxShadow: "inset 0 0 0 1px rgba(255,255,255,.04)",
    }}>
      <div style={{ font: "700 10px var(--font-sans)", letterSpacing: "1.4px", color: "var(--ink-on-work-dim)", marginBottom: 4, textTransform: "uppercase" }}>
        {label}
      </div>
      {children}
    </div>
  );
}

function FlPanelStat({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <span style={{ font: "700 10px var(--font-sans)", letterSpacing: "1.2px", color: "var(--ink-on-work-dim)", textTransform: "uppercase" }}>{label}</span>
      <span style={{ font: "400 22px var(--font-mono)", color: "var(--lcd-amber)", textShadow: "0 0 8px rgba(255,181,91,.4)" }}>{value}</span>
    </div>
  );
}

function FlChip({ label, count }: { label: string; count: number }) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, padding: "4px 10px", borderRadius: 5, background: "var(--work-3)", border: "1px solid var(--line-work)", font: "500 12px var(--font-sans)", color: "var(--ink-on-work)" }}>
      {label}
      <span style={{ font: "400 11px var(--font-mono)", color: "var(--ink-on-work-dim)" }}>{count}</span>
    </span>
  );
}

function Stat({ label, value, accent }: { label: string; value: React.ReactNode; accent?: boolean }) {
  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <span style={{ fontSize: "var(--fs-caption)", color: "var(--text-faint)", textTransform: "uppercase", letterSpacing: "var(--ls-label)" }}>
        {label}
      </span>
      <span style={{ fontSize: "var(--fs-h1)", fontWeight: "var(--fw-bold)" as any, color: accent ? "var(--accent)" : "var(--text-strong)", letterSpacing: "var(--ls-tight)" }}>
        {value}
      </span>
    </Card>
  );
}

function ScanStat({ label, value, accent, sub }: { label: string; value: React.ReactNode; accent?: boolean; sub?: string }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <span style={{ fontSize: "var(--fs-caption)", color: "var(--text-faint)", textTransform: "uppercase", letterSpacing: "var(--ls-label)" }}>
        {label}
      </span>
      <span style={{ fontSize: "var(--fs-h2)", fontWeight: "var(--fw-bold)" as any, color: accent ? "var(--accent)" : "var(--text-strong)" }}>
        {value}
      </span>
      {sub ? <span style={{ fontSize: "var(--fs-caption)", color: "var(--text-faint)" }}>{sub}</span> : null}
    </div>
  );
}

function Chip({ label, count }: { label: string; count: number }) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 8, padding: "6px 12px", borderRadius: "var(--radius-pill)", background: "var(--surface-3)", fontSize: "var(--fs-sm)", color: "var(--text-body)" }}>
      {label}
      <span className="mono" style={{ color: "var(--text-faint)" }}>{count}</span>
    </span>
  );
}

function Muted() {
  const t = useT();
  return <span style={{ fontSize: "var(--fs-sm)", color: "var(--text-faint)" }}>{t.common.none}</span>;
}

function LcdStat({ label, value, color }: { label: string; value: string; color: "green" | "amber" }) {
  const lcdColor = color === "green" ? "var(--lcd-green)" : "var(--lcd-amber)";
  const glowColor = color === "green" ? "rgba(141,255,106,.4)" : "rgba(255,181,91,.5)";
  return (
    <div style={{
      background: "linear-gradient(var(--panel-hi), var(--panel))",
      border: "1px solid var(--panel-lo)",
      borderRadius: 10,
      padding: "16px 18px",
      boxShadow: "inset 0 1px 0 rgba(255,255,255,.5), 0 2px 6px rgba(0,0,0,.3)",
    }}>
      <div style={{ font: "700 10px var(--font-sans)", letterSpacing: "1.4px", color: "var(--ink-dim)", marginBottom: 12 }}>
        {label}
      </div>
      <div style={{ display: "inline-block", padding: "6px 16px", background: "var(--lcd-bg)", borderRadius: 7, boxShadow: "inset 0 2px 6px rgba(0,0,0,.7)" }}>
        <span style={{ font: `400 34px var(--font-mono)`, color: lcdColor, textShadow: `0 0 10px ${glowColor}` }}>
          {value}
        </span>
      </div>
    </div>
  );
}

