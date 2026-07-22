#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
use eframe::egui;

// Settings API is scaffolding for later sub-projects; the Foundation doesn't
// read/write settings yet.
mod archive;
mod classify;
mod dedup;
// Экспорт паков — движок готов, UI подключается в UI-фазе.
#[allow(dead_code)]
mod export;
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
}

impl FlappApp {
    fn new() -> Self {
        Self {
            tab: Tab::Sounds,
            sounds: SoundsTabState::default(),
            player: PlayerTabState::default(),
            settings: SettingsTabState::default(),
            audio: flapp_audio::Player::new().expect("audio output device"),
        }
    }
}

impl eframe::App for FlappApp {
    // eframe 0.35: App::ui hands a background-less Ui; nest panels via show(ui, …).
    fn ui(&mut self, ui: &mut egui::Ui, _frame: &mut eframe::Frame) {
        egui::Panel::top("tabs").show(ui, |ui| {
            ui.horizontal(|ui| {
                ui.selectable_value(&mut self.tab, Tab::Sounds, "SOUNDS");
                ui.selectable_value(&mut self.tab, Tab::Player, "PLAYER");
                ui.selectable_value(&mut self.tab, Tab::Settings, "SETTINGS");
            });
        });
        egui::CentralPanel::default().show(ui, |ui| match self.tab {
            Tab::Sounds => self.sounds.ui(ui, &mut self.audio),
            Tab::Player => self.player.ui(ui, &mut self.audio),
            Tab::Settings => self.settings.ui(ui),
        });
    }
}
