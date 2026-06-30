import React from "react";

type Variant = "primary" | "secondary" | "ghost" | "danger";
type Size = "sm" | "md" | "lg";

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  icon?: React.ReactNode;
  full?: boolean;
}

export function Button({
  children,
  variant = "secondary",
  size = "md",
  icon = null,
  disabled = false,
  full = false,
  style = {},
  ...rest
}: ButtonProps) {
  const sizes: Record<Size, React.CSSProperties> = {
    sm: { padding: "0 14px", fontSize: "var(--fs-sm)", height: "34px" },
    md: { padding: "0 20px", fontSize: "var(--fs-sm)", height: "38px" },
    lg: { padding: "0 26px", fontSize: "var(--fs-body)", height: "44px" },
  };
  const variants: Record<Variant, React.CSSProperties> = {
    primary: {
      background: "var(--btn-primary-bg)",
      color: "var(--btn-primary-color)",
      border: "1px solid var(--btn-primary-border)",
      fontWeight: "var(--fw-semibold)" as any,
      boxShadow: "var(--btn-primary-shadow)",
    },
    secondary: {
      background: "var(--btn-secondary-bg)",
      color: "var(--btn-secondary-color)",
      border: "1px solid var(--btn-secondary-border)",
      fontWeight: "var(--fw-medium)" as any,
      boxShadow: "var(--btn-secondary-shadow)",
    },
    ghost: {
      background: "transparent",
      color: "var(--btn-ghost-color)",
      border: "1px solid transparent",
      fontWeight: "var(--fw-medium)" as any,
    },
    danger: {
      background: "transparent",
      color: "var(--danger)",
      border: "1px solid color-mix(in srgb, var(--danger) 40%, transparent)",
      fontWeight: "var(--fw-medium)" as any,
    },
  };

  const [hover, setHover] = React.useState(false);
  const [active, setActive] = React.useState(false);

  const hoverFx: Record<Variant, React.CSSProperties> = {
    primary: { filter: "brightness(1.08)" },
    secondary: { filter: "brightness(1.05)" },
    ghost: { background: "var(--surface-3)", color: "var(--text-body)" },
    danger: { background: "color-mix(in srgb, var(--danger) 12%, transparent)" },
  };
  const appliedHover = !disabled && hover ? hoverFx[variant] : {};
  const appliedActive = !disabled && active ? { transform: "scale(0.97)" } : {};

  return (
    <button
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => {
        setHover(false);
        setActive(false);
      }}
      onMouseDown={() => setActive(true)}
      onMouseUp={() => setActive(false)}
      disabled={disabled}
      style={{
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        gap: "7px",
        width: full ? "100%" : "auto",
        borderRadius: "var(--btn-radius)",
        fontFamily: "var(--font-sans)",
        cursor: disabled ? "not-allowed" : "pointer",
        opacity: disabled ? 0.45 : 1,
        transition: "filter 120ms ease, transform 100ms ease, background 120ms ease",
        whiteSpace: "nowrap",
        ...sizes[size],
        ...variants[variant],
        ...appliedHover,
        ...appliedActive,
        ...style,
      }}
      {...rest}
    >
      {icon ? <span style={{ display: "inline-flex", fontSize: "1.1em" }}>{icon}</span> : null}
      {children}
    </button>
  );
}
