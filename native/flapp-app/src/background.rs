// Фоновая анимация нот: музыкальные глифы медленно дрейфуют вверх по пустоте,
// едва различимые. Рисуются на фоновом слое, под всем контентом. Сигнатурный
// элемент дизайна — тихий, ненавязчивый.

use eframe::egui::{self, Align2, Color32, FontId, Id, LayerId, Order};

const GLYPHS: &[&str] = &["\u{266A}", "\u{266B}", "\u{266C}"]; // ♪ ♫ ♬ (надёжно есть в шрифте)
const COUNT: usize = 18;

pub fn draw(ctx: &egui::Context) {
    let rect = ctx.content_rect();
    let t = ctx.input(|i| i.time) as f32;
    let (w, h) = (rect.width().max(1.0), rect.height().max(1.0));

    let painter = ctx.layer_painter(LayerId::new(Order::Background, Id::new("ambient-notes")));

    for i in 0..COUNT {
        let r1 = hash01(i as u32 * 2 + 1);
        let r2 = hash01(i as u32 * 7 + 3);
        let r3 = hash01(i as u32 * 13 + 5);

        let speed = 7.0 + r2 * 20.0; // px/сек вверх
        let span = h + 80.0;
        let y = rect.bottom() + 40.0 - (t * speed + r3 * span).rem_euclid(span);

        let sway = (t * (0.15 + r3 * 0.35) + r1 * 6.28).sin() * 16.0;
        let x = rect.left() + (r1 * w + sway).rem_euclid(w);

        let size = 12.0 + r2 * 18.0;
        // Едва видимый серый, слегка разной яркости для глубины.
        let shade = 0x12 + (r3 * 0x12 as f32) as u8;
        let col = Color32::from_rgb(shade, shade, shade);

        painter.text(
            egui::pos2(x, y),
            Align2::CENTER_CENTER,
            GLYPHS[i % GLYPHS.len()],
            FontId::proportional(size),
            col,
        );
    }

    // ~30 fps — плавно и без лишней нагрузки.
    ctx.request_repaint_after(std::time::Duration::from_millis(33));
}

/// Детерминированный хэш → [0,1). Без внешних зависимостей.
fn hash01(x: u32) -> f32 {
    let mut h = x.wrapping_mul(2_654_435_761);
    h ^= h >> 15;
    h = h.wrapping_mul(2_246_822_519);
    h ^= h >> 13;
    (h & 0x00FF_FFFF) as f32 / 0x0100_0000 as f32
}
