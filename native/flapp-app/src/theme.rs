use eframe::egui::{self, Color32, CornerRadius, Stroke, Visuals};

// Terminal-core, доведённый до дизайна: чистый монохром, угловатость (нулевые
// скругления, волосяные линии), «глубина» за счёт слоёв серого. Акцентного
// цвета нет намеренно — единственный яркий сигнал это белый.

pub const VOID: Color32 = Color32::from_rgb(0x00, 0x00, 0x00); // фон
pub const PANEL: Color32 = Color32::from_rgb(0x0a, 0x0a, 0x0a); // приподнятая поверхность
pub const LINE: Color32 = Color32::from_rgb(0x24, 0x24, 0x24); // волосяные разделители/рамки
pub const DIM: Color32 = Color32::from_rgb(0x3a, 0x3a, 0x3a); // очень тусклый текст
pub const MID: Color32 = Color32::from_rgb(0x6b, 0x6b, 0x6b); // вторичный текст
pub const TEXT: Color32 = Color32::from_rgb(0xa3, 0xa3, 0xa3); // основной текст
pub const BRIGHT: Color32 = Color32::from_rgb(0xff, 0xff, 0xff); // заголовки/активное

/// Базовый (непроигранный) цвет волны; проигранная часть — белый.
#[allow(dead_code)]
pub const WAVE_DIM: Color32 = Color32::from_rgb(0x66, 0x66, 0x66);

pub fn terminal_visuals() -> Visuals {
    let mut v = Visuals::dark();
    v.panel_fill = VOID;
    v.window_fill = VOID;
    v.extreme_bg_color = Color32::from_rgb(0x12, 0x12, 0x12); // поля ввода
    v.faint_bg_color = Color32::from_rgb(0x0c, 0x0c, 0x0c); // зебра строк — почти незаметна
    v.code_bg_color = PANEL;
    v.override_text_color = Some(TEXT);
    v.hyperlink_color = BRIGHT;

    // Выделение — белая рамка/заливка без цвета.
    v.selection.bg_fill = Color32::from_rgb(0x22, 0x22, 0x22);
    v.selection.stroke = Stroke::new(1.0, BRIGHT);

    // Угловатость: нулевые скругления везде.
    v.window_corner_radius = CornerRadius::ZERO;
    v.menu_corner_radius = CornerRadius::ZERO;
    v.window_stroke = Stroke::new(1.0, LINE);

    // Волосяные обводки виджетов; активное состояние — ярче.
    for w in [
        &mut v.widgets.noninteractive,
        &mut v.widgets.inactive,
        &mut v.widgets.hovered,
        &mut v.widgets.active,
        &mut v.widgets.open,
    ] {
        w.corner_radius = CornerRadius::ZERO;
        w.bg_stroke = Stroke::new(1.0, LINE);
    }
    v.widgets.noninteractive.fg_stroke = Stroke::new(1.0, TEXT);
    v.widgets.inactive.fg_stroke = Stroke::new(1.0, MID);
    v.widgets.inactive.bg_fill = PANEL;
    v.widgets.inactive.weak_bg_fill = VOID;
    v.widgets.hovered.fg_stroke = Stroke::new(1.0, TEXT);
    v.widgets.hovered.bg_fill = Color32::from_rgb(0x18, 0x18, 0x18);
    v.widgets.hovered.weak_bg_fill = Color32::from_rgb(0x14, 0x14, 0x14);
    v.widgets.hovered.bg_stroke = Stroke::new(1.0, DIM);
    v.widgets.active.fg_stroke = Stroke::new(1.0, BRIGHT);
    v.widgets.active.bg_fill = Color32::from_rgb(0x1e, 0x1e, 0x1e);
    v.widgets.active.bg_stroke = Stroke::new(1.0, MID);
    v
}

pub fn install_theme(ctx: &egui::Context) {
    ctx.set_visuals(terminal_visuals());

    // Моноширинный по умолчанию: делаем моно-шрифт первым в Proportional, но
    // сохраняем emoji/symbol-фолбэк — иначе музыкальные глифы (♪♫) не отрисуются.
    let mut fonts = egui::FontDefinitions::default();
    if let Some(mono_primary) = fonts
        .families
        .get(&egui::FontFamily::Monospace)
        .and_then(|v| v.first())
        .cloned()
    {
        fonts
            .families
            .entry(egui::FontFamily::Proportional)
            .or_default()
            .insert(0, mono_primary);
    }
    ctx.set_fonts(fonts);

    // Плотная, выверенная сетка отступов + тонкая типографика.
    use egui::{FontFamily, FontId, TextStyle};
    ctx.all_styles_mut(|style| {
        style.spacing.item_spacing = egui::vec2(8.0, 6.0);
        style.spacing.button_padding = egui::vec2(10.0, 5.0);
        style.spacing.window_margin = egui::Margin::same(0);
        style.spacing.menu_margin = egui::Margin::same(4);
        style.spacing.interact_size.y = 22.0;
        style.spacing.scroll.bar_width = 8.0;
        style.text_styles = [
            (TextStyle::Heading, FontId::new(15.0, FontFamily::Monospace)),
            (TextStyle::Body, FontId::new(13.0, FontFamily::Monospace)),
            (TextStyle::Monospace, FontId::new(13.0, FontFamily::Monospace)),
            (TextStyle::Button, FontId::new(13.0, FontFamily::Monospace)),
            (TextStyle::Small, FontId::new(11.0, FontFamily::Monospace)),
        ]
        .into();
    });
}

/// Заголовок-«эйбр» в верхнем регистре с разрядкой (буквы через тонкий пробел)
/// — консольный акцент без кастомного трекинга в egui.
pub fn tracked(s: &str) -> String {
    let up = s.to_uppercase();
    let mut out = String::with_capacity(up.len() * 2);
    for (i, c) in up.chars().enumerate() {
        if i > 0 {
            out.push('\u{2009}'); // тонкий пробел = разрядка
        }
        out.push(c);
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn visuals_are_angular_monochrome() {
        let v = terminal_visuals();
        assert!(v.dark_mode);
        assert_eq!(v.panel_fill, VOID);
        assert_eq!(v.window_corner_radius, CornerRadius::ZERO);
        assert_eq!(v.widgets.inactive.corner_radius, CornerRadius::ZERO);
        assert_eq!(v.override_text_color, Some(TEXT));
    }

    #[test]
    fn tracked_inserts_thin_spaces_and_uppercases() {
        assert_eq!(tracked("ab"), "A\u{2009}B");
    }
}
