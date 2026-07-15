import React from "react";
import { Icons, Tabs, type TabItem } from "@/shared/ui";
import { useSettingsStore } from "@/shared/model/settings";
import { isTauri } from "@/shared/lib/tauri";

interface TopBarProps {
  tabs: TabItem[];
  active: string;
  onChange: (key: string) => void;
}

export function TopBar(props: TopBarProps) {
  const theme = useSettingsStore((s) => s.settings?.theme ?? "warm-dark");
  if (theme === "fl") return <DawTopBar {...props} />;
  return <CleanTopBar {...props} />;
}

// ── Кнопки управления окном (закрыть / свернуть / развернуть) ─────────────────
// При наведении на группу все три точки показывают свой символ — как на macOS.

function WindowControls() {
  const [groupHover, setGroupHover] = React.useState(false);

  async function winClose(e: React.MouseEvent) {
    e.stopPropagation();
    if (!isTauri()) return;
    const { getCurrentWindow } = await import("@tauri-apps/api/window");
    await getCurrentWindow().close();
  }

  async function winMinimize(e: React.MouseEvent) {
    e.stopPropagation();
    if (!isTauri()) return;
    const { getCurrentWindow } = await import("@tauri-apps/api/window");
    await getCurrentWindow().minimize();
  }

  async function winMaximize(e: React.MouseEvent) {
    e.stopPropagation();
    if (!isTauri()) return;
    const { getCurrentWindow } = await import("@tauri-apps/api/window");
    await getCurrentWindow().toggleMaximize();
  }

  const dots = [
    { key: "close", base: "#ff5f57", pressed: "#c0403a", symbol: "×", handler: winClose, title: "Close" },
    { key: "min",   base: "#febc2e", pressed: "#c0920e", symbol: "−", handler: winMinimize, title: "Minimize" },
    { key: "max",   base: "#28c840", pressed: "#1d9630", symbol: "+", handler: winMaximize, title: "Maximize" },
  ] as const;

  return (
    <div
      style={{ display: "flex", gap: 7, alignItems: "center" }}
      onMouseEnter={() => setGroupHover(true)}
      onMouseLeave={() => setGroupHover(false)}
    >
      {dots.map(({ key, base, pressed, symbol, handler, title }) => (
        <button
          key={key}
          title={title}
          onClick={handler}
          style={{
            width: 12, height: 12, borderRadius: "50%",
            background: base,
            border: "none", padding: 0, flexShrink: 0,
            cursor: "pointer",
            display: "inline-flex", alignItems: "center", justifyContent: "center",
            boxShadow: `inset 0 0 0 0.5px rgba(0,0,0,0.22)`,
            // При клике чуть темнее — имитируем pressed через active CSS
            outline: "none",
          }}
          onMouseDown={(e) => { (e.currentTarget as HTMLButtonElement).style.background = pressed; }}
          onMouseUp={(e) => { (e.currentTarget as HTMLButtonElement).style.background = base; }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = base; }}
        >
          {/* Символ виден только при наведении на группу */}
          <span style={{
            fontSize: 8, lineHeight: 1, fontWeight: 900,
            color: groupHover ? "rgba(0,0,0,0.55)" : "transparent",
            userSelect: "none",
          }}>
            {symbol}
          </span>
        </button>
      ))}
    </div>
  );
}

// ── Обработчик двойного клика по drag-зоне (развернуть/восстановить) ──────────

function useDblClickMaximize() {
  return React.useCallback(async (e: React.MouseEvent) => {
    // Двойной клик прямо на контейнере (не на дочерней кнопке)
    if (e.target !== e.currentTarget) return;
    if (!isTauri()) return;
    const { getCurrentWindow } = await import("@tauri-apps/api/window");
    await getCurrentWindow().toggleMaximize();
  }, []);
}

// ── Clean TopBar (не-FL темы) ─────────────────────────────────────────────────

function CleanTopBar({ tabs, active, onChange }: TopBarProps) {
  const onDblClick = useDblClickMaximize();

  return (
    <div
      // Вся полоса — зона перетаскивания окна; кнопки внутри перехватывают клики первыми
      data-tauri-drag-region=""
      onDoubleClick={onDblClick}
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: "var(--space-5)",
        padding: "0 var(--space-6)",
        height: 60,
        flexShrink: 0,
        background: "var(--bg-base)",
        borderBottom: "1px solid var(--border-soft)",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 11, width: 200 }}>
        <span
          style={{
            width: 38, height: 38,
            borderRadius: "var(--radius-md)",
            background: "var(--accent-soft)",
            color: "var(--accent)",
            display: "inline-flex",
            alignItems: "center",
            justifyContent: "center",
            flexShrink: 0,
          }}
        >
          <Icons.Wave width={20} height={20} />
        </span>
        <span
          style={{
            fontSize: "var(--fs-h2)",
            fontWeight: "var(--fw-semibold)" as any,
            letterSpacing: "var(--ls-tight)",
            color: "var(--text-strong)",
            whiteSpace: "nowrap",
          }}
        >
          Flapp
        </span>
      </div>

      <Tabs tabs={tabs} value={active} onChange={onChange} />

      {/* Справа: кнопки управления окном */}
      <div style={{ width: 200, display: "flex", alignItems: "center", justifyContent: "flex-end" }}>
        <WindowControls />
      </div>
    </div>
  );
}

// ── DAW TopBar (FL-тема) ───────────────────────────────────────────────────────

function DawTopBar({ tabs, active, onChange }: TopBarProps) {
  return (
    <div style={{ flexShrink: 0 }}>
      <MenuBar tabs={tabs} active={active} onChange={onChange} />
    </div>
  );
}

// ── Меню-бар (42px) ───────────────────────────────────────────────────────────

function MenuBar({ tabs, active, onChange }: { tabs: TabItem[]; active: string; onChange: (k: string) => void }) {
  const onDblClick = useDblClickMaximize();

  return (
    <div
      // Вся полоса — зона перетаскивания; кнопки внутри перехватывают клики первыми
      data-tauri-drag-region=""
      onDoubleClick={onDblClick}
      style={{
        display: "flex",
        alignItems: "center",
        height: 42,
        flexShrink: 0,
        background: "linear-gradient(var(--chrome-hi), var(--chrome))",
        borderBottom: "1px solid var(--chrome-lo)",
        padding: "0 12px",
        gap: 18,
        boxShadow: "inset 0 1px 0 rgba(255,255,255,.55)",
      }}
    >
      {/* Логотип */}
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <div
          style={{
            width: 20, height: 20, borderRadius: 5,
            background: "linear-gradient(135deg, var(--accent), var(--accent-deep, #e8651e))",
            display: "flex", alignItems: "center", justifyContent: "center",
            boxShadow: "inset 0 1px 0 rgba(255,255,255,.5), 0 1px 2px rgba(0,0,0,.35)",
            flexShrink: 0,
          }}
        >
          <Icons.Wave width={13} height={13} style={{ color: "#fff" }} />
        </div>
        <span style={{ font: "700 14px/1 var(--font-sans)", color: "var(--ink)", letterSpacing: ".6px" }}>
          Flapp
        </span>
      </div>

      {/* Вкладки */}
      <div style={{ display: "flex", gap: 5 }}>
        {tabs.map((tab) => {
          const isActive = tab.key === active;
          return (
            <button
              key={tab.key}
              onClick={() => onChange(tab.key)}
              style={{
                display: "flex", alignItems: "center", gap: 7,
                height: 34, padding: "0 15px", borderRadius: 6,
                font: "600 13px var(--font-sans)",
                letterSpacing: ".3px", cursor: "pointer",
                border: "1px solid",
                transition: "none",
                ...(isActive
                  ? {
                      borderColor: "var(--accent-deep, #e8651e)",
                      background: "linear-gradient(var(--accent), var(--accent-deep, #e8651e))",
                      color: "#fff",
                      boxShadow: "inset 0 1px 0 rgba(255,255,255,.4), 0 0 14px rgba(255,138,60,.5), inset 0 -2px 4px rgba(0,0,0,.18)",
                    }
                  : {
                      borderColor: "var(--chrome-lo)",
                      background: "linear-gradient(var(--btn-hi), var(--btn))",
                      color: "var(--ink)",
                      boxShadow: "inset 0 1px 0 rgba(255,255,255,.5), 0 1px 2px rgba(0,0,0,.2)",
                    }),
              }}
            >
              <span style={{ display: "inline-flex" }}>{tab.icon}</span>
              {tab.label}
            </button>
          );
        })}
      </div>

      {/* Правый край: версия + кнопки управления окном */}
      <div style={{ marginLeft: "auto", display: "flex", alignItems: "center", gap: 12 }}>
        <span style={{ font: "400 11px var(--font-mono)", color: "var(--ink-dim)" }}>
          flapp 0.1.0
        </span>
        <WindowControls />
      </div>
    </div>
  );
}

