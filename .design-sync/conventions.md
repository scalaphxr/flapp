# Сборка звуков — Design System

Audio sample-management desktop app (Tauri). Warm-dark aesthetic — deep brown base (`#1C1815`) with coral accent (`#E8845C`).

## Theme

All components read from CSS custom properties. Wrap any composition in:

```html
<div data-theme="warm-dark">…</div>
```

The three themes are `warm-dark` (default), `dark`, and `light`. All previews use `warm-dark`.

## Tokens

| Category | Key tokens |
|---|---|
| Background | `--bg-base` (body), `--bg-raised`, `--surface-1`…`--surface-4` |
| Text | `--text-strong`, `--text-body`, `--text-muted`, `--text-faint` |
| Accent | `--accent` (coral `#E8845C`), `--accent-hover`, `--accent-muted` |
| Danger | `--danger`, `--danger-hover` |
| Border | `--border-soft`, `--border-subtle` |
| Spacing | `--space-1`…`--space-10` (4 px–80 px scale) |
| Radius | `--radius-sm`, `--radius-md`, `--radius-lg`, `--radius-pill` |
| Font | `--font-sans` (Geist), `--font-mono` (JetBrains Mono) |
| Category colors | `--cat-808`, `--cat-kick`, `--cat-snare`, `--cat-clap`, `--cat-hi-hat`, `--cat-open-hat`, `--cat-perc`, `--cat-vox`, `--cat-fx`, `--cat-loop`, `--cat-drum-loop` |

## Components

| Component | Use |
|---|---|
| `Button` | Actions — variants: `primary`, `secondary`, `ghost`, `danger`; sizes: `sm`, `md`, `lg` |
| `PlayButton` | Inline play/pause for sample rows — pass `playing` to toggle state |
| `Input` | Text inputs — optional `icon` prop for leading icon |
| `Checkbox` | Labeled boolean toggles — supports `disabled` |
| `CategoryTag` | Colored pill labels for sample categories — shows a colored dot by default |
| `Card` | Surface container — `elevated` variant, optional `padding` |
| `ProgressBar` | Operation progress — optional `caption` and `percent` display |
| `StatStrip` | Horizontal metadata strip — items can be strings or `{value, label, accent}` objects |
| `Tabs` | Navigation tab bar — items take optional `icon` |
| `DropZone` | Drag-and-drop import area — `active` state for drag-over |
| `Icons` | 29 SVG icons used throughout the app (see `Icons.*` namespace) |
