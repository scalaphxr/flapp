use eframe::egui::{self, Color32, Visuals};

/// tokens.css -> egui Visuals. Black panels, white text, grey borders, white
/// selection. Mirrors frontend/src/app/styles/tokens.css (terminal-core theme).
pub fn terminal_visuals() -> Visuals {
    let mut v = Visuals::dark();
    v.panel_fill = Color32::from_rgb(0, 0, 0); // --surface-1
    v.window_fill = Color32::from_rgb(0, 0, 0);
    v.faint_bg_color = Color32::from_rgb(0x0a, 0x0a, 0x0a); // --surface-2 (row zebra)
    v.extreme_bg_color = Color32::from_rgb(0x14, 0x14, 0x14); // --surface-3 (inputs)
    v.override_text_color = Some(Color32::from_rgb(0xff, 0xff, 0xff)); // --text-strong
    v.selection.bg_fill = Color32::from_rgb(0x33, 0x33, 0x33);
    v.selection.stroke.color = Color32::from_rgb(0xff, 0xff, 0xff); // --accent
    let border = Color32::from_rgb(0x3a, 0x3a, 0x3a); // --border-medium
    for w in [
        &mut v.widgets.noninteractive,
        &mut v.widgets.inactive,
        &mut v.widgets.hovered,
        &mut v.widgets.active,
    ] {
        w.bg_stroke.color = border;
    }
    v
}

/// Waveform base colour (--wave-dim). Played part uses white (--accent).
/// Consumed by the Player tab's waveform painter (Sub-project 1).
#[allow(dead_code)]
pub const WAVE_DIM: Color32 = Color32::from_rgb(0x66, 0x66, 0x66);

/// Apply the terminal-core visuals + make Monospace the default family. egui
/// ships a built-in monospace font, so no external .ttf is required here;
/// bundling a specific font is a later polish task.
pub fn install_theme(ctx: &egui::Context) {
    ctx.set_visuals(terminal_visuals());
    let mut fonts = egui::FontDefinitions::default();
    if let Some(mono) = fonts.families.get(&egui::FontFamily::Monospace).cloned() {
        fonts.families.insert(egui::FontFamily::Proportional, mono);
    }
    ctx.set_fonts(fonts);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn visuals_use_terminal_core_palette() {
        let v = terminal_visuals();
        assert!(v.dark_mode);
        assert_eq!(v.panel_fill, Color32::from_rgb(0, 0, 0)); // --surface-1
        assert_eq!(v.extreme_bg_color, Color32::from_rgb(0x14, 0x14, 0x14)); // --surface-3
        assert_eq!(v.override_text_color, Some(Color32::from_rgb(0xff, 0xff, 0xff)));
    }
}
