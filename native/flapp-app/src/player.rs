// Вкладка Player: подбор папки → сканирование → параллельный анализ (BPM/key/
// волна через flapp-dsp) с потоковым обновлением строк → виртуализированная
// таблица, транспорт и кликабельная волна. DSP уже готов в flapp-dsp; здесь —
// только нативный UI + оркестрация потоков.

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::mpsc::{channel, Receiver};
use std::sync::Arc;
use std::thread;

use eframe::egui;
use egui_extras::{Column, TableBuilder};

use flapp_audio::Player;
use flapp_dsp::{analyze_one, probe_quick, scan_dir_recursive, AnalyzerCache, AudioMeta};

use crate::theme::WAVE_DIM;

pub struct PlayerTabState {
    folder: Option<PathBuf>,
    tracks: Vec<AudioMeta>,
    index: HashMap<String, usize>, // path -> tracks idx
    cache: Arc<AnalyzerCache>,
    rx: Option<Receiver<AudioMeta>>,
    selected: Option<usize>,
    playing: Option<String>, // path currently loaded in the audio player
}

impl Default for PlayerTabState {
    fn default() -> Self {
        let dir = dirs::cache_dir().map(|d| d.join("flapp").join("analysis"));
        Self {
            folder: None,
            tracks: Vec::new(),
            index: HashMap::new(),
            cache: Arc::new(AnalyzerCache::new(dir)),
            rx: None,
            selected: None,
            playing: None,
        }
    }
}

impl PlayerTabState {
    /// Count of tracks still awaiting full analysis (quick-probe rows).
    fn pending(&self) -> usize {
        self.tracks.iter().filter(|t| t.partial == Some(true)).count()
    }

    fn pick_folder(&mut self) {
        if let Some(dir) = rfd::FileDialog::new().pick_folder() {
            self.start_scan(dir);
        }
    }

    /// Reset state and kick a background worker: scan → quick probes → full
    /// analysis, all streamed back over a channel.
    fn start_scan(&mut self, dir: PathBuf) {
        self.tracks.clear();
        self.index.clear();
        self.selected = None;
        self.folder = Some(dir.clone());

        let (tx, rx) = channel::<AudioMeta>();
        self.rx = Some(rx);
        let cache = self.cache.clone();

        thread::spawn(move || {
            use rayon::prelude::*;

            let mut paths: Vec<String> = Vec::new();
            scan_dir_recursive(&dir.to_string_lossy(), &mut paths);
            paths.sort();

            // Stage 1: instant header metadata so the list fills immediately.
            paths.par_iter().for_each_with(tx.clone(), |s, p| {
                let _ = s.send(probe_quick(p));
            });

            // Stage 2: full analysis (one decode pass), cached across sessions.
            paths.par_iter().for_each_with(tx.clone(), |s, p| {
                let meta = if let Some(m) = cache.get(p) {
                    m
                } else {
                    let m = analyze_one(p);
                    cache.set(p, m.clone());
                    m
                };
                let _ = s.send(meta);
            });
        });
    }

    /// Merge a streamed AudioMeta into the table (new path → push, else replace,
    /// but never let a late quick-probe clobber a completed full analysis).
    fn merge(&mut self, meta: AudioMeta) {
        match self.index.get(&meta.path).copied() {
            Some(i) => {
                let existing_full = self.tracks[i].partial != Some(true);
                let incoming_quick = meta.partial == Some(true);
                if !(existing_full && incoming_quick) {
                    self.tracks[i] = meta;
                }
            }
            None => {
                self.index.insert(meta.path.clone(), self.tracks.len());
                self.tracks.push(meta);
            }
        }
    }

    fn drain(&mut self, ctx: &egui::Context) {
        let mut got = false;
        if let Some(rx) = &self.rx {
            // Take a bounded batch per frame to keep the UI responsive.
            let mut batch = Vec::new();
            for _ in 0..2048 {
                match rx.try_recv() {
                    Ok(m) => batch.push(m),
                    Err(_) => break,
                }
            }
            got = !batch.is_empty();
            for m in batch {
                self.merge(m);
            }
        }
        if got {
            ctx.request_repaint();
        }
    }

    fn play_index(&mut self, i: usize, audio: &mut Player) {
        if let Some(t) = self.tracks.get(i) {
            let dur = t.duration_s as f32;
            if audio.play(std::path::Path::new(&t.path), dur).is_ok() {
                self.playing = Some(t.path.clone());
                self.selected = Some(i);
            }
        }
    }

    pub fn ui(&mut self, ui: &mut egui::Ui, audio: &mut Player) {
        self.drain(ui.ctx());

        // ── Top bar: folder + progress ────────────────────────────────────
        ui.horizontal(|ui| {
            if ui.button("📂 Open folder").clicked() {
                self.pick_folder();
            }
            if let Some(f) = &self.folder {
                ui.label(egui::RichText::new(f.to_string_lossy()).weak());
            }
        });
        let total = self.tracks.len();
        let pending = self.pending();
        ui.horizontal(|ui| {
            ui.label(format!("{total} tracks"));
            if pending > 0 {
                ui.add(egui::Spinner::new());
                ui.label(format!("analyzing… {}/{}", total - pending, total));
            }
        });
        ui.separator();

        // ── Transport ─────────────────────────────────────────────────────
        self.transport(ui, audio);

        // ── Waveform of the selected/playing track ────────────────────────
        self.waveform(ui, audio);
        ui.separator();

        // ── Track table (virtualized) ─────────────────────────────────────
        self.table(ui, audio);
    }

    fn transport(&mut self, ui: &mut egui::Ui, audio: &mut Player) {
        ui.horizontal(|ui| {
            if ui.button("⏮").clicked() {
                if let Some(i) = self.selected {
                    if i > 0 {
                        self.play_index(i - 1, audio);
                    }
                }
            }
            let playing = audio.is_playing();
            if ui.button(if playing { "⏸" } else { "▶" }).clicked() {
                if playing {
                    audio.pause();
                } else if self.playing.is_some() {
                    audio.resume();
                } else if let Some(i) = self.selected.or(Some(0)) {
                    self.play_index(i, audio);
                }
            }
            if ui.button("⏹").clicked() {
                audio.stop();
                self.playing = None;
            }
            if ui.button("⏭").clicked() {
                if let Some(i) = self.selected {
                    if i + 1 < self.tracks.len() {
                        self.play_index(i + 1, audio);
                    }
                }
            }
            let pos = audio.position();
            let dur = self
                .playing
                .as_ref()
                .and_then(|p| self.index.get(p))
                .and_then(|&i| self.tracks.get(i))
                .map(|t| t.duration_s as f32)
                .unwrap_or(0.0);
            ui.label(format!("{}  /  {}", fmt_time(pos), fmt_time(dur)));
        });
    }

    fn waveform(&mut self, ui: &mut egui::Ui, audio: &mut Player) {
        let idx = self.selected.or_else(|| {
            self.playing.as_ref().and_then(|p| self.index.get(p).copied())
        });
        let (rect, resp) =
            ui.allocate_exact_size(egui::vec2(ui.available_width(), 90.0), egui::Sense::click());
        let painter = ui.painter_at(rect);
        painter.rect_filled(rect, 2.0, egui::Color32::from_rgb(0x0a, 0x0a, 0x0a));

        let Some(i) = idx else { return };
        let Some(track) = self.tracks.get(i) else { return };
        let peaks = &track.peaks;
        if peaks.is_empty() {
            return;
        }

        let is_playing_this = self.playing.as_deref() == Some(track.path.as_str());
        let dur = track.duration_s as f32;
        let progress = if is_playing_this && dur > 0.0 {
            (audio.position() / dur).clamp(0.0, 1.0)
        } else {
            0.0
        };

        let mid = rect.center().y;
        let half = rect.height() * 0.5 - 2.0;
        let n = peaks.len();
        let play_x = rect.left() + rect.width() * progress;
        for (k, &p) in peaks.iter().enumerate() {
            let x = rect.left() + rect.width() * (k as f32 / n as f32);
            let h = (p.clamp(0.0, 1.0)) * half;
            let color = if x <= play_x {
                egui::Color32::WHITE
            } else {
                WAVE_DIM
            };
            painter.line_segment(
                [egui::pos2(x, mid - h), egui::pos2(x, mid + h)],
                egui::Stroke::new(1.0, color),
            );
        }
        if is_playing_this {
            painter.line_segment(
                [egui::pos2(play_x, rect.top()), egui::pos2(play_x, rect.bottom())],
                egui::Stroke::new(1.0, egui::Color32::from_rgb(0xff, 0x45, 0x3a)),
            );
        }

        // Click to seek within the currently playing track.
        if resp.clicked() && is_playing_this && dur > 0.0 {
            if let Some(pos) = resp.interact_pointer_pos() {
                let frac = ((pos.x - rect.left()) / rect.width()).clamp(0.0, 1.0);
                let _ = audio.seek(frac * dur);
            }
        }
    }

    fn table(&mut self, ui: &mut egui::Ui, audio: &mut Player) {
        let mut play_request: Option<usize> = None;
        let mut new_selected = self.selected;
        let playing_path = self.playing.clone();

        // Immutable borrow of tracks for the table; mutations deferred to locals.
        let tracks = &self.tracks;
        TableBuilder::new(ui)
            .striped(true)
            .cell_layout(egui::Layout::left_to_right(egui::Align::Center))
            .column(Column::remainder().at_least(220.0).clip(true)) // FILE
            .column(Column::exact(70.0)) // DUR
            .column(Column::exact(60.0)) // BPM
            .column(Column::exact(64.0)) // KEY
            .column(Column::exact(64.0)) // TYPE
            .header(20.0, |mut h| {
                h.col(|ui| { ui.strong("FILE"); });
                h.col(|ui| { ui.strong("DUR"); });
                h.col(|ui| { ui.strong("BPM"); });
                h.col(|ui| { ui.strong("KEY"); });
                h.col(|ui| { ui.strong("TYPE"); });
            })
            .body(|body| {
                body.rows(22.0, tracks.len(), |mut row| {
                    let i = row.index();
                    let t = &tracks[i];
                    let is_playing = playing_path.as_deref() == Some(t.path.as_str());
                    let (_, resp) = row.col(|ui| {
                        let label = egui::RichText::new(&t.name);
                        let label = if is_playing { label.color(egui::Color32::WHITE).strong() } else { label };
                        if ui.add(egui::Label::new(label).truncate().sense(egui::Sense::click())).clicked() {
                            play_request = Some(i);
                        }
                    });
                    if resp.clicked() {
                        new_selected = Some(i);
                    }
                    row.col(|ui| { ui.label(fmt_time(t.duration_s as f32)); });
                    row.col(|ui| { ui.label(t.bpm.map(|b| format!("{b:.0}")).unwrap_or_default()); });
                    row.col(|ui| { ui.label(t.key.clone().unwrap_or_default()); });
                    row.col(|ui| { ui.label(&t.format); });
                });
            });

        self.selected = new_selected;
        if let Some(i) = play_request {
            self.play_index(i, audio);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn quick(path: &str) -> AudioMeta {
        AudioMeta { path: path.into(), name: path.into(), partial: Some(true), ..Default::default() }
    }
    fn full(path: &str, bpm: f32) -> AudioMeta {
        AudioMeta { path: path.into(), name: path.into(), bpm: Some(bpm), partial: None, ..Default::default() }
    }

    #[test]
    fn merge_appends_then_full_replaces_but_quick_never_clobbers_full() {
        let mut s = PlayerTabState::default();

        s.merge(quick("a.wav"));
        assert_eq!(s.tracks.len(), 1);
        assert_eq!(s.pending(), 1);

        // Full analysis fills BPM and clears the pending flag.
        s.merge(full("a.wav", 140.0));
        assert_eq!(s.tracks.len(), 1);
        assert_eq!(s.pending(), 0);
        assert_eq!(s.tracks[0].bpm, Some(140.0));

        // A late quick-probe for the same path must NOT wipe the analysis.
        s.merge(quick("a.wav"));
        assert_eq!(s.tracks[0].bpm, Some(140.0));
        assert_eq!(s.pending(), 0);
    }

    #[test]
    fn fmt_time_formats_minutes_seconds() {
        assert_eq!(fmt_time(0.0), "0:00");
        assert_eq!(fmt_time(9.0), "0:09");
        assert_eq!(fmt_time(75.0), "1:15");
        assert_eq!(fmt_time(-1.0), "0:00");
    }
}

fn fmt_time(secs: f32) -> String {
    if !secs.is_finite() || secs <= 0.0 {
        return "0:00".to_string();
    }
    let s = secs as u32;
    format!("{}:{:02}", s / 60, s % 60)
}
