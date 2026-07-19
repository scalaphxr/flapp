import React from "react";

interface CheckboxProps {
  checked?: boolean;
  onChange?: (next: boolean) => void;
  label?: React.ReactNode;
  disabled?: boolean;
  style?: React.CSSProperties;
}

// Rounded square with a coral fill when active; the whole row is clickable.
export function Checkbox({ checked = false, onChange, label, disabled = false, style = {} }: CheckboxProps) {
  const [hover, setHover] = React.useState(false);
  return (
    <label
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: "11px",
        cursor: disabled ? "not-allowed" : "pointer",
        opacity: disabled ? 0.5 : 1,
        userSelect: "none",
        color: "var(--text-body)",
        fontSize: "var(--fs-body)",
        ...style,
      }}
    >
      <span
        onClick={() => !disabled && onChange && onChange(!checked)}
        style={{
          width: 20,
          height: 20,
          flexShrink: 0,
          borderRadius: 0,
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          background: checked ? "var(--accent)" : "var(--surface-input)",
          border: "1px solid",
          borderColor: checked ? "transparent" : hover ? "var(--border-strong)" : "var(--border-medium)",
          transition: "var(--transition-base)",
        }}
      >
        {checked ? (
          <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="var(--text-on-accent)" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M2.5 6.3 4.8 8.6 9.5 3.6" />
          </svg>
        ) : null}
      </span>
      {label ? <span>{label}</span> : null}
    </label>
  );
}
