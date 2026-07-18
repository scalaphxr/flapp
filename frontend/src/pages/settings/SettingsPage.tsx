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

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-6)", maxWidth: 720, padding: 0 }}>
      <div style={{ display: "flex", alignItems: "flex-end", justifyContent: "space-between" }}>
        <div>
          <h1 className="page-title">{t.settings.title}</h1>
          <p className="page-desc">{t.settings.desc}</p>
        </div>
        <span
          style={{
            fontSize: "var(--fs-sm)",
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

      <CardSection label={t.settings.general}>
        <Field label={t.settings.language}>
          <Select
            value={s.language}
            onChange={(v) => patch({ language: v })}
            options={[{ value: "ru", label: "Русский" }, { value: "en", label: "English" }]}
          />
        </Field>

        <Field label={t.settings.exportDir}>
          <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
            <div
              className="mono"
              style={{
                flex: 1,
                height: "var(--input-height)",
                display: "flex", alignItems: "center",
                padding: "0 14px",
                background: "var(--surface-input)",
                border: "1px solid var(--border-medium)",
                borderRadius: "var(--radius-input)",
                fontSize: 11,
                color: s.exportDir ? "var(--text-body)" : "var(--text-faint)",
                whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
              }}
            >
              {s.exportDir || t.common.none}
            </div>
            <Button variant="secondary" icon={<Icons.Folder />} onClick={() => pickFolder().then((d) => { if (d) patch({ exportDir: d }); })}>
              {t.settings.pickFolder}
            </Button>
          </div>
        </Field>
      </CardSection>

      <CardSection label={t.settings.processing}>
        <Field label={t.settings.workers}>
          <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
            <input type="range" min={1} max={16} value={s.workers} onChange={(e) => patch({ workers: Number(e.target.value) })} style={{ flex: 1, accentColor: "var(--accent)" }} />
            <span className="mono" style={{ width: 28, textAlign: "right", color: "var(--text-body)" }}>{s.workers}</span>
          </div>
        </Field>

        <Checkbox checked={s.gpu} onChange={(v) => patch({ gpu: v })} label={t.settings.gpu} />
        <Checkbox checked={s.autoUpdate} onChange={(v) => patch({ autoUpdate: v })} label={t.settings.autoUpdate} />
        <Checkbox checked={s.backupOnExit} onChange={(v) => patch({ backupOnExit: v })} label={t.settings.backupOnExit} />
      </CardSection>

      <CardSection label={t.settings.dedup}>
        <Field label={t.settings.dedupThreshold}>
          <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
            <input type="range" min={0} max={100} value={s.dedupThreshold} onChange={(e) => patch({ dedupThreshold: Number(e.target.value) })} style={{ flex: 1, accentColor: "var(--accent)" }} />
            <span className="mono" style={{ width: 28, textAlign: "right", color: "var(--text-body)" }}>{s.dedupThreshold}</span>
          </div>
        </Field>

        <Checkbox checked={s.deepDedup} onChange={(v) => patch({ deepDedup: v })} label={t.settings.deepDedup} />
        <Checkbox checked={s.generateTags} onChange={(v) => patch({ generateTags: v })} label={t.settings.generateTags} />
      </CardSection>

      <CardSection label={t.settings.cache}>
        <Field label={t.settings.cacheLabel}>
          <span style={{ fontSize: "var(--fs-sm)", color: "var(--text-muted)" }}>
            {t.settings.cacheDesc}
          </span>
          <div style={{ display: "flex", alignItems: "center", gap: "var(--gap-control)", marginTop: 8 }}>
            <Button variant="secondary" onClick={handleCacheClear}>
              <Icons.Trash /> {t.settings.cacheClear}
            </Button>
            {cacheMsg && (
              <span style={{ fontSize: "var(--fs-sm)", color: "var(--positive)" }}>
                {cacheMsg}
              </span>
            )}
          </div>
        </Field>
      </CardSection>

      <CardSection label={t.settings.ytSection}>
        {/* Главный сценарий: креды вшиты в сборку — достаточно одной кнопки.
            Ручная настройка ключей спрятана в свёрнутый блок ниже. */}
        <p style={{ margin: 0, fontSize: "var(--fs-sm)", lineHeight: 1.55, color: "var(--text-faint)", maxWidth: 640 }}>
          {t.settings.ytEasyHint}
        </p>

        {/* Подключение канала + статус */}
        <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
          <Button variant="secondary" onClick={handleYtConnect}>
            <Icons.Yt width={13} height={13} />
            {ytStatus?.connected ? t.settings.ytReconnect : t.settings.ytConnect}
          </Button>
          {ytStatus?.connected && (
            <Button variant="secondary" onClick={handleYtDisconnect}>
              {t.settings.ytDisconnect}
            </Button>
          )}
          <span style={{ fontSize: "var(--fs-sm)", color: authError ? "var(--danger)" : ytStatus?.connected ? "var(--positive)" : "var(--text-faint)" }}>
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
            <p style={{ margin: 0, fontSize: "var(--fs-sm)", lineHeight: 1.55, color: "var(--text-faint)", maxWidth: 640 }}>
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
              <MonoInput value={s.ytClientId} onCommit={(v) => patch({ ytClientId: v })} placeholder="xxxxxxxx.apps.googleusercontent.com" />
            </Field>
            <Field label={t.settings.ytClientSecret}>
              <MonoInput value={s.ytClientSecret} onCommit={(v) => patch({ ytClientSecret: v })} secret />
            </Field>
          </>
        )}

        <Field label={t.settings.ytFfmpegPath}>
          <MonoInput value={s.ffmpegPath} onCommit={(v) => patch({ ffmpegPath: v })} placeholder="C:\ffmpeg\bin\ffmpeg.exe" />
          {/* Только ошибки: когда ffmpeg на месте — ничего не показываем. */}
          {ffmpegInfo != null && !ffmpegInfo.found && (
            <span style={{ fontSize: "var(--fs-sm)", fontFamily: "var(--font-mono)", color: "var(--danger)" }}>
              {t.settings.ytFfmpegMissing}
            </span>
          )}
          {ffmpegInfo != null && !ffmpegInfo.found && (
            <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 4 }}>
              <span style={{ fontSize: "var(--fs-sm)", color: "var(--text-faint)", lineHeight: 1.5, maxWidth: 560 }}>
                {t.settings.ytFfmpegWhy}
              </span>
              {ffJob && (ffJob.status === "running" || ffJob.status === "queued") ? (
                <span style={{ fontSize: "var(--fs-sm)", fontFamily: "var(--font-mono)", color: "var(--accent)" }}>
                  {ffJob.stage === "extract" ? t.settings.ytFfmpegExtracting : `${t.settings.ytFfmpegDownloading} ${Math.round((ffJob.progress || 0) * 100)}%`}
                </span>
              ) : (
                <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                  <Button size="sm" onClick={handleFfmpegDownload}>
                    <Icons.Download width={13} height={13} />
                    {t.settings.ytFfmpegDownload}
                  </Button>
                  {ffJob?.status === "failed" && (
                    <span style={{ fontSize: "var(--fs-sm)", color: "var(--danger)" }}>
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
                height: "var(--input-height)",
                display: "flex", alignItems: "center",
                padding: "0 14px",
                background: "var(--surface-input)",
                border: "1px solid var(--border-medium)",
                borderRadius: "var(--radius-input)",
                fontSize: 11,
                color: s.ytDefaultImage ? "var(--text-body)" : "var(--text-faint)",
                whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
              }}
            >
              {s.ytDefaultImage || t.common.none}
            </div>
            <Button variant="secondary" onClick={() => pickImageFile().then((p) => { if (p) patch({ ytDefaultImage: p }); })}>
              <Icons.Folder width={13} height={13} />
              {t.settings.ytPickImage}
            </Button>
          </div>
        </Field>

        <p style={{ margin: 0, fontSize: "var(--fs-sm)", lineHeight: 1.55, color: "var(--text-faint)", maxWidth: 640 }}>
          {t.settings.ytQuotaHint}
        </p>
      </CardSection>
    </div>
  );
}

// ── Local controls (both themes) ───────────────────────────────────────────────

// MonoInput — текстовое поле для ключей/путей: коммит по blur или Enter.
function MonoInput({ value, onCommit, secret, placeholder }: {
  value: string;
  onCommit: (v: string) => void;
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
        height: "var(--input-height)",
        padding: "0 14px",
        background: "var(--surface-input)",
        border: "1px solid var(--border-medium)",
        borderRadius: "var(--radius-input)",
        color: "var(--text-body)",
        fontFamily: "var(--font-mono)",
        fontSize: 12,
        outline: "none",
      }}
    />
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

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <span style={{ fontSize: "var(--fs-sm)", color: "var(--text-muted)" }}>{label}</span>
      {children}
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
