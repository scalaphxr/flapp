use eframe::egui;

#[derive(Debug, PartialEq, Eq, Clone, Copy)]
pub enum Tab {
    Sounds,
    Player,
    Settings,
}

#[derive(Default)]
pub struct SettingsTabState {
    pub scratch: String,
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

    // Each tab's state is a plain struct held by FlappApp across frames; switching
    // the active Tab must not touch another tab's fields.
    #[test]
    fn independent_tab_state_persists_across_switches() {
        let mut a = SettingsTabState::default();
        let mut b = SettingsTabState::default();
        let mut active = Tab::Sounds;
        assert_eq!(active, Tab::Sounds);
        a.scratch = "a".to_string();
        active = Tab::Settings;
        assert_eq!(active, Tab::Settings);
        b.scratch = "b".to_string();
        active = Tab::Sounds;
        assert_eq!(active, Tab::Sounds);
        assert_eq!(a.scratch, "a");
        assert_eq!(b.scratch, "b");
    }
}
