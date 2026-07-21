// Вкладка Sounds: библиотека сэмплов. Подбор папки → сканирование → имя-
// классификация (порт classify.rs) + анализ через flapp-dsp → фильтр по
// категории + текстовый поиск + воспроизведение. Порт архивов/.flp/дедупа —
// отдельные будущие заходы (см. статус рерайта).

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::mpsc::{channel, Receiver};
use std::sync::Arc;
use std::thread;

use eframe::egui;
use egui_extras::{Column, TableBuilder};

use flapp_audio::Player;
use flapp_dsp::{analyze_one, probe_quick, scan_dir_recursive, AnalyzerCache, AudioMeta};

use crate::classify::{classify_by_name, Category};
use crate::util::fmt_time;

struct Sample {
    meta: AudioMeta,
    category: Category,
}

pub struct SoundsTabState {
    folder: Option<PathBuf>,
    samples: Vec<Sample>,
    index: HashMap<String, usize>,
    cache: Arc<AnalyzerCache>,
    rx: Option<Receiver<AudioMeta>>,
    filter: Option<Category>, // None = All
    search: String,
    selected: Option<usize>,
    playing: Option<String>,
}

impl Default for SoundsTabState {
    fn default() -> Self {
        let dir = dirs::cache_dir().map(|d| d.join("flapp").join("analysis"));
        Self {
            folder: None,
            samples: Vec::new(),
            index: HashMap::new(),
            cache: Arc::new(AnalyzerCache::new(dir)),
            rx: None,
            filter: None,
            search: String::new(),
            selected: None,
            playing: None,
        }
    }
}

impl SoundsTabState {
    fn pending(&self) -> usize {
        self.samples.iter().filter(|s| s.meta.partial == Some(true)).count()
    }

    fn pick_folder(&mut self) {
        if let Some(dir) = rfd::FileDialog::new().pick_folder() {
            self.start_scan(dir);
        }
    }

    fn start_scan(&mut self, dir: PathBuf) {
        self.samples.clear();
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
            paths.par_iter().for_each_with(tx.clone(), |s, p| {
                let _ = s.send(probe_quick(p));
            });
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

    fn merge(&mut self, meta: AudioMeta) {
        let category = classify_by_name(&meta.name, &meta.path)
            .map(|(c, _)| c)
            .unwrap_or(Category::Fx);
        match self.index.get(&meta.path).copied() {
            Some(i) => {
                let existing_full = self.samples[i].meta.partial != Some(true);
                let incoming_quick = meta.partial == Some(true);
                if !(existing_full && incoming_quick) {
                    self.samples[i] = Sample { meta, category };
                }
            }
            None => {
                self.index.insert(meta.path.clone(), self.samples.len());
                self.samples.push(Sample { meta, category });
            }
        }
    }

    fn drain(&mut self, ctx: &egui::Context) {
        let mut got = false;
        if let Some(rx) = &self.rx {
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

    /// Indices of samples passing the category filter + text search.
    fn visible(&self) -> Vec<usize> {
        let q = self.search.to_ascii_lowercase();
        self.samples
            .iter()
            .enumerate()
            .filter(|(_, s)| self.filter.is_none_or(|f| s.category == f))
            .filter(|(_, s)| q.is_empty() || s.meta.name.to_ascii_lowercase().contains(&q))
            .map(|(i, _)| i)
            .collect()
    }

    fn play_index(&mut self, i: usize, audio: &mut Player) {
        if let Some(s) = self.samples.get(i) {
            let dur = s.meta.duration_s as f32;
            if audio.play(std::path::Path::new(&s.meta.path), dur).is_ok() {
                self.playing = Some(s.meta.path.clone());
                self.selected = Some(i);
            }
        }
    }

    pub fn ui(&mut self, ui: &mut egui::Ui, audio: &mut Player) {
        self.drain(ui.ctx());

        ui.horizontal(|ui| {
            if ui.button("📂 Open folder").clicked() {
                self.pick_folder();
            }
            if let Some(f) = &self.folder {
                ui.label(egui::RichText::new(f.to_string_lossy()).weak());
            }
        });

        let total = self.samples.len();
        let pending = self.pending();
        ui.horizontal(|ui| {
            ui.label(format!("{total} samples"));
            if pending > 0 {
                ui.add(egui::Spinner::new());
                ui.label(format!("analyzing… {}/{}", total - pending, total));
            }
        });

        // Category filter chips.
        ui.horizontal_wrapped(|ui| {
            if ui.selectable_label(self.filter.is_none(), "All").clicked() {
                self.filter = None;
            }
            for cat in Category::ALL {
                if ui.selectable_label(self.filter == Some(cat), cat.label()).clicked() {
                    self.filter = if self.filter == Some(cat) { None } else { Some(cat) };
                }
            }
        });

        ui.horizontal(|ui| {
            ui.label("🔍");
            ui.text_edit_singleline(&mut self.search);
            if !self.search.is_empty() && ui.button("✕").clicked() {
                self.search.clear();
            }
        });
        ui.separator();

        self.table(ui, audio);
    }

    fn table(&mut self, ui: &mut egui::Ui, audio: &mut Player) {
        let rows = self.visible();
        let mut play_request: Option<usize> = None;
        let mut new_selected = self.selected;
        let playing_path = self.playing.clone();
        let samples = &self.samples;

        TableBuilder::new(ui)
            .striped(true)
            .cell_layout(egui::Layout::left_to_right(egui::Align::Center))
            .column(Column::exact(78.0)) // CAT
            .column(Column::remainder().at_least(200.0).clip(true)) // FILE
            .column(Column::exact(56.0)) // BPM
            .column(Column::exact(60.0)) // KEY
            .column(Column::exact(64.0)) // DUR
            .column(Column::exact(58.0)) // TYPE
            .header(20.0, |mut h| {
                h.col(|ui| { ui.strong("CAT"); });
                h.col(|ui| { ui.strong("FILE"); });
                h.col(|ui| { ui.strong("BPM"); });
                h.col(|ui| { ui.strong("KEY"); });
                h.col(|ui| { ui.strong("DUR"); });
                h.col(|ui| { ui.strong("TYPE"); });
            })
            .body(|body| {
                body.rows(22.0, rows.len(), |mut row| {
                    let si = rows[row.index()];
                    let s = &samples[si];
                    let is_playing = playing_path.as_deref() == Some(s.meta.path.as_str());
                    row.col(|ui| { ui.label(s.category.label()); });
                    let (_, resp) = row.col(|ui| {
                        let label = egui::RichText::new(&s.meta.name);
                        let label = if is_playing { label.color(egui::Color32::WHITE).strong() } else { label };
                        if ui.add(egui::Label::new(label).truncate().sense(egui::Sense::click())).clicked() {
                            play_request = Some(si);
                        }
                    });
                    if resp.clicked() {
                        new_selected = Some(si);
                    }
                    row.col(|ui| { ui.label(s.meta.bpm.map(|b| format!("{b:.0}")).unwrap_or_default()); });
                    row.col(|ui| { ui.label(s.meta.key.clone().unwrap_or_default()); });
                    row.col(|ui| { ui.label(fmt_time(s.meta.duration_s as f32)); });
                    row.col(|ui| { ui.label(&s.meta.format); });
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

    fn quick(path: &str, name: &str) -> AudioMeta {
        AudioMeta { path: path.into(), name: name.into(), partial: Some(true), ..Default::default() }
    }

    #[test]
    fn merge_classifies_and_filters() {
        let mut s = SoundsTabState::default();
        s.merge(quick("/lib/808 deep.wav", "808 deep.wav"));
        s.merge(quick("/lib/snare_01.wav", "snare_01.wav"));
        s.merge(quick("/lib/kick_punchy.wav", "kick_punchy.wav"));
        assert_eq!(s.samples.len(), 3);

        // Category assignment via the ported classifier.
        assert_eq!(s.samples[0].category, Category::C808);
        assert_eq!(s.samples[1].category, Category::Snare);
        assert_eq!(s.samples[2].category, Category::Kick);

        // Filter by category.
        s.filter = Some(Category::Snare);
        assert_eq!(s.visible(), vec![1]);

        // Text search (case-insensitive).
        s.filter = None;
        s.search = "KICK".into();
        assert_eq!(s.visible(), vec![2]);
    }
}
