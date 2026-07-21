// Вкладка Sounds: библиотека сэмплов. Подбор папки → сканирование → имя-
// классификация (порт classify.rs) + анализ через flapp-dsp → дедуп (точный +
// акустический) → фильтр по категории + поиск + воспроизведение. Порт
// архивов/.flp/MIDI — отдельные будущие заходы.

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::mpsc::{channel, Receiver};
use std::sync::Arc;
use std::thread;

use eframe::egui;
use egui_extras::{Column, TableBuilder};

use flapp_audio::Player;
use flapp_dsp::{
    analyze_one, decode_mono_native, extract_features, probe_quick, scan_dir_recursive,
    AnalyzerCache, AudioMeta,
};

use crate::classify::{classify_by_name, classify_full, wants_audio, Category};
use crate::dedup::{quick_hash, DedupIndex, DupKind};
use crate::util::fmt_time;

/// Кап декода признаков (нативный SR) для аудио-классификации ambiguous-файлов.
const FEATURE_MAX_SAMPLES: usize = 44_100 * 12;

/// Одно сообщение анализа: метаданные + контент-хэш (для точного дедупа) +
/// разрешённая категория (считается в воркере, не на UI-потоке).
struct SoundMsg {
    meta: AudioMeta,
    content_hash: String,
    category: Category,
}

struct Sample {
    meta: AudioMeta,
    category: Category,
    content_hash: String,
    dup: DupKind,
}

pub struct SoundsTabState {
    folder: Option<PathBuf>,
    samples: Vec<Sample>,
    index: HashMap<String, usize>,
    cache: Arc<AnalyzerCache>,
    rx: Option<Receiver<SoundMsg>>,
    filter: Option<Category>, // None = All
    search: String,
    selected: Option<usize>,
    playing: Option<String>,
    deep_dedup: bool,
    hide_dupes: bool,
    dedup_dirty: bool,
    dup_count: usize,
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
            deep_dedup: true,
            hide_dupes: false,
            dedup_dirty: false,
            dup_count: 0,
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
        self.dup_count = 0;
        self.dedup_dirty = false;
        self.folder = Some(dir.clone());

        let (tx, rx) = channel::<SoundMsg>();
        self.rx = Some(rx);
        let cache = self.cache.clone();

        thread::spawn(move || {
            use rayon::prelude::*;
            let mut paths: Vec<String> = Vec::new();
            scan_dir_recursive(&dir.to_string_lossy(), &mut paths);
            // Архивы: извлекаем аудио из .zip и добавляем к списку (структура
            // папок сохраняется → классификатор по пути работает).
            if let Some(root) = dirs::cache_dir().map(|d| d.join("flapp").join("extracted")) {
                for zip in crate::archive::find_zips(&dir.to_string_lossy()) {
                    paths.extend(crate::archive::extract_zip_audio(&zip, &root));
                }
            }
            paths.sort();
            paths.dedup();
            // Стадия 1: мгновенные заголовки + имя-классификация (без аудио).
            paths.par_iter().for_each_with(tx.clone(), |s, p| {
                let meta = probe_quick(p);
                let category = classify_by_name(&meta.name, p).map(|(c, _)| c).unwrap_or(Category::Fx);
                let _ = s.send(SoundMsg { meta, content_hash: String::new(), category });
            });
            // Стадия 2: полный анализ + контент-хэш + аудио-классификация
            // ambiguous-файлов (для них — доп. декод на нативном SR).
            paths.par_iter().for_each_with(tx.clone(), |s, p| {
                let meta = if let Some(m) = cache.get(p) {
                    m
                } else {
                    let m = analyze_one(p);
                    cache.set(p, m.clone());
                    m
                };
                let content_hash = quick_hash(std::path::Path::new(p)).unwrap_or_default();
                let feat = if wants_audio(&meta.name, p, meta.duration_s) {
                    decode_mono_native(p, FEATURE_MAX_SAMPLES).map(|(m, sr)| extract_features(&m, sr))
                } else {
                    None
                };
                let category = classify_full(&meta.name, p, feat.as_ref()).0;
                let _ = s.send(SoundMsg { meta, content_hash, category });
            });
        });
    }

    fn merge(&mut self, msg: SoundMsg) {
        let SoundMsg { meta, content_hash, category } = msg;
        match self.index.get(&meta.path).copied() {
            Some(i) => {
                let existing_full = self.samples[i].meta.partial != Some(true);
                let incoming_quick = meta.partial == Some(true);
                if !(existing_full && incoming_quick) {
                    self.samples[i].meta = meta;
                    self.samples[i].category = category;
                    if !content_hash.is_empty() {
                        self.samples[i].content_hash = content_hash;
                    }
                }
            }
            None => {
                self.index.insert(meta.path.clone(), self.samples.len());
                self.samples.push(Sample { meta, category, content_hash, dup: DupKind::Unique });
            }
        }
        self.dedup_dirty = true;
    }

    /// Детерминированный проход дедупа по всем полностью проанализированным
    /// сэмплам (порядок — по пути). Порт логики harvest+index: точный дубль по
    /// контент-хэшу; акустический — при близости отпечатка И совпадении имени.
    fn recompute_dedup(&mut self) {
        let mut order: Vec<usize> = (0..self.samples.len()).collect();
        order.sort_by(|&a, &b| self.samples[a].meta.path.cmp(&self.samples[b].meta.path));

        let mut ix = DedupIndex::new(self.deep_dedup, 0);
        let mut dups = 0usize;
        for &i in &order {
            // Пока не проанализирован полностью — считаем уникальным (не решаем).
            if self.samples[i].meta.partial == Some(true) {
                self.samples[i].dup = DupKind::Unique;
                continue;
            }
            let (name, hash, fp) = {
                let s = &self.samples[i];
                (s.meta.name.clone(), s.content_hash.clone(), s.meta.fingerprint.clone())
            };
            let (kind, existing) = ix.check(&hash, &fp, &name);
            let is_dup = match kind {
                DupKind::Exact => true,
                // Акустический — вероятностный: требуем ещё совпадения имени.
                DupKind::Acoustic => existing
                    .as_deref()
                    .is_some_and(|e| e.eq_ignore_ascii_case(&name)),
                DupKind::Unique => false,
            };
            if is_dup {
                self.samples[i].dup = kind;
                dups += 1;
            } else {
                self.samples[i].dup = DupKind::Unique;
                ix.add(&hash, &fp, &name);
            }
        }
        self.dup_count = dups;
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
        // Когда анализ устоялся — один раз пересчитываем дедуп.
        if self.dedup_dirty && self.pending() == 0 && !self.samples.is_empty() {
            self.recompute_dedup();
            self.dedup_dirty = false;
        }
    }

    fn visible(&self) -> Vec<usize> {
        let q = self.search.to_ascii_lowercase();
        self.samples
            .iter()
            .enumerate()
            .filter(|(_, s)| !(self.hide_dupes && s.dup != DupKind::Unique))
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
            } else if self.dup_count > 0 {
                ui.label(egui::RichText::new(format!("· {} duplicates", self.dup_count)).weak());
            }
        });

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
            ui.separator();
            ui.checkbox(&mut self.hide_dupes, "Hide dupes");
            if ui.checkbox(&mut self.deep_dedup, "Acoustic").changed() {
                self.dedup_dirty = true; // пересчитать при смене режима
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
                    let is_dup = s.dup != DupKind::Unique;
                    row.col(|ui| { ui.label(s.category.label()); });
                    let (_, resp) = row.col(|ui| {
                        let mut label = egui::RichText::new(&s.meta.name);
                        if is_playing {
                            label = label.color(egui::Color32::WHITE).strong();
                        } else if is_dup {
                            label = label.weak(); // дубли приглушены
                        }
                        if ui.add(egui::Label::new(label).truncate().sense(egui::Sense::click())).clicked() {
                            play_request = Some(si);
                        }
                        if is_dup {
                            let tag = if s.dup == DupKind::Exact { "=dup" } else { "≈dup" };
                            ui.label(egui::RichText::new(tag).small().weak());
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

    fn cat_of(name: &str, path: &str) -> Category {
        classify_by_name(name, path).map(|(c, _)| c).unwrap_or(Category::Fx)
    }
    fn quick(path: &str, name: &str) -> SoundMsg {
        SoundMsg {
            meta: AudioMeta { path: path.into(), name: name.into(), partial: Some(true), ..Default::default() },
            content_hash: String::new(),
            category: cat_of(name, path),
        }
    }
    fn full(path: &str, name: &str, hash: &str, fp: &str) -> SoundMsg {
        SoundMsg {
            meta: AudioMeta {
                path: path.into(),
                name: name.into(),
                partial: None,
                fingerprint: fp.into(),
                ..Default::default()
            },
            content_hash: hash.into(),
            category: cat_of(name, path),
        }
    }

    #[test]
    fn merge_classifies_and_filters() {
        let mut s = SoundsTabState::default();
        s.merge(quick("/lib/808 deep.wav", "808 deep.wav"));
        s.merge(quick("/lib/snare_01.wav", "snare_01.wav"));
        s.merge(quick("/lib/kick_punchy.wav", "kick_punchy.wav"));
        assert_eq!(s.samples.len(), 3);
        assert_eq!(s.samples[0].category, Category::C808);
        assert_eq!(s.samples[1].category, Category::Snare);
        assert_eq!(s.samples[2].category, Category::Kick);

        s.filter = Some(Category::Snare);
        assert_eq!(s.visible(), vec![1]);
        s.filter = None;
        s.search = "KICK".into();
        assert_eq!(s.visible(), vec![2]);
    }

    #[test]
    fn exact_duplicates_detected_and_hideable() {
        let mut s = SoundsTabState::default();
        // Two different names, identical content hash → second is an exact dup.
        s.merge(full("/lib/a.wav", "a.wav", "qHASH", ""));
        s.merge(full("/lib/b.wav", "b.wav", "qHASH", ""));
        s.merge(full("/lib/c.wav", "c.wav", "qOTHER", ""));
        s.recompute_dedup();

        assert_eq!(s.dup_count, 1);
        // a (first by path) kept unique, b is the dup, c unique.
        let by = |name: &str| s.samples.iter().find(|x| x.meta.name == name).unwrap();
        assert_eq!(by("a.wav").dup, DupKind::Unique);
        assert_eq!(by("b.wav").dup, DupKind::Exact);
        assert_eq!(by("c.wav").dup, DupKind::Unique);

        // Hiding dupes removes exactly the duplicate row.
        s.hide_dupes = true;
        assert_eq!(s.visible().len(), 2);
    }
}
