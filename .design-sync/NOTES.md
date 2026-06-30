# Flapp Design System — Sync Notes

## Repo facts
- Package: `flapp` (root package.json), components at `frontend/src/shared/ui/index.ts`
- Synth-entry mode: no dist build; converter reads TSX source directly via `--entry frontend/src/shared/ui/index.ts --node-modules frontend/node_modules --tsconfig frontend/tsconfig.json`
- `CategoryTag` imports `@/shared/config/categories` (tsconfig `@/` → `frontend/src/`); tsconfig path needed
- Fonts: Geist + Geist Mono loaded from Google Fonts via `<link>` in index.html — not shipped with bundle (`runtimeFontPrefixes`)

## Preview authoring
- All previews must wrap content in `<div data-theme="warm-dark">` so CSS custom properties resolve. Without this attribute, all color tokens (`--accent`, `--bg-base`, etc.) are undefined (defined in `[data-theme="warm-dark"]` block, not `:root`).
- Non-color tokens (font, spacing, radius, animation) ARE in `:root` and resolve without the wrapper.
- Warm-dark palette: bg-base `#1C1815`, accent `#E8845C` (coral), text-body `#E4D9CC`

## Known render warns
None — validate exits clean with no warnings on the final build.

## Re-sync command
```
node .ds-sync/package-build.mjs --config .design-sync/config.json --node-modules frontend/node_modules --entry frontend/src/shared/ui/index.ts --out ./ds-bundle
node .ds-sync/package-validate.mjs ./ds-bundle
```

## Re-sync risks
- Token additions/changes in `tokens.css` update automatically (`cssEntry`); if token names are renamed, update any preview files that reference them inline.
- `CategoryTag` imports `frontend/src/shared/config/categories.ts` via `@/` path alias — if `ALL_CATEGORIES` changes, rebuild; if the module is moved, update the path alias in `frontend/tsconfig.json`.
- Previews authored with warm-dark theme; other themes (dark, light) are not previewed — they share the same component code and differ only in color values.
- `Icons` component is a namespace (`window.Flapp.Icons.Sub.*`) — if new icons are added to `Icons.tsx`, the `AllIcons` preview will miss them until the preview is updated.
- `componentSrcMap` enumerates all 11 components explicitly (no `.d.ts` discovery). If a new component is added to `index.ts`, add it to `componentSrcMap` in config and author a preview.
- No `buildCmd` recorded: this project uses synth-entry (no dist build). The `--entry` flag is required on every resync; it's baked into the re-sync command above.
- `runtimeFontPrefixes` suppresses `[FONT_MISSING]` for Geist, Geist Mono, JetBrains Mono, SF Mono. These fonts are served via Google Fonts at runtime — verify they still load if the font loading strategy in `index.html` changes.
