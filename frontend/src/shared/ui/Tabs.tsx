import React from "react";

export interface TabItem {
  key: string;
  label: React.ReactNode;
  icon?: React.ReactNode;
}

interface TabsProps {
  tabs: TabItem[];
  value: string;
  onChange?: (key: string) => void;
  style?: React.CSSProperties;
}

// Top navigation, Terminal-Core: активная вкладка — белый текст в рамке-боксе,
// без скруглений и заливки. Остальные — приглушённый серый.
export function Tabs({ tabs, value, onChange, style = {} }: TabsProps) {
  return (
    <div
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: "4px",
        padding: 0,
        background: "transparent",
        borderRadius: 0,
        ...style,
      }}
    >
      {tabs.map((t) => (
        <TabPill key={t.key} active={t.key === value} icon={t.icon} onClick={() => onChange && onChange(t.key)}>
          {t.label}
        </TabPill>
      ))}
    </div>
  );
}

function TabPill({
  active,
  icon,
  children,
  onClick,
}: {
  active: boolean;
  icon?: React.ReactNode;
  children: React.ReactNode;
  onClick: () => void;
}) {
  const [hover, setHover] = React.useState(false);
  return (
    <button
      onClick={onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: "8px",
        padding: "8px 16px",
        border: active ? "1px solid var(--border-strong)" : "1px solid transparent",
        borderRadius: 0,
        cursor: "pointer",
        fontFamily: "var(--font-mono)",
        fontSize: "var(--fs-sm)",
        textTransform: "uppercase",
        letterSpacing: "0.06em",
        fontWeight: (active ? "var(--fw-semibold)" : "var(--fw-medium)") as any,
        background: active ? "var(--surface-3)" : hover ? "var(--surface-2)" : "transparent",
        color: active ? "var(--text-strong)" : hover ? "var(--text-body)" : "var(--text-muted)",
        transition: "var(--transition-base)",
        whiteSpace: "nowrap",
      }}
    >
      {icon ? <span style={{ display: "inline-flex" }}>{icon}</span> : null}
      {children}
    </button>
  );
}
