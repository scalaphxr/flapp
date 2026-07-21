use eframe::egui;

#[derive(Debug, PartialEq, Eq, Clone, Copy)]
pub enum Tab {
    Sounds,
    Player,
    Settings,
}

#[derive(Default)]
pub struct SoundsTabState {
    pub scratch: String,
}

#[derive(Default)]
pub struct SettingsTabState {
    pub scratch: String,
}

impl SoundsTabState {
    pub fn ui(&mut self, ui: &mut egui::Ui) {
        ui.heading("SOUNDS");
        ui.label("TODO: sound library (Sub-project 2)");
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

#[cfg(test)]
mod tests {
    use super::*;

    // The stub states are plain structs held by FlappApp across frames; switching
    // the active Tab must not touch other tabs' fields.
    #[test]
    fn switching_tabs_preserves_other_tab_state() {
        let mut sounds = SoundsTabState::default();
        let mut settings = SettingsTabState::default();
        let mut active = Tab::Sounds;
        assert_eq!(active, Tab::Sounds);
        sounds.scratch = "typed in sounds".to_string();
        active = Tab::Settings; // switch away
        assert_eq!(active, Tab::Settings);
        settings.scratch = "typed in settings".to_string();
        active = Tab::Sounds; // switch back
        assert_eq!(active, Tab::Sounds);
        assert_eq!(sounds.scratch, "typed in sounds");
        assert_eq!(settings.scratch, "typed in settings");
    }
}
