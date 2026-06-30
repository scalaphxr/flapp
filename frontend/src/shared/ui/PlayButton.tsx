import React from "react";

interface PlayButtonProps {
  playing?: boolean;
  size?: number;
  onClick?: (e: React.MouseEvent<HTMLButtonElement>) => void;
  style?: React.CSSProperties;
}

// Circular ghost button with a warm filled triangle; fills coral on hover and
// swaps to a pause glyph when playing.
export function PlayButton({ playing = false, size = 36, onClick, style = {} }: PlayButtonProps) {
  const [hover, setHover] = React.useState(false);
  return (
    <button
      onClick={onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      aria-label={playing ? "Stop" : "Play"}
      style={{
        width: size,
        height: size,
        borderRadius: "var(--radius-pill)",
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        cursor: "pointer",
        border: "1px solid",
        borderColor: playing || hover ? "transparent" : "var(--border-medium)",
        background: playing ? "var(--accent)" : hover ? "var(--accent-soft)" : "transparent",
        color: playing ? "var(--text-on-accent)" : "var(--accent)",
        transition: "var(--transition-base)",
        transform: hover && !playing ? "scale(1.06)" : "scale(1)",
        ...style,
      }}
    >
      {playing ? (
        <svg width={size * 0.34} height={size * 0.34} viewBox="0 0 10 10" fill="currentColor">
          <rect x="1" y="1" width="3" height="8" rx="1" />
          <rect x="6" y="1" width="3" height="8" rx="1" />
        </svg>
      ) : (
        <svg width={size * 0.36} height={size * 0.36} viewBox="0 0 10 12" fill="currentColor">
          <path d="M1 1.4c0-.8.86-1.3 1.55-.9l7 4.6c.66.43.66 1.4 0 1.82l-7 4.6c-.69.4-1.55-.1-1.55-.9V1.4Z" />
        </svg>
      )}
    </button>
  );
}
