import React from "react";
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from "recharts";
import { Card, CategoryTag } from "@/shared/ui";
import { useT } from "@/shared/i18n";
import { api } from "@/shared/api/client";
import type { Analytics } from "@/shared/api/types";
import { groupHex, groupOf } from "@/shared/config/categories";
import { useJobsStore } from "@/shared/model/jobs";
import { formatBytes } from "@/shared/lib/format";

// Читает текущее значение CSS-переменной с documentElement.
const cv = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim();

export function AnalyticsPage() {
  const t = useT();
  const jobs = useJobsStore();
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
    <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-6)", flex: 1, minHeight: 0, overflowY: "auto", padding: 0 }}>
      <div>
        <h1 className="page-title">{t.analytics.title}</h1>
        <p className="page-desc">{t.analytics.desc}</p>
      </div>

      {/* Last scan results */}
      {hasScan ? (
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
      ) : null}

      {/* Library overview */}
      {empty ? (
        <Card style={{ padding: "60px 24px", textAlign: "center", color: "var(--text-faint)" }}>
          {t.analytics.empty}
        </Card>
      ) : (
        <>
          {/* Top stats */}
          <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 14 }}>
            <Stat label={t.analytics.samples} value={data!.samples} accent />
            <Stat label={t.analytics.total} value={formatBytes(data!.bytesTotal)} />
            <Stat label="Avg. uses" value={data!.topUsed && data!.topUsed.length > 0 ? `${data!.topUsed[0].used}×` : "—"} />
          </div>

          {/* Category bar chart */}
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

          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
            {/* Most used */}
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

            {/* BPM chart */}
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
          </div>

          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
            {/* Top keys */}
            <Card>
              <span className="ds-section-label">{t.analytics.topKeys}</span>
              <div style={{ marginTop: 14, display: "flex", flexWrap: "wrap", gap: 8 }}>
                {(data!.topKeys ?? []).length === 0 ? <Muted /> : null}
                {(data!.topKeys ?? []).map((k) => <Chip key={k.key} label={k.key} count={k.count} />)}
              </div>
            </Card>

            {/* Top tags */}
            <Card>
              <span className="ds-section-label">{t.analytics.topTags}</span>
              <div style={{ marginTop: 14, display: "flex", flexWrap: "wrap", gap: 8 }}>
                {(data!.topTags ?? []).length === 0 ? <Muted /> : null}
                {(data!.topTags ?? []).map((tag) => <Chip key={tag.tag} label={`#${tag.tag}`} count={tag.count} />)}
              </div>
            </Card>
          </div>

          {/* Category size breakdown */}
          {data!.byCategory && data!.byCategory.some((c) => c.bytes > 0) ? (
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
          ) : null}
        </>
      )}
    </div>
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

