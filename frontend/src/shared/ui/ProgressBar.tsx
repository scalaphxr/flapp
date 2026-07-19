import React from "react";

interface ProgressBarProps {
  value?: number; // 0..100
  caption?: React.ReactNode;
  percent?: boolean;
  style?: React.CSSProperties;
}

// Soft coral, fully rounded track with an optional caption + percentage row.
export function ProgressBar({ value = 0, caption, percent = false, style = {} }: ProgressBarProps) {
  const pct = Math.max(0, Math.min(100, value));
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "8px", width: "100%", ...style }}>
      {caption != null || percent ? (
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 12 }}>
          <span style={{ fontSize: "var(--fs-sm)", color: "var(--text-muted)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            {caption}
          </span>
          {percent ? (
            <span style={{ fontSize: "var(--fs-sm)", color: "var(--text-faint)", fontFamily: "var(--font-mono)", flexShrink: 0 }}>
              {Math.round(pct)}%
            </span>
          ) : null}
        </div>
      ) : null}
      <div style={{ height: 7, width: "100%", background: "var(--surface-3)", borderRadius: "var(--radius-pill)", overflow: "hidden" }}>
        <div
          style={{
            height: "100%",
            width: pct + "%",
            background: "var(--accent)",
            borderRadius: "var(--radius-pill)",
            transition: "width var(--dur-slow) var(--ease-out)",
          }}
        />
      </div>
    </div>
  );
}
