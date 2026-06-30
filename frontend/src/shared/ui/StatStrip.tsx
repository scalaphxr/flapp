import React from "react";

export interface StatItem {
  value?: React.ReactNode;
  label: React.ReactNode;
  accent?: boolean;
}

interface StatStripProps {
  items: (StatItem | string)[];
  style?: React.CSSProperties;
}

// A subtle warm strip of stats separated by middots; items can carry accent.
export function StatStrip({ items, style = {} }: StatStripProps) {
  return (
    <div
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: "12px",
        padding: "7px 14px",
        background: "var(--surface-3)",
        borderRadius: "var(--radius-pill)",
        fontSize: "var(--fs-sm)",
        color: "var(--text-muted)",
        ...style,
      }}
    >
      {items.map((it, i) => {
        const item: StatItem = typeof it === "string" ? { label: it } : it;
        return (
          <React.Fragment key={i}>
            {i > 0 ? <span style={{ width: 3, height: 3, borderRadius: "50%", background: "var(--text-faint)", opacity: 0.6 }} /> : null}
            <span style={{ color: item.accent ? "var(--accent)" : "inherit" }}>
              {item.value != null ? (
                <>
                  <strong style={{ fontWeight: "var(--fw-semibold)" as any, color: item.accent ? "var(--accent)" : "var(--text-body)" }}>
                    {item.value}
                  </strong>{" "}
                  {item.label}
                </>
              ) : (
                item.label
              )}
            </span>
          </React.Fragment>
        );
      })}
    </div>
  );
}
