import React from "react";
import { Icons, Tabs, type TabItem } from "@/shared/ui";
import { isTauri } from "@/shared/lib/tauri";

interface TopBarProps {
  tabs: TabItem[];
  active: string;
  onChange: (key: string) => void;
}

export function TopBar(props: TopBarProps) {
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
            width: 34, height: 34,
            borderRadius: 0,
            background: "transparent",
            border: "1px solid var(--border-medium)",
            color: "var(--text-strong)",
            display: "inline-flex",
            alignItems: "center",
            justifyContent: "center",
            flexShrink: 0,
          }}
        >
          <Icons.Wave width={18} height={18} />
        </span>
        <span
          style={{
            fontSize: "var(--fs-h2)",
            fontWeight: "var(--fw-semibold)" as any,
            letterSpacing: "0.02em",
            color: "var(--text-strong)",
            whiteSpace: "nowrap",
          }}
        >
          flapp<span style={{ color: "var(--text-faint)" }}>_</span>
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

