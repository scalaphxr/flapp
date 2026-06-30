import React from "react";

interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  padding?: number | string;
  elevated?: boolean;
}

// A rounded warm panel. Elevation comes from surface lightness, not borders.
export function Card({ children, padding, elevated = false, style = {}, ...rest }: CardProps) {
  return (
    <div
      style={{
        background: "var(--surface-card)",
        borderRadius: "var(--radius-card)",
        border: "1px solid var(--border-soft)",
        padding: padding != null ? padding : "var(--pad-card)",
        boxShadow: elevated ? "var(--shadow-md)" : "none",
        ...style,
      }}
      {...rest}
    >
      {children}
    </div>
  );
}
