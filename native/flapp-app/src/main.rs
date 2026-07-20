#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
use eframe::egui;

mod theme;

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
            Ok(Box::<FlappApp>::default())
        }),
    )
}

#[derive(Default)]
struct FlappApp {}

impl eframe::App for FlappApp {
    // eframe 0.35: App::ui hands a background-less Ui; wrap content in a panel.
    fn ui(&mut self, ui: &mut egui::Ui, _frame: &mut eframe::Frame) {
        egui::CentralPanel::default().show(ui, |ui| {
            ui.label("Flapp native — scaffold");
        });
    }
}
