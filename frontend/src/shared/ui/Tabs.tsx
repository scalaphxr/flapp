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

// Top navigation where the active tab is a soft filled coral pill (not an
// underline), sitting in a quiet track.
export function Tabs({ tabs, value, onChange, style = {} }: TabsProps) {
  return (
    <div
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: "4px",
        padding: "5px",
        background: "var(--surface-1)",
        borderRadius: "var(--radius-pill)",
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
        padding: "9px 18px",
        border: "none",
        borderRadius: "var(--radius-pill)",
        cursor: "pointer",
        fontFamily: "var(--font-sans)",
        fontSize: "var(--fs-sm)",
        fontWeight: (active ? "var(--fw-semibold)" : "var(--fw-medium)") as any,
        background: active ? "var(--accent-soft)" : hover ? "var(--surface-3)" : "transparent",
        color: active ? "var(--accent)" : hover ? "var(--text-body)" : "var(--text-muted)",
        transition: "var(--transition-base)",
        whiteSpace: "nowrap",
      }}
    >
      {icon ? <span style={{ display: "inline-flex" }}>{icon}</span> : null}
      {children}
    </button>
  );
}
