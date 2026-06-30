import React from "react";

interface InputProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, "style"> {
  icon?: React.ReactNode;
  style?: React.CSSProperties;
}

// Rounded text field with an optional leading icon; coral ring on focus.
export function Input({ icon = null, style = {}, ...rest }: InputProps) {
  const [focus, setFocus] = React.useState(false);
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: "10px",
        height: "var(--input-height)",
        padding: "0 14px",
        background: "var(--surface-input)",
        border: "1px solid",
        borderColor: focus ? "var(--accent)" : "var(--border-medium)",
        borderRadius: "var(--radius-input)",
        boxShadow: focus ? "var(--focus-ring)" : "none",
        transition: "var(--transition-base)",
        ...style,
      }}
    >
      {icon ? (
        <span
          style={{
            display: "inline-flex",
            color: focus ? "var(--accent)" : "var(--text-faint)",
            transition: "var(--transition-base)",
          }}
        >
          {icon}
        </span>
      ) : null}
      <input
        onFocus={() => setFocus(true)}
        onBlur={() => setFocus(false)}
        style={{
          flex: 1,
          minWidth: 0,
          background: "transparent",
          border: "none",
          outline: "none",
          color: "var(--text-body)",
          fontFamily: "var(--font-sans)",
          fontSize: "var(--fs-body)",
        }}
        {...rest}
      />
    </div>
  );
}
