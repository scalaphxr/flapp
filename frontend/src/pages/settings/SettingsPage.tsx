import React from "react";
import { Button, Card, Checkbox, Icons } from "@/shared/ui";
import { useT } from "@/shared/i18n";
import { useSettingsStore } from "@/shared/model/settings";
import { useJobsStore } from "@/shared/model/jobs";
import { pickFolder } from "@/shared/lib/tauri";
import type { Settings, YtStatus } from "@/shared/api/types";
import { api } from "@/shared/api/client";
import { formatBytes } from "@/shared/lib/format";

export function SettingsPage() {
  const t = useT();
  const { settings, load, update } = useSettingsStore();
  const [flash, setFlash] = React.useState(false);
  const [cacheMsg, setCacheMsg] = React.useState<string | null>(null);

  // YouTube: статус подключения, найденный ffmpeg, ожидание OAuth-редиректа.
  const [ytStatus, setYtStatus] = React.useState<YtStatus | null>(null);
  const [ffmpegInfo, setFfmpegInfo] = React.useState<{ found: boolean; path: string } | null>(null);
  const [authWaiting, setAuthWaiting] = React.useState(false);
  const [authError, setAuthError] = React.useState<string | null>(null);
  const [showYtAdvanced, setShowYtAdvanced] = React.useState(false);

  const refreshYt = React.useCallback(() => {
    api.ytStatus().then(setYtStatus).catch(() => setYtStatus(null));
  }, []);
  React.useEffect(() => { refreshYt(); }, [refreshYt]);
  React.useEffect(() => {
    api.ytFfmpeg().then(setFfmpegInfo).catch(() => setFfmpegInfo(null));
  }, [settings?.ffmpegPath]);

  // Автоскачивание ffmpeg: джоба с прогрессом; по завершении бэкенд сам
  // прописывает путь в настройки — перечитываем их и статус.
  const jobs = useJobsStore((s) => s.jobs);
  const [ffJobId, setFfJobId] = React.useState<string | null>(null);
  const ffJob = ffJobId ? jobs[ffJobId] : null;
  React.useEffect(() => {
    if (!ffJob) return;
    if (ffJob.status === "completed") {
      setFfJobId(null);
      void load();
    } else if (ffJob.status === "failed" || ffJob.status === "canceled") {
      // Джобу не сбрасываем — под кнопкой останется текст ошибки.
      api.ytFfmpeg().then(setFfmpegInfo).catch(() => {});
    }
  }, [ffJob?.status]); // eslint-disable-line react-hooks/exhaustive-deps

  async function handleFfmpegDownload() {
    try {
      const { jobId } = await api.ytFfmpegDownload();
      setFfJobId(jobId);
    } catch { /* статусная строка останется красной */ }
  }

  // Пока пользователь подтверждает доступ в браузере — опрашиваем статус.
  // После успеха возвращаем окно приложения на передний план.
  React.useEffect(() => {
    if (!authWaiting) return;
    const iv = window.setInterval(() => {
      api.ytStatus().then((st) => {
        setYtStatus(st);
        if (st.connected) {
          setAuthWaiting(false);
          void import("@tauri-apps/api/window").then(async ({ getCurrentWindow }) => {
            const w = getCurrentWindow();
            await w.unminimize().catch(() => {});
            await w.setFocus();
          }).catch(() => {});
        }
      }).catch(() => {});
    }, 2000);
    // Не держим «подтверди доступ…» вечно: по таймауту возвращаем обычный
    // статус, чтобы не маскировать реальное состояние (флоу на бэке живёт 5 мин).
    const to = window.setTimeout(() => setAuthWaiting(false), 2 * 60_000);
    return () => { window.clearInterval(iv); window.clearTimeout(to); };
  }, [authWaiting]);

  // Открывает ссылку в системном браузере (Chrome/Edge), не внутри приложения.
  function openExternal(url: string) {
    void import("@tauri-apps/api/core").then(({ invoke }) =>
      invoke("plugin:shell|open", { path: url }),
    ).catch(() => {});
  }

  async function handleYtConnect() {
    setAuthError(null);
    try {
      await api.ytAuth();
      setAuthWaiting(true);
    } catch (e) {
      // Показываем причину прямо в статусной строке — молчаливая кнопка
      // выглядит сломанной (например, когда не заданы ключи API).
      setAuthWaiting(false);
      setAuthError(e instanceof Error ? e.message : String(e));
    }
  }

  async function handleYtDisconnect() {
    try { await api.ytDisconnect(); } catch { /* ignore */ }
    setAuthWaiting(false);
    setAuthError(null);
    refreshYt();
  }

  async function pickImageFile(): Promise<string | null> {
    try {
      const { open } = await import("@tauri-apps/plugin-dialog");
      const res = await open({
        multiple: false,
        directory: false,
        filters: [{ name: "Images", extensions: ["png", "jpg", "jpeg", "webp", "bmp"] }],
      });
      if (!res) return null;
      return Array.isArray(res) ? res[0] : res;
    } catch {
      return null;
    }
  }

  async function handleCacheClear() {
    if (!window.confirm(t.settings.cacheClearConfirm)) return;
    try {
      const stats = await api.cacheClear();
      setCacheMsg(`${t.settings.cacheCleared} ${formatBytes(stats.totalBytes)}`);
      window.setTimeout(() => setCacheMsg(null), 3000);
    } catch {
      setCacheMsg("Error clearing cache");
      window.setTimeout(() => setCacheMsg(null), 3000);
    }
  }

  React.useEffect(() => {
    if (!settings) load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const s = settings;
  const isFl = s?.theme === "fl";

  async function patch(p: Partial<Settings>) {
    await update(p);
    setFlash(true);
    window.clearTimeout((patch as any)._t);
    (patch as any)._t = window.setTimeout(() => setFlash(false), 1500);
  }

  if (!s) {
    return (
      <div>
        <h1 className="page-title">{t.settings.title}</h1>
        <p className="page-desc">{t.common.loading}</p>
      </div>
    );
  }

  // Helper: wrap a section in either Card or FL rack panel
  const Section = isFl ? FlSection : CardSection;
  const ChkBox = isFl ? FlMetaCheckbox : Checkbox;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: isFl ? 14 : "var(--space-6)", maxWidth: 720, padding: isFl ? "20px 24px" : 0 }}>
      <div style={{ display: "flex", alignItems: "flex-end", justifyContent: "space-between" }}>
        {!isFl && (
          <div>
            <h1 className="page-title">{t.settings.title}</h1>
            <p className="page-desc">{t.settings.desc}</p>
          </div>
        )}
        <span
          style={{
            fontSize: isFl ? "12px" : "var(--fs-sm)",
            fontFamily: isFl ? "var(--font-sans)" : undefined,
            color: "var(--positive)",
            opacity: flash ? 1 : 0,
            transition: "opacity var(--dur-base) var(--ease-out)",
            display: "inline-flex",
            alignItems: "center",
            gap: 6,
          }}
        >
          <Icons.Check width={14} height={14} /> {t.settings.saved}
        </span>
      </div>

      <Section label={t.settings.general}>
        <Field label={t.settings.language}>
          {isFl ? (
            <FlSelect
              value={s.language}
              onChange={(v) => patch({ language: v })}
              options={[{ value: "ru", label: "Русский" }, { value: "en", label: "English" }]}
            />
          ) : (
            <Select
              value={s.language}
              onChange={(v) => patch({ language: v })}
              options={[{ value: "ru", label: "Русский" }, { value: "en", label: "English" }]}
            />
          )}
        </Field>

        <Field label={t.settings.theme}>
          <ThemePicker value={s.theme} onChange={(v) => patch({ theme: v })} />
        </Field>

        <Field label={t.settings.exportDir}>
          <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
            <div
              className="mono"
              style={{
                flex: 1,
                height: isFl ? 36 : "var(--input-height)",
                display: "flex", alignItems: "center",
                padding: "0 14px",
                background: isFl ? "var(--groove)" : "var(--surface-input)",
                border: `1px solid ${isFl ? "var(--line-work)" : "var(--border-medium)"}`,
                borderRadius: isFl ? 7 : "var(--radius-input)",
                boxShadow: isFl ? "inset 0 2px 4px rgba(0,0,0,.35)" : undefined,
                fontSize: 11,
                color: s.exportDir ? (isFl ? "var(--ink-on-work)" : "var(--text-body)") : (isFl ? "var(--ink-dim)" : "var(--text-faint)"),
                whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
              }}
            >
              {s.exportDir || t.common.none}
            </div>
            {isFl ? (
              <button
                onClick={() => pickFolder().then((d) => { if (d) patch({ exportDir: d }); })}
                style={{ height: 36, padding: "0 14px", display: "flex", alignItems: "center", gap: 7, background: "linear-gradient(var(--btn-hi),var(--btn))", border: "1px solid var(--chrome-lo)", borderRadius: 7, color: "var(--ink)", font: "600 12.5px var(--font-sans)", cursor: "pointer", boxShadow: "inset 0 1px 0 rgba(255,255,255,.5),0 1px 2px rgba(0,0,0,.25)" }}
              >
                <Icons.Folder width={13} height={13} />
                {t.settings.pickFolder}
              </button>
            ) : (
              <Button variant="secondary" icon={<Icons.Folder />} onClick={() => pickFolder().then((d) => { if (d) patch({ exportDir: d }); })}>
                {t.settings.pickFolder}
              </Button>
            )}
          </div>
        </Field>
      </Section>

      <Section label={t.settings.processing}>
        <Field label={t.settings.workers}>
          {isFl ? (
            <FlFader value={s.workers} min={1} max={16} step={1} onChange={(v) => patch({ workers: v })} />
          ) : (
            <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
              <input type="range" min={1} max={16} value={s.workers} onChange={(e) => patch({ workers: Number(e.target.value) })} style={{ flex: 1, accentColor: "var(--accent)" }} />
              <span className="mono" style={{ width: 28, textAlign: "right", color: "var(--text-body)" }}>{s.workers}</span>
            </div>
          )}
        </Field>

        <ChkBox checked={s.gpu} onChange={(v) => patch({ gpu: v })} label={t.settings.gpu} />
        <ChkBox checked={s.autoUpdate} onChange={(v) => patch({ autoUpdate: v })} label={t.settings.autoUpdate} />
        <ChkBox checked={s.backupOnExit} onChange={(v) => patch({ backupOnExit: v })} label={t.settings.backupOnExit} />
      </Section>

      <Section label={t.settings.dedup}>
        <Field label={t.settings.dedupThreshold}>
          {isFl ? (
            <FlFader value={s.dedupThreshold} min={0} max={100} step={1} onChange={(v) => patch({ dedupThreshold: v })} />
          ) : (
            <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
              <input type="range" min={0} max={100} value={s.dedupThreshold} onChange={(e) => patch({ dedupThreshold: Number(e.target.value) })} style={{ flex: 1, accentColor: "var(--accent)" }} />
              <span className="mono" style={{ width: 28, textAlign: "right", color: "var(--text-body)" }}>{s.dedupThreshold}</span>
            </div>
          )}
        </Field>

        <ChkBox checked={s.deepDedup} onChange={(v) => patch({ deepDedup: v })} label={t.settings.deepDedup} />
        <ChkBox checked={s.generateTags} onChange={(v) => patch({ generateTags: v })} label={t.settings.generateTags} />
      </Section>

      <Section label={t.settings.cache}>
        <Field label={t.settings.cacheLabel}>
          <span style={{ fontSize: isFl ? "12.5px" : "var(--fs-sm)", fontFamily: isFl ? "var(--font-sans)" : undefined, color: isFl ? "var(--ink-on-work-dim)" : "var(--text-muted)" }}>
            {t.settings.cacheDesc}
          </span>
          <div style={{ display: "flex", alignItems: "center", gap: "var(--gap-control)", marginTop: 8 }}>
            {isFl ? (
              <button
                onClick={handleCacheClear}
                style={{ height: 34, padding: "0 14px", display: "flex", alignItems: "center", gap: 7, background: "linear-gradient(var(--btn-hi),var(--btn))", border: "1px solid var(--chrome-lo)", borderRadius: 7, color: "var(--ink)", font: "600 12.5px var(--font-sans)", cursor: "pointer", boxShadow: "inset 0 1px 0 rgba(255,255,255,.5),0 1px 2px rgba(0,0,0,.25)" }}
              >
                <Icons.Trash width={13} height={13} />
                {t.settings.cacheClear}
              </button>
            ) : (
              <Button variant="secondary" onClick={handleCacheClear}>
                <Icons.Trash /> {t.settings.cacheClear}
              </Button>
            )}
            {cacheMsg && (
              <span style={{ fontSize: isFl ? "12px" : "var(--fs-sm)", fontFamily: isFl ? "var(--font-mono)" : undefined, color: "var(--positive)" }}>
                {cacheMsg}
              </span>
            )}
          </div>
        </Field>
      </Section>

      <Section label={t.settings.ytSection}>
        {/* Главный сценарий: креды вшиты в сборку — достаточно одной кнопки.
            Ручная настройка ключей спрятана в свёрнутый блок ниже. */}
        <p style={{ margin: 0, fontSize: isFl ? "11.5px" : "var(--fs-sm)", lineHeight: 1.55, color: isFl ? "var(--ink-dim)" : "var(--text-faint)", maxWidth: 640 }}>
          {t.settings.ytEasyHint}
        </p>

        {/* Подключение канала + статус */}
        <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
          <ChromeBtn isFl={isFl} onClick={handleYtConnect}>
            <Icons.Yt width={13} height={13} />
            {ytStatus?.connected ? t.settings.ytReconnect : t.settings.ytConnect}
          </ChromeBtn>
          {ytStatus?.connected && (
            <ChromeBtn isFl={isFl} onClick={handleYtDisconnect}>
              {t.settings.ytDisconnect}
            </ChromeBtn>
          )}
          <span style={{ fontSize: isFl ? "12px" : "var(--fs-sm)", fontFamily: isFl ? "var(--font-sans)" : undefined, color: authError ? "var(--rec, #ff453a)" : ytStatus?.connected ? "var(--positive)" : (isFl ? "var(--ink-dim)" : "var(--text-faint)") }}>
            {authError
              ? `${t.settings.ytAuthFailed} ${authError}`
              : authWaiting
                ? t.settings.ytAuthWait
                : ytStatus?.connected
                  ? `${t.settings.ytConnected} ${ytStatus.channelTitle || "YouTube"}`
                  : ytStatus && !ytStatus.configured
                    ? t.settings.ytNotConfigured
                    : t.settings.ytNotConnected}
          </span>
        </div>

        <div>
          <Button size="sm" variant="ghost" onClick={() => setShowYtAdvanced((v) => !v)}>
            {showYtAdvanced ? "▾" : "▸"} {t.settings.ytAdvanced}
          </Button>
        </div>
        {showYtAdvanced && (
          <>
            <p style={{ margin: 0, fontSize: isFl ? "11.5px" : "var(--fs-sm)", lineHeight: 1.55, color: isFl ? "var(--ink-dim)" : "var(--text-faint)", maxWidth: 640 }}>
              {t.settings.ytHowTo}
            </p>

            {/* Прямые ссылки на нужные страницы консоли — открываются в браузере,
                чтобы не набирать адреса руками. */}
            <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 8 }}>
              {([
                [t.settings.ytLinkCreateProject, "https://console.cloud.google.com/projectcreate"],
                [t.settings.ytLinkEnableApi, "https://console.cloud.google.com/apis/library/youtube.googleapis.com"],
                [t.settings.ytLinkConsent, "https://console.cloud.google.com/auth/overview"],
                [t.settings.ytLinkCredentials, "https://console.cloud.google.com/apis/credentials"],
              ] as [string, string][]).map(([label, url], i) => (
                <Button key={url} size="sm" variant="ghost" onClick={() => openExternal(url)}>
                  {i + 1}. {label} ↗
                </Button>
              ))}
            </div>

            <Field label={t.settings.ytClientId}>
              <MonoInput isFl={isFl} value={s.ytClientId} onCommit={(v) => patch({ ytClientId: v })} placeholder="xxxxxxxx.apps.googleusercontent.com" />
            </Field>
            <Field label={t.settings.ytClientSecret}>
              <MonoInput isFl={isFl} value={s.ytClientSecret} onCommit={(v) => patch({ ytClientSecret: v })} secret />
            </Field>
          </>
        )}

        <Field label={t.settings.ytFfmpegPath}>
          <MonoInput isFl={isFl} value={s.ffmpegPath} onCommit={(v) => patch({ ffmpegPath: v })} placeholder="C:\ffmpeg\bin\ffmpeg.exe" />
          <span style={{ fontSize: isFl ? "11.5px" : "var(--fs-sm)", fontFamily: "var(--font-mono)", color: ffmpegInfo?.found ? "var(--positive)" : "var(--rec, #ff453a)" }}>
            {ffmpegInfo == null ? "…" : ffmpegInfo.found ? `${t.settings.ytFfmpegOk} ${ffmpegInfo.path}` : t.settings.ytFfmpegMissing}
          </span>
          {ffmpegInfo != null && !ffmpegInfo.found && (
            <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 4 }}>
              <span style={{ fontSize: isFl ? "11.5px" : "var(--fs-sm)", color: isFl ? "var(--ink-dim)" : "var(--text-faint)", lineHeight: 1.5, maxWidth: 560 }}>
                {t.settings.ytFfmpegWhy}
              </span>
              {ffJob && (ffJob.status === "running" || ffJob.status === "queued") ? (
                <span style={{ fontSize: isFl ? "11.5px" : "var(--fs-sm)", fontFamily: "var(--font-mono)", color: isFl ? "var(--lcd-amber, #ffb55b)" : "var(--accent)" }}>
                  {ffJob.stage === "extract" ? t.settings.ytFfmpegExtracting : `${t.settings.ytFfmpegDownloading} ${Math.round((ffJob.progress || 0) * 100)}%`}
                </span>
              ) : (
                <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                  <Button size="sm" onClick={handleFfmpegDownload}>
                    <Icons.Download width={13} height={13} />
                    {t.settings.ytFfmpegDownload}
                  </Button>
                  {ffJob?.status === "failed" && (
                    <span style={{ fontSize: "var(--fs-sm)", color: "var(--rec, #ff453a)" }}>
                      {t.settings.ytFfmpegFailed}{ffJob.error ? `: ${ffJob.error}` : ""}
                    </span>
                  )}
                </div>
              )}
            </div>
          )}
        </Field>

        <Field label={t.settings.ytDefaultImage}>
          <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
            <div
              className="mono"
              style={{
                flex: 1,
                height: isFl ? 36 : "var(--input-height)",
                display: "flex", alignItems: "center",
                padding: "0 14px",
                background: isFl ? "var(--groove)" : "var(--surface-input)",
                border: `1px solid ${isFl ? "var(--line-work)" : "var(--border-medium)"}`,
                borderRadius: isFl ? 7 : "var(--radius-input)",
                boxShadow: isFl ? "inset 0 2px 4px rgba(0,0,0,.35)" : undefined,
                fontSize: 11,
                color: s.ytDefaultImage ? (isFl ? "var(--ink-on-work)" : "var(--text-body)") : (isFl ? "var(--ink-dim)" : "var(--text-faint)"),
                whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
              }}
            >
              {s.ytDefaultImage || t.common.none}
            </div>
            <ChromeBtn isFl={isFl} onClick={() => pickImageFile().then((p) => { if (p) patch({ ytDefaultImage: p }); })}>
              <Icons.Folder width={13} height={13} />
              {t.settings.ytPickImage}
            </ChromeBtn>
          </div>
        </Field>

        <p style={{ margin: 0, fontSize: isFl ? "11.5px" : "var(--fs-sm)", lineHeight: 1.55, color: isFl ? "var(--ink-dim)" : "var(--text-faint)", maxWidth: 640 }}>
          {t.settings.ytQuotaHint}
        </p>
      </Section>
    </div>
  );
}

// ── Local controls (both themes) ───────────────────────────────────────────────

// MonoInput — текстовое поле для ключей/путей: коммит по blur или Enter.
function MonoInput({ value, onCommit, isFl, secret, placeholder }: {
  value: string;
  onCommit: (v: string) => void;
  isFl: boolean;
  secret?: boolean;
  placeholder?: string;
}) {
  const [draft, setDraft] = React.useState(value ?? "");
  React.useEffect(() => setDraft(value ?? ""), [value]);
  return (
    <input
      type={secret ? "password" : "text"}
      value={draft}
      placeholder={placeholder}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={() => { if (draft.trim() !== (value ?? "")) onCommit(draft.trim()); }}
      onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); }}
      spellCheck={false}
      style={{
        width: "100%", maxWidth: 460,
        height: isFl ? 36 : "var(--input-height)",
        padding: "0 14px",
        background: isFl ? "var(--groove)" : "var(--surface-input)",
        border: `1px solid ${isFl ? "var(--line-work)" : "var(--border-medium)"}`,
        borderRadius: isFl ? 7 : "var(--radius-input)",
        boxShadow: isFl ? "inset 0 2px 4px rgba(0,0,0,.35)" : undefined,
        color: isFl ? "var(--ink-on-work)" : "var(--text-body)",
        fontFamily: "var(--font-mono)",
        fontSize: 12,
        outline: "none",
      }}
    />
  );
}

// ChromeBtn — кнопка в стиле страницы: металл в FL-теме, secondary в остальных.
function ChromeBtn({ onClick, children, isFl }: { onClick: () => void; children: React.ReactNode; isFl: boolean }) {
  if (!isFl) {
    return <Button variant="secondary" onClick={onClick}>{children}</Button>;
  }
  return (
    <button
      onClick={onClick}
      style={{ height: 34, padding: "0 14px", display: "flex", alignItems: "center", gap: 7, background: "linear-gradient(var(--btn-hi),var(--btn))", border: "1px solid var(--chrome-lo)", borderRadius: 7, color: "var(--ink)", font: "600 12.5px var(--font-sans)", cursor: "pointer", boxShadow: "inset 0 1px 0 rgba(255,255,255,.5),0 1px 2px rgba(0,0,0,.25)" }}
    >
      {children}
    </button>
  );
}

// ── Layout helpers ─────────────────────────────────────────────────────────────

function CardSection({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 18 }}>
      <span className="ds-section-label">{label}</span>
      {children}
    </Card>
  );
}

function FlSection({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{
      position: "relative",
      background: "linear-gradient(var(--panel-hi), var(--panel))",
      border: "1px solid var(--panel-lo)",
      borderRadius: 10,
      padding: "16px 20px 18px",
      boxShadow: "inset 0 1px 0 rgba(255,255,255,.5), 0 2px 6px rgba(0,0,0,.3)",
    }}>
      {/* Corner screws */}
      <Screw style={{ top: 7, left: 7 }} />
      <Screw style={{ top: 7, right: 7 }} />
      <Screw style={{ bottom: 7, left: 7 }} />
      <Screw style={{ bottom: 7, right: 7 }} />

      <div style={{ font: "700 10px var(--font-sans)", letterSpacing: "1.5px", textTransform: "uppercase", color: "var(--ink-dim)", marginBottom: 14 }}>
        {label}
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        {children}
      </div>
    </div>
  );
}

function Screw({ style }: { style: React.CSSProperties }) {
  return (
    <div style={{
      position: "absolute", width: 10, height: 10, borderRadius: "50%",
      background: "radial-gradient(circle at 40% 35%, var(--panel-hi), var(--panel-lo))",
      border: "1px solid rgba(0,0,0,.2)",
      boxShadow: "inset 0 1px 0 rgba(255,255,255,.3)",
      ...style,
    }}>
      <div style={{ position: "absolute", inset: "1px", borderRadius: "50%", display: "flex", alignItems: "center", justifyContent: "center" }}>
        <div style={{ width: 5, height: 1, background: "rgba(0,0,0,.35)", borderRadius: 1 }} />
      </div>
    </div>
  );
}

// ── FL fader (drag) ────────────────────────────────────────────────────────────

function FlFader({ value, min, max, step, onChange }: { value: number; min: number; max: number; step: number; onChange: (v: number) => void }) {
  const trackRef = React.useRef<HTMLDivElement>(null);
  const startRef = React.useRef<{ x: number; v: number } | null>(null);

  const pct = ((value - min) / (max - min)) * 100;

  const handleMouseDown = (e: React.MouseEvent) => {
    e.preventDefault();
    startRef.current = { x: e.clientX, v: value };
    const onMove = (me: MouseEvent) => {
      if (!startRef.current || !trackRef.current) return;
      const rect = trackRef.current.getBoundingClientRect();
      const ratio = Math.max(0, Math.min(1, (me.clientX - rect.left) / rect.width));
      const raw = min + ratio * (max - min);
      onChange(Math.round(raw / step) * step);
    };
    const onUp = () => {
      startRef.current = null;
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  };

  const handleTrackClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!trackRef.current) return;
    const rect = trackRef.current.getBoundingClientRect();
    const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    const raw = min + ratio * (max - min);
    onChange(Math.round(raw / step) * step);
  };

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
      <div
        ref={trackRef}
        onClick={handleTrackClick}
        style={{ flex: 1, height: 14, borderRadius: 3, background: "var(--groove)", border: "1px solid var(--panel-lo)", boxShadow: "inset 0 1px 3px rgba(0,0,0,.5)", position: "relative", cursor: "pointer" }}
      >
        <div style={{ position: "absolute", left: 0, top: 0, bottom: 0, width: `${pct}%`, background: "var(--accent)", borderRadius: 3 }} />
        <div
          onMouseDown={handleMouseDown}
          style={{
            position: "absolute", top: "50%", left: `${pct}%`,
            transform: "translate(-50%, -50%)",
            width: 14, height: 22, borderRadius: 3, cursor: "ew-resize",
            background: "linear-gradient(var(--btn-hi), var(--btn))",
            border: "1px solid var(--chrome-lo)",
            boxShadow: "inset 0 1px 0 rgba(255,255,255,.5), 0 2px 4px rgba(0,0,0,.35)",
          }}
        />
      </div>
      <div style={{ width: 34, textAlign: "right", font: "400 13px var(--font-mono)", color: "var(--lcd-amber)", textShadow: "0 0 6px rgba(255,181,91,.4)" }}>{value}</div>
    </div>
  );
}

// ── FL metal checkbox ─────────────────────────────────────────────────────────

function FlMetaCheckbox({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <label style={{ display: "flex", alignItems: "center", gap: 10, cursor: "pointer" }}>
      <span
        onClick={() => onChange(!checked)}
        style={{
          width: 20, height: 20, borderRadius: 5, flexShrink: 0,
          display: "flex", alignItems: "center", justifyContent: "center",
          background: checked ? "linear-gradient(var(--accent), var(--accent-deep, #e8651e))" : "linear-gradient(var(--panel-hi), var(--panel))",
          border: "1px solid var(--panel-lo)",
          boxShadow: checked
            ? "inset 0 1px 0 rgba(255,255,255,.3), 0 0 8px rgba(255,138,60,.4)"
            : "inset 0 1px 2px rgba(0,0,0,.3), inset 0 -1px 0 rgba(255,255,255,.15)",
        }}
      >
        {checked && (
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
            <path d="M4 12l5 5L20 6"/>
          </svg>
        )}
      </span>
      <span style={{ font: "500 13px var(--font-sans)", color: "var(--ink)" }}>{label}</span>
    </label>
  );
}

// ── FL select ─────────────────────────────────────────────────────────────────

function FlSelect({ value, onChange, options }: { value: string; onChange: (v: string) => void; options: { value: string; label: string }[] }) {
  return (
    <div style={{ position: "relative", display: "inline-flex", alignItems: "center" }}>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        style={{
          height: 36, padding: "0 32px 0 14px",
          background: "linear-gradient(var(--btn-hi), var(--btn))",
          color: "var(--ink)",
          border: "1px solid var(--chrome-lo)",
          borderRadius: 7,
          fontFamily: "var(--font-sans)", fontSize: "13px", fontWeight: 600,
          cursor: "pointer", appearance: "none", outline: "none",
          boxShadow: "inset 0 1px 0 rgba(255,255,255,.5), 0 1px 2px rgba(0,0,0,.25)",
        }}
      >
        {options.map((o) => <option key={o.value} value={o.value} style={{ background: "var(--panel)", color: "var(--ink)" }}>{o.label}</option>)}
      </select>
      <svg width="10" height="10" viewBox="0 0 24 24" fill="currentColor" style={{ position: "absolute", right: 10, pointerEvents: "none", color: "var(--ink-dim)" }}>
        <path d="M7 10l5 5 5-5z"/>
      </svg>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <span style={{ fontSize: "var(--fs-sm)", color: "var(--text-muted)" }}>{label}</span>
      {children}
    </div>
  );
}

function ThemePicker({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const t = useT();
  const options = [
    { value: "warm-dark", label: t.settings.themeWarmDark },
    { value: "light",     label: t.settings.themeLight },
    { value: "dark",      label: t.settings.themeDark },
    { value: "fl",        label: "FL Studio" },
  ];
  return (
    <div
      style={{
        display: "inline-flex",
        background: "var(--surface-well)",
        borderRadius: "var(--radius-md)",
        padding: 3,
        gap: 2,
        border: "1px solid var(--border-soft)",
      }}
    >
      {options.map((o) => {
        const active = value === o.value;
        return (
          <button
            key={o.value}
            onClick={() => onChange(o.value)}
            style={{
              padding: "8px 18px",
              borderRadius: "calc(var(--radius-md) - 1px)",
              border: "none",
              cursor: "pointer",
              fontSize: "var(--fs-sm)",
              fontFamily: "var(--font-sans)",
              fontWeight: active ? 600 : 400,
              background: active ? "var(--surface-card)" : "transparent",
              color: active ? "var(--text-strong)" : "var(--text-muted)",
              boxShadow: active ? "var(--shadow-sm)" : "none",
              transition: "all var(--dur-fast) var(--ease-out)",
            }}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}

function Select({ value, onChange, options }: { value: string; onChange: (v: string) => void; options: { value: string; label: string }[] }) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      style={{
        height: "var(--input-height)",
        padding: "0 14px",
        background: "var(--surface-input)",
        color: "var(--text-body)",
        border: "1px solid var(--border-medium)",
        borderRadius: "var(--radius-input)",
        fontFamily: "var(--font-sans)",
        fontSize: "var(--fs-body)",
        cursor: "pointer",
        appearance: "none",
        outline: "none",
        maxWidth: 280,
      }}
    >
      {options.map((o) => (
        <option key={o.value} value={o.value} style={{ background: "var(--surface-2)" }}>
          {o.label}
        </option>
      ))}
    </select>
  );
}
