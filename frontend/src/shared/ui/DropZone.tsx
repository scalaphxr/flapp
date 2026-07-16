import React from "react";

interface DropZoneProps {
  title: React.ReactNode;
  subtitle: React.ReactNode;
  icon?: React.ReactNode;
  active?: boolean;
  onClick?: () => void;
  style?: React.CSSProperties;
}

// Large, inviting drop target with a dashed rounded border and a warm glow on
// hover/active. Friendly, never an error box.
export function DropZone({ title, subtitle, icon, active = false, onClick, style = {} }: DropZoneProps) {
  const [hover, setHover] = React.useState(false);
  const isHot = hover || active;
  return (
    <div
      onClick={onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        gap: "12px",
        textAlign: "center",
        padding: "32px 24px",
        minHeight: 150,
        cursor: "pointer",
        borderRadius: "var(--radius-lg)",
        border: "2px dashed",
        borderColor: isHot ? "var(--accent)" : "var(--border-strong)",
        background: isHot ? "var(--accent-soft)" : "var(--surface-1)",
        boxShadow: "none",
        transition: "var(--transition-base)",
        ...style,
      }}
    >
      <span
        style={{
          width: 52,
          height: 52,
          borderRadius: "var(--radius-pill)",
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          background: isHot ? "var(--accent-soft)" : "var(--surface-3)",
          color: isHot ? "var(--accent)" : "var(--text-muted)",
          transition: "var(--transition-base)",
          transform: isHot ? "translateY(-2px)" : "none",
        }}
      >
        {icon || <DefaultDropIcon />}
      </span>
      <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
        <span style={{ fontSize: "var(--fs-body)", fontWeight: "var(--fw-medium)" as any, color: "var(--text-body)" }}>{title}</span>
        <span style={{ fontSize: "var(--fs-sm)", color: "var(--text-faint)" }}>{subtitle}</span>
      </div>
    </div>
  );
}

function DefaultDropIcon() {
  return (
    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M12 15V3" />
      <path d="m7 8 5-5 5 5" />
      <path d="M5 15v4a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-4" />
    </svg>
  );
}
