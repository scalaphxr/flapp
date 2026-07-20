# Native Rewrite — Foundation (Rust + egui) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a native Rust/egui app skeleton — window, retained-state tab shell, terminal-core theme, a pure DSP crate extracted from `analyzer.rs`, and a native audio-playback crate — while keeping the existing Tauri+Go app fully runnable.

**Architecture:** New `native/` Cargo workspace with three crates: `flapp-dsp` (pure analysis, no Tauri), `flapp-audio` (rodio playback), `flapp-app` (eframe/egui binary). The existing `src-tauri/src/analyzer.rs` is refactored into a thin Tauri wrapper that calls `flapp-dsp`, so there is a single source of truth and the old app keeps working. Tab bodies are stubs in this sub-project.

**Tech Stack:** Rust (edition 2021), eframe/egui, rodio (cpal), symphonia, rustfft, rayon.

## Global Constraints

- **Rust edition 2021**, MSVC toolchain, Windows (win32) is the primary target.
- **Pin dependency versions.** After `cargo add`, record the exact resolved version in each `Cargo.toml` (no wildcard `*`). Scaffold the eframe app from the official example for the pinned eframe version so the `eframe::App` trait signature matches that version.
- **The existing Tauri+Go app MUST build and run after every task** (`npm run dev` from repo root, Player tab analyzes a file). This is a hard gate on Task 3.
- **`flapp-dsp` has zero Tauri dependencies.**
- **No Go and no Node in the new `flapp-app` binary.**
- TDD, DRY, YAGNI, frequent commits. Run all `cargo` commands from `native/` unless noted.

---

## File Structure

```
native/
├─ Cargo.toml                 # [workspace] members = flapp-dsp, flapp-audio, flapp-app
├─ flapp-dsp/
│  ├─ Cargo.toml
│  └─ src/lib.rs              # moved pure DSP from src-tauri/src/analyzer.rs
├─ flapp-audio/
│  ├─ Cargo.toml
│  └─ src/lib.rs              # Player + PositionTracker
└─ flapp-app/
   ├─ Cargo.toml
   └─ src/
      ├─ main.rs              # eframe entry, FlappApp
      ├─ theme.rs             # install_theme + terminal_visuals
      ├─ settings.rs          # Settings load/save
      └─ tabs.rs              # Tab enum + stub tab states
```
Modified: `src-tauri/Cargo.toml` (add path dep), `src-tauri/src/analyzer.rs` (becomes wrapper).

---

### Task 1: Native workspace scaffold + empty egui window

**Files:**
- Create: `native/Cargo.toml`, `native/flapp-dsp/Cargo.toml`, `native/flapp-dsp/src/lib.rs`, `native/flapp-audio/Cargo.toml`, `native/flapp-audio/src/lib.rs`, `native/flapp-app/Cargo.toml`, `native/flapp-app/src/main.rs`

**Interfaces:**
- Produces: a buildable workspace; `flapp_dsp` and `flapp_audio` library crates (empty), `flapp-app` binary.

- [ ] **Step 1: Create the workspace manifest**

`native/Cargo.toml`:
```toml
[workspace]
resolver = "2"
members = ["flapp-dsp", "flapp-audio", "flapp-app"]
```

- [ ] **Step 2: Create the two library crate stubs**

`native/flapp-dsp/Cargo.toml`:
```toml
[package]
name = "flapp-dsp"
version = "0.1.0"
edition = "2021"

[dependencies]
```
`native/flapp-dsp/src/lib.rs`:
```rust
#[cfg(test)]
mod tests {
    #[test]
    fn crate_builds() {
        assert_eq!(2 + 2, 4);
    }
}
```
`native/flapp-audio/Cargo.toml`:
```toml
[package]
name = "flapp-audio"
version = "0.1.0"
edition = "2021"

[dependencies]
```
`native/flapp-audio/src/lib.rs`:
```rust
#[cfg(test)]
mod tests {
    #[test]
    fn crate_builds() {
        assert_eq!(2 + 2, 4);
    }
}
```

- [ ] **Step 3: Create the app crate and add eframe**

`native/flapp-app/Cargo.toml`:
```toml
[package]
name = "flapp-app"
version = "0.1.0"
edition = "2021"

[dependencies]
```
Run (from `native/flapp-app`): `cargo add eframe egui`
Then edit `Cargo.toml` to replace any `*`/caret-only entries with the exact resolved versions shown in `Cargo.lock` (e.g. `eframe = "0.29"`). Record those exact versions.

- [ ] **Step 4: Write the minimal eframe entry point**

Open the eframe `hello_world` example for the pinned version (docs.rs or the eframe repo tag) and copy its `main` + `App` skeleton so the `App` trait method signature matches. Adapt to:

`native/flapp-app/src/main.rs`:
```rust
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
use eframe::egui;

fn main() -> eframe::Result {
    let options = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default()
            .with_inner_size([1200.0, 800.0])
            .with_min_inner_size([800.0, 600.0]),
        ..Default::default()
    };
    eframe::run_native("Flapp", options, Box::new(|_cc| Ok(Box::<FlappApp>::default())))
}

#[derive(Default)]
struct FlappApp {}

impl eframe::App for FlappApp {
    fn update(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        egui::CentralPanel::default().show(ctx, |ui| {
            ui.label("Flapp native — scaffold");
        });
    }
}
```
(If the pinned eframe version's `App` trait uses a different method name/signature than `update`, use the one from that version's example — the example is authoritative.)

- [ ] **Step 5: Build the workspace and the library tests**

Run (from `native/`): `cargo build`
Expected: compiles, no errors.
Run: `cargo test -p flapp-dsp -p flapp-audio`
Expected: 2 tests pass.

- [ ] **Step 6: Smoke-run the window**

Run (from `native/`): `cargo run -p flapp-app`
Expected: a native window opens showing "Flapp native — scaffold". Close it.

- [ ] **Step 7: Commit**

```bash
git add native/
git commit -m "feat(native): Cargo workspace scaffold + empty egui window"
```

---

### Task 2: Extract pure DSP into `flapp-dsp`

Move the pure analysis code out of `src-tauri/src/analyzer.rs` into `flapp-dsp`. This is a mechanical move (not a rewrite): copy the items listed below verbatim, then delete them from `analyzer.rs` in Task 3.

**Files:**
- Modify: `native/flapp-dsp/Cargo.toml`, `native/flapp-dsp/src/lib.rs`
- Source of truth to copy from: `src-tauri/src/analyzer.rs`

**Interfaces:**
- Produces (all `pub` from `flapp_dsp`):
  - `struct AudioMeta` (serde `Serialize`/`Deserialize`/`Clone`) — analysis result
  - `struct AnalyzerCache` with `fn new(dir: Option<std::path::PathBuf>) -> Self`, `fn get(&self, path: &str) -> Option<AudioMeta>`, `fn set(&self, path: &str, meta: AudioMeta)`, and `#[derive(Clone)]`
  - `fn analyze_one(path: &str) -> AudioMeta`
  - `fn probe_quick(path: &str) -> AudioMeta`
  - `fn scan_dir_recursive(dir: &str, results: &mut Vec<String>)`
  - `fn encode_wav(samples: &[f32], sr: u32, channels: u32) -> Vec<u8>`
  - `const ANALYSIS_VERSION: u32`

- [ ] **Step 1: Add flapp-dsp dependencies**

Determine the crates `analyzer.rs` uses by reading its `use` statements at the top of `src-tauri/src/analyzer.rs`. Add the same audio/DSP deps to `native/flapp-dsp/Cargo.toml` with the SAME versions as `src-tauri/Cargo.toml` (copy the exact version strings for `symphonia`, the FFT crate, `rayon`, `serde`). Example shape:
```toml
[dependencies]
serde = { version = "1", features = ["derive"] }
symphonia = { version = "0.5", default-features = false, features = ["<same features as src-tauri>"] }
rustfft = "<same as src-tauri>"
rayon = "<same as src-tauri>"
```

- [ ] **Step 2: Move the pure code into `flapp-dsp/src/lib.rs`**

From `src-tauri/src/analyzer.rs`, copy into `native/flapp-dsp/src/lib.rs` **everything except** the five `#[tauri::command]` functions (`player_get_dates`, `player_scan_folder`, `player_analyze_file`, `player_analyze_batch`, `player_decode_to_wav`) and any `use tauri::...` imports. Concretely, copy: `AudioMeta` (line ~32), `AnalysisProgress` (line ~59) — omit if it is only used by the batch command; `ANALYSIS_VERSION`, `cache_key`, `fnv1a`, `CacheEntry`, `AnalyzerCache` + impl, `probe_container`, `StreamedAudio`, `stream_peaks_and_dsp`, `compute_bpm`, `bpm_from_onset`, `ChromaFeatures`, `chroma_features`, `detect_key`, `match_key`, `key_scores`, `top_two`, `related_weight`, `pearson_correlation`, `normalize_bpm`, `bpm_from_filename`, `probe_quick`, `analyze_one`, `encode_wav`, `is_audio_ext`, `scan_dir_recursive`, `scan_dir_bounded`, and the entire `#[cfg(test)] mod tests`.

Make these `pub`: `AudioMeta`, `AnalyzerCache` (+ its `new`/`get`/`set` methods), `analyze_one`, `probe_quick`, `scan_dir_recursive`, `encode_wav`, `ANALYSIS_VERSION`. Leave the rest private. Replace the removed `mod tests` stub from Task 1 with the moved test module.

- [ ] **Step 3: Fix imports and build**

Add the needed `use std::...` imports at the top of `lib.rs` (e.g. `std::path::{Path, PathBuf}`, `std::fs`, `std::time::SystemTime`, `std::sync::...`) as required by the moved code. Remove any `AppHandle`/`State`/`emit` references (those live only in the command wrappers, which are NOT moved).

Run (from `native/`): `cargo build -p flapp-dsp`
Expected: compiles. Fix any missing-import errors until clean.

- [ ] **Step 4: Run the ported DSP tests**

Run: `cargo test -p flapp-dsp`
Expected: the moved analyzer tests (click-track BPM, scale/key detection, `bpm_from_filename`) pass.

- [ ] **Step 5: Commit**

```bash
git add native/flapp-dsp/
git commit -m "feat(flapp-dsp): extract pure audio analysis from analyzer.rs"
```

---

### Task 3: Rewire `analyzer.rs` as a thin wrapper over `flapp-dsp` (keep old app working)

**Files:**
- Modify: `src-tauri/Cargo.toml`, `src-tauri/src/analyzer.rs`

**Interfaces:**
- Consumes: everything `flapp_dsp` produces (Task 2).
- Produces: unchanged Tauri command surface — `player_get_dates`, `player_scan_folder`, `player_analyze_file`, `player_analyze_batch`, `player_decode_to_wav`, and `AnalyzerCache` as Tauri managed state (now re-exported from `flapp_dsp`). `lib.rs` registrations in `generate_handler!` stay byte-for-byte identical.

- [ ] **Step 1: Add the path dependency**

In `src-tauri/Cargo.toml` `[dependencies]` add:
```toml
flapp-dsp = { path = "../native/flapp-dsp" }
```

- [ ] **Step 2: Replace the moved bodies with re-use**

Edit `src-tauri/src/analyzer.rs`: delete every item that was moved in Task 2 (the whole file except the five `#[tauri::command]` functions and the file's imports). At the top add:
```rust
use flapp_dsp::{analyze_one, probe_quick, scan_dir_recursive, encode_wav, AudioMeta, AnalyzerCache};
```
Keep `AnalysisProgress` here (it is only used by `player_analyze_batch`'s event emit). The command bodies already call `analyze_one`, `probe_quick`, `scan_dir_recursive`, `AudioMeta`, `AnalyzerCache` — they now resolve to the `flapp_dsp` imports. `player_decode_to_wav` calls `encode_wav` from the import. Ensure `lib.rs`'s `app.manage(analyzer::AnalyzerCache::new(cache_dir))` still resolves (re-export: add `pub use flapp_dsp::AnalyzerCache;` in `analyzer.rs` if `lib.rs` refers to `analyzer::AnalyzerCache`).

- [ ] **Step 3: Build the old app crate**

Run (from `src-tauri/`): `cargo build`
Expected: compiles with no errors. Fix import/visibility mismatches until clean.

- [ ] **Step 4: Verify the old app still runs (HARD GATE — Global Constraint)**

Stop any running dev app first (kill `flapp.exe` and `flapp-core.exe` by PID). Then run (from repo root): `npm run dev`
Expected: the Tauri window opens; on the Player tab, adding a folder/file analyzes it (BPM/key/waveform appear) exactly as before. If anything regressed, fix before committing.

- [ ] **Step 5: Commit**

```bash
git add src-tauri/Cargo.toml src-tauri/src/analyzer.rs
git commit -m "refactor(analyzer): call flapp-dsp; analyzer.rs is now a thin Tauri wrapper"
```

---

### Task 4: `flapp-audio` — native playback with position tracking

**Files:**
- Modify: `native/flapp-audio/Cargo.toml`
- Modify: `native/flapp-audio/src/lib.rs`

**Interfaces:**
- Produces:
  - `struct PositionTracker` with `fn new(duration_sec: f32) -> Self`, `fn start(&mut self)`, `fn pause(&mut self)`, `fn seek_to(&mut self, secs: f32)`, `fn position(&self) -> f32` (clamped to `[0, duration_sec]`)
  - `struct Player` with `fn new() -> anyhow::Result<Self>`, `fn play(&mut self, path: &Path, duration_sec: f32) -> anyhow::Result<()>`, `fn pause(&self)`, `fn resume(&self)`, `fn stop(&mut self)`, `fn seek(&self, secs: f32) -> anyhow::Result<()>`, `fn position(&self) -> f32`, `fn is_playing(&self) -> bool`

- [ ] **Step 1: Write the failing test for PositionTracker**

Append to `native/flapp-audio/src/lib.rs`:
```rust
#[cfg(test)]
mod pos_tests {
    use super::*;
    use std::{thread, time::Duration};

    #[test]
    fn position_advances_from_zero_and_clamps() {
        let mut t = PositionTracker::new(2.0);
        assert_eq!(t.position(), 0.0);
        t.start();
        thread::sleep(Duration::from_millis(50));
        let p = t.position();
        assert!(p > 0.0 && p < 2.0, "expected 0<p<2, got {p}");
    }

    #[test]
    fn seek_sets_position_and_clamps_to_duration() {
        let mut t = PositionTracker::new(2.0);
        t.seek_to(1.5);
        assert!((t.position() - 1.5).abs() < 1e-6);
        t.seek_to(99.0);
        assert_eq!(t.position(), 2.0);
        t.seek_to(-5.0);
        assert_eq!(t.position(), 0.0);
    }

    #[test]
    fn pause_freezes_position() {
        let mut t = PositionTracker::new(10.0);
        t.seek_to(3.0);
        t.start();
        t.pause();
        let a = t.position();
        thread::sleep(Duration::from_millis(30));
        assert_eq!(a, t.position());
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run (from `native/`): `cargo test -p flapp-audio pos_tests`
Expected: FAIL — `PositionTracker` not found.

- [ ] **Step 3: Implement PositionTracker**

At the top of `native/flapp-audio/src/lib.rs`:
```rust
use std::time::Instant;

/// Tracks playback position against a known (probed) duration. Position is
/// derived from an anchor offset plus elapsed wall time while playing, clamped
/// to [0, duration]. Uses probed duration, not decoder-reported (VBR mp3 lies).
pub struct PositionTracker {
    duration_sec: f32,
    anchor_sec: f32,        // position at the last start/seek
    playing_since: Option<Instant>,
}

impl PositionTracker {
    pub fn new(duration_sec: f32) -> Self {
        Self { duration_sec: duration_sec.max(0.0), anchor_sec: 0.0, playing_since: None }
    }
    pub fn start(&mut self) {
        if self.playing_since.is_none() { self.playing_since = Some(Instant::now()); }
    }
    pub fn pause(&mut self) {
        self.anchor_sec = self.position();
        self.playing_since = None;
    }
    pub fn seek_to(&mut self, secs: f32) {
        self.anchor_sec = secs.clamp(0.0, self.duration_sec);
        if self.playing_since.is_some() { self.playing_since = Some(Instant::now()); }
    }
    pub fn position(&self) -> f32 {
        let elapsed = self.playing_since.map(|t| t.elapsed().as_secs_f32()).unwrap_or(0.0);
        (self.anchor_sec + elapsed).clamp(0.0, self.duration_sec)
    }
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run (from `native/`): `cargo test -p flapp-audio pos_tests`
Expected: 3 tests PASS.

- [ ] **Step 5: Add rodio and implement Player**

Run (from `native/flapp-audio`): `cargo add rodio anyhow`
Pin the resolved versions in `Cargo.toml`.

Add to `native/flapp-audio/src/lib.rs`:
```rust
use std::path::Path;
use std::io::BufReader;
use std::fs::File;
use std::sync::Mutex;
use rodio::{Decoder, OutputStream, OutputStreamHandle, Sink};

/// Native audio player. Owns the output stream + a rodio Sink and mirrors
/// playback position with a PositionTracker (probed duration is authoritative).
pub struct Player {
    _stream: OutputStream,
    handle: OutputStreamHandle,
    sink: Option<Sink>,
    tracker: Mutex<PositionTracker>,
}

impl Player {
    pub fn new() -> anyhow::Result<Self> {
        let (_stream, handle) = OutputStream::try_default()?;
        Ok(Self { _stream, handle, sink: None, tracker: Mutex::new(PositionTracker::new(0.0)) })
    }

    pub fn play(&mut self, path: &Path, duration_sec: f32) -> anyhow::Result<()> {
        let sink = Sink::try_new(&self.handle)?;
        let file = BufReader::new(File::open(path)?);
        let source = Decoder::new(file)?;
        sink.append(source);
        sink.play();
        self.sink = Some(sink);
        let mut t = self.tracker.lock().unwrap();
        *t = PositionTracker::new(duration_sec);
        t.start();
        Ok(())
    }

    pub fn pause(&self) {
        if let Some(s) = &self.sink { s.pause(); }
        self.tracker.lock().unwrap().pause();
    }
    pub fn resume(&self) {
        if let Some(s) = &self.sink { s.play(); }
        self.tracker.lock().unwrap().start();
    }
    pub fn stop(&mut self) {
        if let Some(s) = self.sink.take() { s.stop(); }
    }
    pub fn seek(&self, secs: f32) -> anyhow::Result<()> {
        if let Some(s) = &self.sink {
            // rodio >=0.17: Sink::try_seek. If Unsupported, position still tracks.
            let _ = s.try_seek(std::time::Duration::from_secs_f32(secs));
        }
        self.tracker.lock().unwrap().seek_to(secs);
        Ok(())
    }
    pub fn position(&self) -> f32 { self.tracker.lock().unwrap().position() }
    pub fn is_playing(&self) -> bool {
        self.sink.as_ref().map(|s| !s.is_paused() && !s.empty()).unwrap_or(false)
    }
}
```
(If the pinned rodio version's `try_seek`/`OutputStream::try_default` signatures differ, adapt to that version's API — the position tracker is independent of that.)

- [ ] **Step 6: Build and test**

Run (from `native/`): `cargo build -p flapp-audio && cargo test -p flapp-audio`
Expected: compiles; pos_tests still pass.

- [ ] **Step 7: Manual smoke (optional but recommended)**

Temporarily add a `#[test] #[ignore]` that plays a real file for ~300ms, or verify later in Task 6. Not required to pass CI.

- [ ] **Step 8: Commit**

```bash
git add native/flapp-audio/
git commit -m "feat(flapp-audio): rodio Player + position tracking against probed duration"
```

---

### Task 5: Terminal-core egui theme

**Files:**
- Create: `native/flapp-app/src/theme.rs`
- Modify: `native/flapp-app/src/main.rs`

**Interfaces:**
- Produces: `pub fn terminal_visuals() -> egui::Visuals`, `pub fn install_theme(ctx: &egui::Context)`

- [ ] **Step 1: Write the failing test for the palette**

`native/flapp-app/src/theme.rs`:
```rust
use eframe::egui::{self, Color32, Visuals};

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn visuals_use_terminal_core_palette() {
        let v = terminal_visuals();
        assert!(v.dark_mode);
        assert_eq!(v.panel_fill, Color32::from_rgb(0, 0, 0));           // --surface-1
        assert_eq!(v.extreme_bg_color, Color32::from_rgb(0x14, 0x14, 0x14)); // --surface-3
        assert_eq!(v.override_text_color, Some(Color32::from_rgb(0xff, 0xff, 0xff)));
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Add `mod theme;` to `main.rs`. Run (from `native/`): `cargo test -p flapp-app visuals_use_terminal_core_palette`
Expected: FAIL — `terminal_visuals` not found.

- [ ] **Step 3: Implement the theme**

Append to `native/flapp-app/src/theme.rs`:
```rust
/// tokens.css -> egui Visuals. Black panels, white text, grey borders, white
/// selection. Mirrors frontend/src/app/styles/tokens.css.
pub fn terminal_visuals() -> Visuals {
    let mut v = Visuals::dark();
    v.panel_fill = Color32::from_rgb(0, 0, 0);                 // --surface-1
    v.window_fill = Color32::from_rgb(0, 0, 0);
    v.faint_bg_color = Color32::from_rgb(0x0a, 0x0a, 0x0a);    // --surface-2 (row zebra)
    v.extreme_bg_color = Color32::from_rgb(0x14, 0x14, 0x14);  // --surface-3 (inputs)
    v.override_text_color = Some(Color32::from_rgb(0xff, 0xff, 0xff)); // --text-strong
    v.selection.bg_fill = Color32::from_rgb(0x33, 0x33, 0x33);
    v.selection.stroke.color = Color32::from_rgb(0xff, 0xff, 0xff); // --accent
    let border = Color32::from_rgb(0x3a, 0x3a, 0x3a);          // --border-medium
    for w in [&mut v.widgets.noninteractive, &mut v.widgets.inactive,
              &mut v.widgets.hovered, &mut v.widgets.active] {
        w.bg_stroke.color = border;
    }
    v
}

/// Waveform base colour (--wave-dim). Played part uses white (--accent).
pub const WAVE_DIM: Color32 = Color32::from_rgb(0x66, 0x66, 0x66);

pub fn install_theme(ctx: &egui::Context) {
    ctx.set_visuals(terminal_visuals());
    let mut fonts = egui::FontDefinitions::default();
    // Make Monospace the default proportional family too (terminal-core is mono).
    if let Some(mono) = fonts.families.get(&egui::FontFamily::Monospace).cloned() {
        fonts.families.insert(egui::FontFamily::Proportional, mono);
    }
    ctx.set_fonts(fonts);
}
```
(egui ships a built-in monospace font, so no external font file is required for the Foundation. Bundling a specific `.ttf` via `FontData::from_static` is a later polish task.)

- [ ] **Step 4: Run the test to verify it passes**

Run (from `native/`): `cargo test -p flapp-app visuals_use_terminal_core_palette`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add native/flapp-app/
git commit -m "feat(app): terminal-core egui theme (tokens.css -> Visuals)"
```

---

### Task 6: App shell — retained tabs, stubs, theme + audio wired

**Files:**
- Create: `native/flapp-app/src/tabs.rs`
- Modify: `native/flapp-app/src/main.rs`

**Interfaces:**
- Consumes: `theme::install_theme` (Task 5), `flapp_audio::Player` (Task 4).
- Produces: `enum Tab { Sounds, Player, Settings }` (derives `PartialEq`, `Eq`, `Clone`, `Copy`); `struct FlappApp` holding one state struct per tab; stub states each carry a mutable field to prove retention.

- [ ] **Step 1: Write the failing test for state retention**

`native/flapp-app/src/tabs.rs`:
```rust
#[derive(PartialEq, Eq, Clone, Copy)]
pub enum Tab { Sounds, Player, Settings }

#[derive(Default)]
pub struct SoundsTabState { pub scratch: String }
#[derive(Default)]
pub struct PlayerTabState { pub scratch: String }
#[derive(Default)]
pub struct SettingsTabState { pub scratch: String }

#[cfg(test)]
mod tests {
    use super::*;
    // The stub states are plain structs held by FlappApp across frames; switching
    // the active Tab must not touch other tabs' fields. This models that: mutate
    // one, switch, and confirm it persists.
    #[test]
    fn switching_tabs_preserves_other_tab_state() {
        let mut sounds = SoundsTabState::default();
        let mut player = PlayerTabState::default();
        let mut active = Tab::Sounds;
        sounds.scratch = "typed in sounds".to_string();
        active = Tab::Player;                 // switch away
        player.scratch = "typed in player".to_string();
        active = Tab::Sounds;                 // switch back
        assert_eq!(active, Tab::Sounds);
        assert_eq!(sounds.scratch, "typed in sounds");
        assert_eq!(player.scratch, "typed in player");
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Add `mod tabs;` to `main.rs`. Run (from `native/`): `cargo test -p flapp-app switching_tabs_preserves_other_tab_state`
Expected: FAIL — `tabs` module/types not found (until `mod tabs;` added and file saved), then PASS-able logic. If it compiles and passes immediately after adding the module, that is acceptable (the test documents the invariant).

- [ ] **Step 3: Implement stub tab UIs**

Append to `native/flapp-app/src/tabs.rs`:
```rust
use eframe::egui;

impl SoundsTabState {
    pub fn ui(&mut self, ui: &mut egui::Ui) {
        ui.heading("SOUNDS");
        ui.label("TODO: sound library (Sub-project 2)");
        ui.text_edit_singleline(&mut self.scratch);
    }
}
impl PlayerTabState {
    pub fn ui(&mut self, ui: &mut egui::Ui, _audio: &mut flapp_audio::Player) {
        ui.heading("PLAYER");
        ui.label("TODO: player (Sub-project 1)");
        ui.text_edit_singleline(&mut self.scratch);
    }
}
impl SettingsTabState {
    pub fn ui(&mut self, ui: &mut egui::Ui) {
        ui.heading("SETTINGS");
        ui.label("TODO: settings (Sub-project 3)");
        ui.text_edit_singleline(&mut self.scratch);
    }
}
```

- [ ] **Step 4: Rewrite `main.rs` to compose the shell**

`native/flapp-app/src/main.rs`:
```rust
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
use eframe::egui;

mod theme;
mod tabs;
mod settings;

use tabs::{Tab, SoundsTabState, PlayerTabState, SettingsTabState};

fn main() -> eframe::Result {
    let options = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default()
            .with_inner_size([1200.0, 800.0])
            .with_min_inner_size([800.0, 600.0]),
        ..Default::default()
    };
    eframe::run_native("Flapp", options, Box::new(|cc| {
        theme::install_theme(&cc.egui_ctx);
        Ok(Box::new(FlappApp::new()))
    }))
}

struct FlappApp {
    tab: Tab,
    sounds: SoundsTabState,
    player: PlayerTabState,
    settings: SettingsTabState,
    audio: flapp_audio::Player,
}

impl FlappApp {
    fn new() -> Self {
        Self {
            tab: Tab::Sounds,
            sounds: Default::default(),
            player: Default::default(),
            settings: Default::default(),
            audio: flapp_audio::Player::new().expect("audio output"),
        }
    }
}

impl eframe::App for FlappApp {
    fn update(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        egui::TopBottomPanel::top("tabs").show(ctx, |ui| {
            ui.horizontal(|ui| {
                ui.selectable_value(&mut self.tab, Tab::Sounds, "SOUNDS");
                ui.selectable_value(&mut self.tab, Tab::Player, "PLAYER");
                ui.selectable_value(&mut self.tab, Tab::Settings, "SETTINGS");
            });
        });
        egui::CentralPanel::default().show(ctx, |ui| match self.tab {
            Tab::Sounds => self.sounds.ui(ui),
            Tab::Player => self.player.ui(ui, &mut self.audio),
            Tab::Settings => self.settings.ui(ui),
        });
    }
}
```
Add `flapp-audio` as a dependency: run (from `native/flapp-app`) `cargo add flapp-audio --path ../flapp-audio`.

- [ ] **Step 5: Build, test, smoke-run**

Run (from `native/`): `cargo test -p flapp-app`
Expected: theme + tabs tests PASS.
Run: `cargo run -p flapp-app`
Expected: black window, mono white text, three tabs SOUNDS/PLAYER/SETTINGS. Type into the SOUNDS text field, switch to PLAYER and back — the SOUNDS text is still there (state retained). Close.

- [ ] **Step 6: Commit**

```bash
git add native/flapp-app/
git commit -m "feat(app): retained-state tab shell with stub tabs, themed, audio wired"
```

---

### Task 7: Settings load/save compatible with existing `settings.json`

**Files:**
- Create: `native/flapp-app/src/settings.rs`

**Interfaces:**
- Produces: `pub struct Settings` (serde, all fields `#[serde(default)]`), `pub fn config_path() -> std::path::PathBuf`, `pub fn load() -> Settings`, `pub fn save(s: &Settings) -> std::io::Result<()>`

- [ ] **Step 1: Write the failing test (round-trip through a temp file)**

`native/flapp-app/src/settings.rs`:
```rust
use serde::{Deserialize, Serialize};
use std::path::PathBuf;

#[derive(Serialize, Deserialize, Clone, Default)]
pub struct Settings {
    #[serde(default)]
    pub language: String,
    #[serde(default)]
    pub theme: String,
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn round_trips_and_tolerates_unknown_fields() {
        // Existing settings.json has many more keys; unknown keys must not break load.
        let json = r#"{"language":"ru","theme":"warm-dark","ytNickname":"x","workers":4}"#;
        let s: Settings = serde_json::from_str(json).unwrap();
        assert_eq!(s.language, "ru");
        assert_eq!(s.theme, "warm-dark");
        let back = serde_json::to_string(&s).unwrap();
        let s2: Settings = serde_json::from_str(&back).unwrap();
        assert_eq!(s2.language, "ru");
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run (from `native/flapp-app`): `cargo add serde --features derive` and `cargo add serde_json dirs`. Pin versions.
Run (from `native/`): `cargo test -p flapp-app round_trips_and_tolerates_unknown_fields`
Expected: FAIL until `serde_json` is present, then PASS on the deserialize logic.

- [ ] **Step 3: Implement path + load/save**

Append to `native/flapp-app/src/settings.rs`:
```rust
pub fn config_path() -> PathBuf {
    // Matches the Go/Tauri app: os.UserConfigDir()/flapp/settings.json
    let base = dirs::config_dir().unwrap_or_else(|| PathBuf::from("."));
    base.join("flapp").join("settings.json")
}

pub fn load() -> Settings {
    match std::fs::read_to_string(config_path()) {
        Ok(text) => serde_json::from_str(&text).unwrap_or_default(),
        Err(_) => Settings::default(),
    }
}

pub fn save(s: &Settings) -> std::io::Result<()> {
    let path = config_path();
    if let Some(dir) = path.parent() { std::fs::create_dir_all(dir)?; }
    let text = serde_json::to_string_pretty(s).unwrap();
    std::fs::write(path, text)
}
```
Note: `load` uses `unwrap_or_default()` so a malformed or partial file never panics. Foundation does not write settings yet (no tab needs it); `save` exists for later sub-projects.

- [ ] **Step 4: Run the test to verify it passes**

Run (from `native/`): `cargo test -p flapp-app`
Expected: all app tests PASS (theme, tabs, settings).

- [ ] **Step 5: Commit**

```bash
git add native/flapp-app/
git commit -m "feat(app): settings load/save compatible with existing settings.json"
```

---

## Definition of Done (verify after Task 7)

- `cargo run -p flapp-app` (from `native/`) opens a black/white/mono native window with 3 switchable tabs; typing in one tab and switching away and back preserves the text (retained state).
- `cargo test` (from `native/`) — all tests pass across the three crates.
- The old Tauri+Go app still builds and runs (`npm run dev`), Player analyzes a file (verified in Task 3).
- The new binary links no Go and no Node.

## Self-Review Notes (spec coverage)

- Crate layout (`flapp-dsp`/`flapp-audio`/`flapp-app`) → Tasks 1,2,4,5,6,7. ✓
- DSP extract + old `analyzer.rs` becomes wrapper → Tasks 2,3. ✓
- Retained tab shell (kills tab-switch pain) → Task 6. ✓
- terminal-core theme + token mapping → Task 5. ✓
- Playback with probed-duration position (VBR gotcha) → Task 4. ✓
- Settings compatible with existing json, SQLite deferred → Task 7. ✓
- "Old app keeps working" hard gate → Task 3 Step 4. ✓
- Out-of-scope (tab content, SQLite, watcher, YouTube/covers, packaging) → not included. ✓
