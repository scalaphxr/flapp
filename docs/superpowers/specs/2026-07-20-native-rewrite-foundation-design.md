# Нативный рерайт Flapp — Sub-project 0: Фундамент (Rust + egui)

**Дата:** 2026-07-20
**Статус:** на ревью
**Стек:** всё на Rust, GUI — egui/eframe. Без webview, без Go-сайдкара.

## Контекст и мотивация

Текущий стек — Tauri v2 (Rust-оболочка) + Go-сайдкар (HTTP+SSE, SQLite) +
React/Vite (webview). Четыре болевые точки по скорости:

1. **Список звуков** — не виртуализирован: `SoundTable` рендерит все строки, у
   каждой свой `<canvas>` волны → тяжёлый DOM, лаг прокрутки/кликов.
2. **Переключение вкладок** — `App.tsx` полностью размонтирует/монтирует страницы;
   Player (lazy, ~2500 строк) пересобирается при каждом заходе.
3. **Анализ звуков** — межпроцессный HTTP/SSE-хоп между webview и Go.
4. **Поиск картинок** — сетевой (Pinterest), но ещё и кросс-процессный хоп.

Решение (выбрано пользователем): **полная смена стека на нативный Rust + egui**,
миграция **постепенно, по вкладкам**. egui выбран потому, что даёт встроенную
виртуализацию списков (`ScrollArea::show_rows`), простое кастомное рисование
(волны/спектр/пианоролл через `Painter`), быстро писать соло, и идеально ложится
на минималистичный terminal-core чёрно-белый моно-стиль.

Нативная оболочка снимает боли 1–3 архитектурно: нативный виртуализированный
список (1), retained-виджеты с сохранением состояния (2), прямые вызовы функций
вместо IPC (3). Боль 4 остаётся сетевой, но без кросс-процессного хопа.

Этот документ описывает **только Фундамент** (Sub-project 0). Остальные вкладки —
отдельные спеки (см. «Карта миграции» в конце).

## Границы Фундамента

**В зоне ответственности:** каркас нативного приложения, на котором строятся все
последующие вкладки.

**Вне зоны (YAGNI для Фундамента):** реальное содержимое любой вкладки, SQLite,
файловый watcher, YouTube/covers, упаковка/инсталлятор. Всё это — последующие
sub-projects.

## Раскладка крейтов

Новая директория `native/` с Cargo-workspace. Старые `src-tauri/` и `backend/`
**не трогаем** — они собираются и запускаются в течение всей миграции.

```
native/
├─ Cargo.toml            # workspace: члены flapp-dsp, flapp-audio, flapp-app
├─ flapp-dsp/            # чистый анализ (без Tauri)
├─ flapp-audio/          # нативное воспроизведение (rodio/cpal)
└─ flapp-app/            # бинарник eframe/egui: окно, вкладки, тема
```

### `flapp-dsp` — чистый анализ

Извлекается из `src-tauri/src/analyzer.rs` (~1700 строк): `probe_container`,
`stream_peaks_and_dsp`, `compute_bpm`/`bpm_from_onset`/`bpm_from_filename`,
`detect_key`/`chroma_features`/`match_key`, двухуровневый `AnalyzerCache`
(память + диск, ключ = `ANALYSIS_VERSION`+путь+mtime+размер). Зависимости:
`symphonia`, `rustfft` (или текущий FFT), `rayon`. **Ноль зависимостей от Tauri.**

Публичный API (примерно):
```rust
pub struct Analysis { pub bpm: Option<f32>, pub key: Option<String>,
                      pub peaks: Vec<(f32, f32)>, pub duration_sec: f32, /* … */ }
pub fn probe(path: &Path) -> Result<Probe>;          // быстрый зонд заголовка
pub fn analyze_file(path: &Path, cache: &Cache) -> Result<Analysis>;
pub fn analyze_batch(paths: &[PathBuf], cache: &Cache,
                     on_progress: impl Fn(usize, usize)) -> Vec<Result<Analysis>>;
```
Прогресс батча — через колбэк/канал (в нативе не нужны Tauri-события).

**Решение по дублированию:** извлекаем чистый DSP в `flapp-dsp`, а старый
`src-tauri/src/analyzer.rs` превращаем в тонкую Tauri-обёртку, вызывающую
`flapp-dsp`. Единственный источник правды, старый плеер продолжает работать.
Обёртка держит только `#[tauri::command]`-функции и эмит событий прогресса.

### `flapp-audio` — воспроизведение

`rodio` (поверх `cpal`). API:
```rust
pub struct Player { /* Sink + OutputStream */ }
impl Player {
    pub fn play(&mut self, path: &Path) -> Result<()>;
    pub fn pause(&self); pub fn resume(&self); pub fn stop(&self);
    pub fn seek(&self, secs: f32) -> Result<()>;   // rodio Sink::try_seek
    pub fn position(&self) -> f32;                  // текущая позиция, сек
    pub fn is_playing(&self) -> bool;
}
```
**Позиция и seek считаются от probed-длительности из `flapp-dsp`**, а не от
длительности, сообщённой декодером — переносим инвариант «VBR-mp3 врёт про
длительность» (см. ARCHITECTURE §7). Playhead в UI берёт `position()`.

### `flapp-app` — приложение egui

Точка входа:
```rust
fn main() -> eframe::Result {
    let opts = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default()
            .with_inner_size([1200.0, 800.0]).with_min_inner_size([800.0, 600.0]),
        ..Default::default()
    };
    eframe::run_native("Flapp", opts, Box::new(|cc| {
        install_theme(&cc.egui_ctx);   // фонты + Visuals
        Ok(Box::new(FlappApp::new(cc)))
    }))
}
```

Состояние приложения живёт между кадрами (retained) — **состояние вкладок не
теряется при переключении**:
```rust
enum Tab { Sounds, Player, Settings }
struct FlappApp {
    tab: Tab,
    player: PlayerTabState,     // в Фундаменте — заглушки
    sounds: SoundsTabState,
    settings: SettingsTabState,
    audio: flapp_audio::Player,
}
impl eframe::App for FlappApp {
    fn update(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        egui::TopBottomPanel::top("tabs").show(ctx, |ui| {
            ui.horizontal(|ui| {
                ui.selectable_value(&mut self.tab, Tab::Sounds,  "SOUNDS");
                ui.selectable_value(&mut self.tab, Tab::Player,  "PLAYER");
                ui.selectable_value(&mut self.tab, Tab::Settings,"SETTINGS");
            });
        });
        egui::CentralPanel::default().show(ctx, |ui| match self.tab {
            Tab::Sounds   => self.sounds.ui(ui),
            Tab::Player   => self.player.ui(ui, &mut self.audio),
            Tab::Settings => self.settings.ui(ui),
        });
    }
}
```
В Фундаменте `*.ui()` рисуют заглушки (заголовок + «TODO») — этого достаточно,
чтобы проверить каркас, тему и сохранение состояния между вкладками.

### Настройки/данные

Крошечный модуль в `flapp-app` (или отдельный `flapp-settings` позже): чтение/
запись того же `os.UserConfigDir()/flapp/settings.json` (совместимо со старым
приложением). Только поля, нужные вкладкам. **SQLite откладываем** до Sounds-
sub-project. Кэш анализа — тот же `app_cache_dir`, что у `flapp-dsp`.

## Тема terminal-core → egui Visuals

Моноширинный шрифт по умолчанию через `FontDefinitions` (бандлим тот же моно-шрифт).
`ctx.set_visuals(...)` с маппингом из `tokens.css`:

| tokens.css        | egui Visuals                         | Значение   |
|-------------------|--------------------------------------|------------|
| `--surface-1` #000| `panel_fill`, `window_fill`          | чёрный фон |
| `--surface-2` #0a0a0a | `faint_bg_color` (зебра строк)   | #0a0a0a    |
| `--surface-3` #141414 | `extreme_bg_color` (поля ввода)  | #141414    |
| `--text-strong` #fff | `override_text_color` / сильный   | белый      |
| `--text-body` #a3a3a3| текст виджетов                     | #a3a3a3    |
| `--border-medium` #3a3a3a | `widgets.*.bg_stroke`         | границы    |
| `--accent` #fff   | `selection.bg_fill`, активное        | белый      |
| `--wave-dim` #666 | (в `flapp-app` как константа для волн)| #666666    |

Тема ставится один раз в `install_theme(ctx)`.

## Критерии готовности (Definition of Done)

1. `cargo run -p flapp-app` открывает нативное окно: чёрный фон, белый моно-текст,
   три вкладки SOUNDS/PLAYER/SETTINGS.
2. Переключение вкладок мгновенное; состояние каждой вкладки сохраняется (проверка:
   изменить поле-заглушку на одной вкладке, уйти и вернуться — значение на месте).
3. `cargo test -p flapp-dsp` — портированные юнит-тесты анализатора проходят.
4. `flapp-audio` проигрывает и перематывает wav/flac/mp3 (ручной smoke-тест +
   юнит-тест на арифметику позиции/seek относительно probed-длительности).
5. **Старое приложение Tauri+Go по-прежнему собирается и запускается** после
   извлечения DSP (проверка: `npm run dev`, плеер анализирует файл).
6. В новом бинарнике нет Go и node.

## Тестирование

- `flapp-dsp`: переносим существующие тесты анализатора (BPM/key/имена файлов).
- `flapp-audio`: юнит-тест на арифметику позиции и seek; воспроизведение — smoke.
- `flapp-app`: логика редьюсера вкладок тривиально тестируема; сам egui-UI —
  ручной smoke (иммедиат-mode UI юнит-тестами не покрываем).

## Риски и смягчение

1. **Извлечение DSP дестабилизирует старый плеер.** Смягчение: после извлечения
   явно проверяем критерий №5 (старый `npm run dev` + анализ файла). Старый код —
   в запушенном снапшоте, откат безопасен.
2. **Точность seek у rodio на VBR-mp3.** Смягчение: позиция/seek от probed-
   длительности `flapp-dsp`, а не от декодера.
3. **Дрейф версий egui/eframe (имена методов трейта App и т.п.).** Смягчение:
   пиним версии `egui`/`eframe` в `Cargo.toml`; сверяемся с примерами именно этой
   версии.
4. **rodio может не поддерживать seek для части форматов.** Смягчение: если
   `try_seek` вернёт `Unsupported` — фолбэк на переоткрытие потока со смещения;
   зафиксировать как под-задачу Player-sub-project, если проявится.

## Карта миграции (контекст, не часть Фундамента)

Каждый пункт — отдельная спека → план → реализация. Старое приложение работает,
пока новое не достигнет паритета; затем удаляем `src-tauri/` webview + Go.

- **Sub-project 0 — Фундамент** (этот документ): каркас, тема, `flapp-dsp`,
  `flapp-audio`, настройки.
- **Sub-project 1 — вкладка Player** (следующая): порт UI (DSP уже готов в
  `flapp-dsp`). Строим переиспользуемые виджеты: виртуализированный список,
  `Painter`-волна, playback, watcher. Быстрая победа.
- **Sub-project 2 — вкладка Sounds** (крупнейшая): порт Go-конвейера harvest в
  Rust — archive (zip/rar/7z), парсер `.flp`, classify, dedup (перцептивный хэш),
  FTS-поиск; здесь появляется SQLite (`rusqlite`, bundled).
- **Sub-project 3 — Settings + YouTube + Covers**: ffmpeg рендер+загрузка, OAuth,
  подбор обложек Pinterest.
- **Финал:** удалить webview `src-tauri/` и Go-сайдкар.
