#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
use eframe::egui;

mod archive;
mod background;
mod classify;
mod dedup;
// Экспорт паков — движок готов, UI подключается в UI-фазе.
#[allow(dead_code)]
mod export;
mod flp;
// MIDI-экспорт из .flp — движок готов, UI подключается в UI-фазе.
#[allow(dead_code)]
mod midi;
mod player;
// Движок переименования — готов, UI подключается в UI-фазе.
#[allow(dead_code)]
mod rename;
#[allow(dead_code)]
mod settings;
// NL-поиск — готов, UI подключается в UI-фазе.
#[allow(dead_code)]
mod smartsearch;
mod sounds;
mod tabs;
mod theme;
mod util;

use player::PlayerTabState;
use sounds::SoundsTabState;
use tabs::{SettingsTabState, Tab};

fn main() -> eframe::Result {
    let options = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default()
            .with_inner_size([1200.0, 800.0])
            .with_min_inner_size([800.0, 600.0]),
        ..Default::default()
    };
    eframe::run_native(
        "Flapp",
        options,
        Box::new(|cc| {
            theme::install_theme(&cc.egui_ctx);
            Ok(Box::new(FlappApp::new()))
        }),
    )
}

/// Root app state. Held across frames by eframe (retained) — so each tab keeps
/// its own state when you switch away and back.
struct FlappApp {
    tab: Tab,
    sounds: SoundsTabState,
    player: PlayerTabState,
    settings: SettingsTabState,
    audio: flapp_audio::Player,
    shot_frame: u32,
}

impl FlappApp {
    fn new() -> Self {
        Self {
            tab: Tab::Sounds,
            sounds: SoundsTabState::default(),
            player: PlayerTabState::default(),
            settings: SettingsTabState::default(),
            audio: flapp_audio::Player::new().expect("audio output device"),
            shot_frame: 0,
        }
    }
}

impl eframe::App for FlappApp {
    // Прозрачный фон окна — чтобы фоновый слой с нотами просвечивал.
    fn clear_color(&self, _visuals: &egui::Visuals) -> [f32; 4] {
        theme::VOID.to_normalized_gamma_f32()
    }

    fn ui(&mut self, ui: &mut egui::Ui, _frame: &mut eframe::Frame) {
        self.maybe_screenshot(&ui.ctx().clone());
        background::draw(&ui.ctx().clone());

        // ── Консольная шапка: вордмарк + вкладки, прозрачный фон ──────────────
        let hdr = egui::Panel::top("hdr")
            .frame(egui::Frame::NONE.inner_margin(egui::Margin { left: 16, right: 16, top: 11, bottom: 9 }))
            .show(ui, |ui| {
                ui.horizontal(|ui| {
                    ui.label(
                        egui::RichText::new(theme::tracked("FLAPP"))
                            .color(theme::BRIGHT)
                            .size(15.0)
                            .strong(),
                    );
                    ui.add_space(26.0);
                    tab_button(ui, &mut self.tab, Tab::Sounds, "Sounds");
                    tab_button(ui, &mut self.tab, Tab::Player, "Player");
                    tab_button(ui, &mut self.tab, Tab::Settings, "Settings");
                });
            });
        // Волосяной разделитель под шапкой.
        let y = hdr.response.rect.bottom();
        let full = ui.ctx().content_rect();
        ui.painter().hline(full.x_range(), y, egui::Stroke::new(1.0, theme::LINE));

        egui::CentralPanel::default()
            .frame(egui::Frame::NONE.inner_margin(egui::Margin { left: 16, right: 16, top: 12, bottom: 12 }))
            .show(ui, |ui| match self.tab {
                Tab::Sounds => self.sounds.ui(ui, &mut self.audio),
                Tab::Player => self.player.ui(ui, &mut self.audio),
                Tab::Settings => self.settings.ui(ui),
            });
    }
}

impl FlappApp {
    /// Env-gated self-screenshot (FLAPP_SHOT=path): через ~40 кадров просит
    /// кадр у eframe, сохраняет PNG из фреймбуфера и закрывает окно. Для
    /// дизайн-итераций (внешний захват GPU-окна невозможен).
    fn maybe_screenshot(&mut self, ctx: &egui::Context) {
        let Ok(path) = std::env::var("FLAPP_SHOT") else { return };
        self.shot_frame += 1;
        if let Some(tab) = std::env::var("FLAPP_SHOT_TAB").ok().and_then(|t| match t.as_str() {
            "player" => Some(Tab::Player),
            "settings" => Some(Tab::Settings),
            _ => Some(Tab::Sounds),
        }) {
            self.tab = tab;
        }
        if self.shot_frame == 40 {
            ctx.send_viewport_cmd(egui::ViewportCommand::Screenshot(egui::UserData::default()));
        }
        let img = ctx.input(|i| {
            i.events.iter().find_map(|e| match e {
                egui::Event::Screenshot { image, .. } => Some(image.clone()),
                _ => None,
            })
        });
        if let Some(image) = img {
            save_png(&path, &image);
            ctx.send_viewport_cmd(egui::ViewportCommand::Close);
        }
        ctx.request_repaint();
    }
}

fn save_png(path: &str, image: &egui::ColorImage) {
    let [w, h] = image.size;
    let mut buf = Vec::with_capacity(w * h * 4);
    for p in &image.pixels {
        buf.extend_from_slice(&[p.r(), p.g(), p.b(), 255]);
    }
    if let Ok(file) = std::fs::File::create(path) {
        let mut enc = png::Encoder::new(std::io::BufWriter::new(file), w as u32, h as u32);
        enc.set_color(png::ColorType::Rgba);
        enc.set_depth(png::BitDepth::Eight);
        if let Ok(mut w) = enc.write_header() {
            let _ = w.write_image_data(&buf);
        }
    }
}

/// Вкладка-таб: верхний регистр с разрядкой, активная — ярко-белая с 2px
/// подчёркиванием, неактивная — приглушённая, при наведении — тонкая линия.
fn tab_button(ui: &mut egui::Ui, cur: &mut Tab, tab: Tab, label: &str) {
    let active = *cur == tab;
    let color = if active { theme::BRIGHT } else { theme::MID };
    let resp = ui.add(
        egui::Label::new(egui::RichText::new(theme::tracked(label)).color(color).size(12.0))
            .sense(egui::Sense::click()),
    );
    let r = resp.rect;
    if active {
        ui.painter().hline(r.x_range(), r.bottom() + 5.0, egui::Stroke::new(2.0, theme::BRIGHT));
    } else if resp.hovered() {
        ui.painter().hline(r.x_range(), r.bottom() + 5.0, egui::Stroke::new(1.0, theme::DIM));
    }
    if resp.clicked() {
        *cur = tab;
    }
    ui.add_space(12.0);
}
