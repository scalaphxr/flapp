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
pub struct PlayerTabState {
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

#[cfg(test)]
mod tests {
    use super::*;

    // The stub states are plain structs held by FlappApp across frames; switching
    // the active Tab must not touch other tabs' fields. This models that: mutate
    // one, switch, and confirm each field persists independently.
    #[test]
    fn switching_tabs_preserves_other_tab_state() {
        let mut sounds = SoundsTabState::default();
        let mut player = PlayerTabState::default();
        let mut active = Tab::Sounds;
        assert_eq!(active, Tab::Sounds);
        sounds.scratch = "typed in sounds".to_string();
        active = Tab::Player; // switch away
        assert_eq!(active, Tab::Player);
        player.scratch = "typed in player".to_string();
        active = Tab::Sounds; // switch back
        assert_eq!(active, Tab::Sounds);
        assert_eq!(sounds.scratch, "typed in sounds");
        assert_eq!(player.scratch, "typed in player");
    }
}
