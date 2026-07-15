import type { SVGProps } from "react";

// Line icons in the design's style: 24-grid, currentColor, ~2px stroke. The
// first block is ported verbatim from the design kit; the rest are new icons
// the fuller app needs, drawn to match.

type P = SVGProps<SVGSVGElement>;
const base = {
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 2,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

export const Icons = {
  Wave: (p: P) => (
    <svg width="16" height="16" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M2 12h2l2-7 4 16 3-13 2 6 1-2h6" />
    </svg>
  ),
  Tool: (p: P) => (
    <svg width="16" height="16" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18v3h3l6.3-6.3a4 4 0 0 0 5.4-5.4l-2.6 2.6-2-2 2.6-2.6Z" />
    </svg>
  ),
  Gear: (p: P) => (
    <svg width="16" height="16" viewBox="0 0 24 24" {...base} {...p}>
      <circle cx="12" cy="12" r="3.2" />
      <path d="M19.4 15a1.6 1.6 0 0 0 .3 1.8 2 2 0 1 1-2.6 2.6 1.6 1.6 0 0 0-1.8-.3 1.6 1.6 0 0 0-1 1.5V21a2 2 0 0 1-4 0 1.6 1.6 0 0 0-1.1-1.5 1.6 1.6 0 0 0-1.8.4 2 2 0 1 1-2.6-2.6 1.6 1.6 0 0 0 .3-1.8 1.6 1.6 0 0 0-1.5-1H3a2 2 0 0 1 0-4 1.6 1.6 0 0 0 1.5-1.1 1.6 1.6 0 0 0-.4-1.8 2 2 0 1 1 2.6-2.6 1.6 1.6 0 0 0 1.8.3H9a1.6 1.6 0 0 0 1-1.5V3a2 2 0 0 1 4 0 1.6 1.6 0 0 0 1 1.5 1.6 1.6 0 0 0 1.8-.3 2 2 0 1 1 2.6 2.6 1.6 1.6 0 0 0-.3 1.8V9a1.6 1.6 0 0 0 1.5 1H21a2 2 0 0 1 0 4 1.6 1.6 0 0 0-1.5 1Z" />
    </svg>
  ),
  Search: (p: P) => (
    <svg width="16" height="16" viewBox="0 0 24 24" {...base} strokeWidth={2.2} {...p}>
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.2-3.2" />
    </svg>
  ),
  Folder: (p: P) => (
    <svg width="15" height="15" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z" />
    </svg>
  ),
  Zip: (p: P) => (
    <svg width="15" height="15" viewBox="0 0 24 24" {...base} {...p}>
      <rect x="4" y="3" width="16" height="18" rx="2" />
      <path d="M12 3v4M10 7h2M12 9v2M10 11h2" />
    </svg>
  ),
  Flp: (p: P) => (
    <svg width="15" height="15" viewBox="0 0 24 24" {...base} {...p}>
      <rect x="4" y="3" width="16" height="18" rx="2" />
      <path d="M8 8h8M8 12h8M8 16h5" />
    </svg>
  ),
  Audio: (p: P) => (
    <svg width="15" height="15" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M9 18V6l10-2v12" />
      <circle cx="6" cy="18" r="3" />
      <circle cx="16" cy="16" r="3" />
    </svg>
  ),
  Trash: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2m2 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
    </svg>
  ),
  Plus: (p: P) => (
    <svg width="15" height="15" viewBox="0 0 24 24" {...base} strokeWidth={2.2} {...p}>
      <path d="M12 5v14M5 12h14" />
    </svg>
  ),
  Save: (p: P) => (
    <svg width="16" height="16" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2Z" />
      <path d="M17 21v-8H7v8M7 3v5h8" />
    </svg>
  ),
  Info: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" {...base} {...p}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 16v-4M12 8h.01" />
    </svg>
  ),
  Stop: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" {...p}>
      <rect x="6" y="6" width="12" height="12" rx="2" />
    </svg>
  ),
  Heart: (p: P) => (
    <svg width="15" height="15" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M12 20s-7-4.4-9.2-8.6A4.8 4.8 0 0 1 12 6.2 4.8 4.8 0 0 1 21.2 11.4C19 15.6 12 20 12 20Z" />
    </svg>
  ),
  Star: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" {...base} {...p}>
      <path d="m12 3 2.7 5.6 6.1.9-4.4 4.3 1 6.1L12 17.8 6.6 20l1-6.1L3.2 9.5l6.1-.9L12 3Z" />
    </svg>
  ),
  Wand: (p: P) => (
    <svg width="16" height="16" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M15 4V2M15 10V8M11 6H9M21 6h-2M18.4 3.4l-1.4 1.4M18.4 8.6l-1.4-1.4" />
      <path d="m3 21 9-9" />
    </svg>
  ),
  Box: (p: P) => (
    <svg width="16" height="16" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M21 8 12 3 3 8v8l9 5 9-5V8Z" />
      <path d="m3 8 9 5 9-5M12 13v8" />
    </svg>
  ),
  Chart: (p: P) => (
    <svg width="16" height="16" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M3 3v18h18" />
      <path d="M7 14v3M12 9v8M17 5v12" />
    </svg>
  ),
  Pencil: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M17 3a2.83 2.83 0 0 1 4 4L7.5 20.5 2 22l1.5-5.5Z" />
    </svg>
  ),
  X: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" {...base} strokeWidth={2.2} {...p}>
      <path d="M6 6l12 12M18 6 6 18" />
    </svg>
  ),
  Check: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" {...base} strokeWidth={2.4} {...p}>
      <path d="M4 12.5 9 17.5 20 6.5" />
    </svg>
  ),
  Music: (p: P) => (
    <svg width="15" height="15" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M9 18V5l12-2v13" />
      <circle cx="6" cy="18" r="3" />
      <circle cx="18" cy="16" r="3" />
    </svg>
  ),
  Yt: (p: P) => (
    <svg width="15" height="15" viewBox="0 0 24 24" {...base} {...p}>
      <rect x="2" y="5" width="20" height="14" rx="4" />
      <polygon points="10,9 15,12 10,15" fill="currentColor" stroke="none" />
    </svg>
  ),
  Play: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" {...p}>
      <polygon points="5,3 19,12 5,21" />
    </svg>
  ),
  Pause: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" {...p}>
      <rect x="5" y="3" width="4" height="18" rx="1" />
      <rect x="15" y="3" width="4" height="18" rx="1" />
    </svg>
  ),
  SkipBack: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" {...base} {...p}>
      <polygon points="19,20 9,12 19,4" />
      <line x1="5" y1="4" x2="5" y2="20" />
    </svg>
  ),
  SkipFwd: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" {...base} {...p}>
      <polygon points="5,4 15,12 5,20" />
      <line x1="19" y1="4" x2="19" y2="20" />
    </svg>
  ),
  Volume: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" {...base} {...p}>
      <polygon points="11,5 6,9 2,9 2,15 6,15 11,19" />
      <path d="M15.54 8.46a5 5 0 0 1 0 7.07M19.07 4.93a10 10 0 0 1 0 14.14" />
    </svg>
  ),
  // Пианино-клавиатура — иконка MIDI вкладки.
  Midi: (p: P) => (
    <svg width="16" height="16" viewBox="0 0 24 24" {...base} {...p}>
      <rect x="2" y="6" width="20" height="12" rx="1" />
      <path d="M7 6v7M12 6v7M17 6v7" strokeWidth={1.5} />
      <rect x="5" y="6" width="3" height="5" rx="0.5" fill="currentColor" stroke="none" />
      <rect x="10" y="6" width="3" height="5" rx="0.5" fill="currentColor" stroke="none" />
      <rect x="15" y="6" width="3" height="5" rx="0.5" fill="currentColor" stroke="none" />
    </svg>
  ),
  Download: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" {...base} {...p}>
      <path d="M12 3v13M7 11l5 5 5-5" />
      <path d="M3 19h18" />
    </svg>
  ),
  ChevronDown: (p: P) => (
    <svg width="14" height="14" viewBox="0 0 24 24" {...base} {...p}>
      <polyline points="6 9 12 15 18 9" />
    </svg>
  ),
  Dedup: (p: P) => (
    <svg width="16" height="16" viewBox="0 0 24 24" {...base} {...p}>
      <rect x="3" y="3" width="10" height="13" rx="2" />
      <rect x="7" y="7" width="10" height="13" rx="2" />
      <path d="M17 7l3 3-3 3M20 10H13" />
    </svg>
  ),
};

export type IconName = keyof typeof Icons;
