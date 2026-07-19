import React from "react";
import { groupOf, groupColor } from "@/shared/config/categories";

interface CategoryTagProps {
  category: string; // full backend category, e.g. "Hi-Hat", "808", "Open Hat"
  label?: string;
  dot?: boolean;
  style?: React.CSSProperties;
}

// Soft, color-coded pill labelling a sound's type. The label shows the precise
// 40-category name; the color comes from its 13-group mapping (tokens).
export function CategoryTag({ category, label, dot = true, style = {} }: CategoryTagProps) {
  const c = groupColor(groupOf(category));
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: "7px",
        padding: "3px 10px 3px 8px",
        borderRadius: 0,
        background: "transparent",
        border: `1px solid ${c.color}`,
        color: c.color,
        fontSize: "var(--fs-caption)",
        fontWeight: "var(--fw-semibold)" as any,
        textTransform: "uppercase",
        letterSpacing: "0.04em",
        lineHeight: 1,
        whiteSpace: "nowrap",
        ...style,
      }}
    >
      {dot ? <span style={{ width: 6, height: 6, borderRadius: "50%", background: c.color, flexShrink: 0 }} /> : null}
      {label ?? category}
    </span>
  );
}
