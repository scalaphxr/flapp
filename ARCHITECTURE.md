# ARCHITECTURE — карта проекта Flapp («Сборка звуков»)

> Документ для быстрого въезда в проект (в т.ч. для новой сессии ИИ), чтобы не изучать
> всё с нуля. Здесь — где что лежит, как связано и почему сделано именно так.
> Держи в актуальном состоянии при крупных изменениях. `README.md` местами устарел
> (в нём ещё «40 категорий», YouTube/covers/MIDI/player/watcher там не описаны) —
> при расхождении верь этому файлу и коду.

## 1. Что это

Десктоп-приложение для битмейкеров: собирает звуки из архивов / `.flp` / папок,
анализирует, категоризирует, дедуплицирует, даёт библиотеку с поиском/превью/волной,
массовое переименование (Beat Manager), сборку паков в ZIP, извлечение MIDI из `.flp`,
плеер-анализатор папок (BPM/тональность/волна) и публикацию бита на YouTube.

## 2. Стек и топология процессов

**Tauri v2 (Rust) + Go-сайдкар + React/TS (Vite) + SQLite (modernc, чистый Go, без cgo).**

Ядро Tauri — Rust, поэтому Go не может *быть* бэкендом Tauri напрямую. Схема:

```
Tauri (Rust, src-tauri/)            — окно, spawn сайдкара, IPC-команды, drag-out, watcher, плеер-анализ
  └─ spawn ──> Go sidecar (flapp-core) — локальный HTTP+SSE на 127.0.0.1:<случайный порт>
                 печатает "PORT=NNNN" в stdout; Rust ловит строку (lib.rs:parse_port)
                 └─ SQLite library.db (FTS5)
React webview (frontend/)           — fetch http://127.0.0.1:NNNN/api/... + SSE; часть работы через Tauri invoke
```

- При старте Go печатает `PORT=NNNN` → Rust сохраняет и отдаёт фронту командой
  `get_backend_port` + событием `backend-ready` (`src-tauri/src/lib.rs`).
- При закрытии окна Rust убивает сайдкар (`RunEvent::ExitRequested` → `child.kill()`).
- **Важно:** при старте Go **очищает library.db** (`store.Samples.DeleteAll`,
  `flapp-core/main.go`) — библиотека сэмплов эфемерна между запусками; персистентны
  настройки, кэши анализа, MIDI/exports.

**Две независимые «половины» приложения:**
1. **Вкладка Sounds** — полный Go-конвейер harvest → библиотека в SQLite (HTTP API).
2. **Вкладка Player** — Rust-анализатор папок напрямую через Tauri invoke (НЕ трогает
   Go/SQLite; свой дисковый кэш в app_cache_dir). Быстрый анализ BPM/key/волны.

## 3. Запуск и сборка

- Dev: `npm run dev` (из корня; = `tauri dev`). Сначала хотя бы раз собрать сайдкар.
- Сборка сайдкара: `scripts/build-sidecar.ps1` (Win) / `.sh` — `go build` в
  `src-tauri/binaries/flapp-core-<target-triple>[.exe]` (суффикс обязателен для Tauri).
- Полная сборка: `npm run build` (= build:sidecar + `tauri build`).
- Тесты Go: `cd backend && go test ./internal/...`.
- **После любых изменений кода — перезапускать приложение** (см. память проекта).
- Данные приложения: `os.UserConfigDir()/flapp/` → `library.db`, `library/`, `tmp/`,
  `settings.json`, `exports/`, `exports/MIDI/`, `covers/`, `ffmpeg`, `youtube_token.json`.

## 4. Backend (Go) — Clean Architecture

Модуль `github.com/flapp/core`, Go 1.23. `backend/cmd/flapp-core/main.go` — единственная
точка сборки графа зависимостей (`run()`), всё через конструкторы/интерфейсы.

```
backend/internal/
├─ domain/            Сущности + порты (интерфейсы). Нет внешних зависимостей.
├─ usecase/           Сценарии (сервисы). Зависят только от domain-портов.
├─ infrastructure/    Реализации портов (audio, classify, dedup, flp, ...).
└─ adapter/
   ├─ storage/        SQLite-репозитории (реализуют domain-порты) + FTS5
   └─ http/           HTTP-роуты, хэндлеры, SSE (потребляют usecase + domain-порты)
```

### 4.1 domain/ (порты и типы)
- `ports.go` — **все интерфейсы**: `SampleRepository`, `ProjectRepository`,
  `CollectionRepository`, `AnalyticsRepository`, `TagRepository` (persistence);
  `ArchiveExtractor`, `FLPParser`, `AudioAnalyzer`, `Classifier`, `TagGenerator`,
  `Hasher`, `Packer`, `JobQueue`, `ProgressReporter` (сервисы). Здесь же тип
  `Analytics` и агрегаты для дашборда.
- `sample.go` — `Sample` (центральная сущность), `AudioFeatures` (RMS, спектральный
  центроид, ZCR, атака, spectral flatness, crest factor и т.д.), `Project`,
  `FLPChannel`, `FLPNote` (ноты пианоролла, только внутри Go), `Collection`, `DedupStats`.
- `category.go` — **таксономия (актуально 11 категорий, НЕ 40 из README):**
  `808, Kick, Snare, Clap, Hi-Hat, Open Hat, Perc, Vox, FX, Loop, Drum Loop`.
  `ColorGroup` → CSS-переменные `--cat-*`. `RemapLegacy` сводит старые дробные имена.
  Хелперы `IsLoop/IsDrum/Group`.
- `job.go`, `midi.go` — типы фоновых задач и MIDI-клипов.

### 4.2 usecase/ (сервисы)
- `harvest.go` — **главный конвейер импорта**: распаковка → парсинг `.flp` → извлечение
  аудио → анализ → категоризация → дедуп → запись в SQLite. Разделённые пулы воркеров:
  `ioWorkers` (чтение/декод, I/O-bound, ~2) и `cpuWorkers` (FFT/хэш, ~cores-1); решение
  о дубле и запись в БД воронкой через одну горутину (консистентность dedup-индекса и
  SQLite). Прогресс — через `ProgressReporter` → SSE. Использует `AnalysisCache`.
- `library.go` — поиск/фильтры/операции над сэмплами (favorite/rating/tags/category).
- `beatmanager.go` — движок массового переименования (UPPER/lower/Title, чистка,
  префикс/суффикс, вставка у BPM, find&replace, regex, «умное» имя) + preview/apply.
- `packbuilder.go` — экспорт выбранных звуков в ZIP по папкам категорий (+ MIDI-паки).
- `midi_extract.go` — извлечение MIDI из `.flp`: парсит ноты пианоролла, группирует в
  клипы, пишет `.mid` в `exports/MIDI/`, категоризирует, дедуп. Свой BPM-из-имени
  (зеркалит `analyzer.rs::bpm_from_filename`).
- `smartsearch.go` — NL-поиск (RU+EN): «тёмные 808 на 140 bpm» → фильтры.
- `analytics.go` — агрегаты для дашборда.
- `youtube.go` — `YouTubeService`: рендер видео-из-картинки (ffmpeg) + upload на канал.
  Работа под мьютексом (CPU-рендер + один канал). `YouTubeUploadRequest`.
- `covers.go` — `CoverService`: поиск обложек (публичная картиночная выдача, Bing
  Images — Pinterest API отдаёт 403 анонимам; pinimg-результаты сортируются первыми,
  см. память), скачивание в `dataDir/covers`.
- `scan.go`, `library.go`, `unresolved.go` — вспомогательные.

### 4.3 infrastructure/ (реализации)
- `audio/` — декодеры (`decode_flac.go`, `decode_mp3.go`, `decode_ogg.go`; WAV/AIFF
  декодируются полностью), `analyzer.go` (FFT, RMS, центроид, ZCR, атака…),
  `fft.go`, `peaks.go` (пики волны, min/max пары — peaks v2), `spectrogram.go`,
  `duration.go`. Для mp3/flac/ogg/m4a — длительность по заголовку.
- `classify/` — правила категоризации: `classifier.go` (комбинирует имя+папку+аудио),
  `rules.go` (`ClassifyByName`, приоритеты, аббревиатуры; напр. «bd» = TR-808 bass drum
  = Cat808, а не Kick — см. память), `model.go`/`model_classifier.go`.
- `dedup/` — `hasher.go` (MD5/SHA-256 — точные дубли), `index.go` (перцептивный
  496-битный хэш + расстояние Хэмминга — «похоже звучащие»), `quickhash.go`.
- `flp/` — `parser.go`: реальный бинарный формат FLP (события FLhd/FLdt) — пути к
  сэмплам, BPM, каналы, плагины, ноты пианоролла, PPQ, Time Spent.
- `archive/` — `archive.go` (zip/rar/7z обход), `bombguard.go` (zip-бомбы), `nested.go`
  (вложенные архивы), `rar.go`, `sevenzip.go`, `packer.go` (запись ZIP/7z).
- `jobs/` — `queue.go`: фоновая очередь задач с прогрессом и Subscribe (для SSE).
- `midi/` — парсер/писатель `.mid`, категоризация, мультитрек.
- `tagging/` — автогенерация тегов из сэмпла.
- `settings/` — `settings.go`: `settings.json` под мьютексом, всегда полный объект с
  дефолтами. Поля: язык/тема/папки/workers/dedup + **YouTube** (ffmpeg path, OAuth
  client id/secret, шаблоны названия/описания, теги, privacy).
- `youtube/` — `oauth.go` (OAuth desktop-флоу), `upload.go`, `ffmpeg.go` (поиск/скачка
  ffmpeg), `tags.go`, `defaults.go` (`DefaultClientID/Secret` — вшитые дефолт-креды,
  генерируются скриптом `scripts/gen-yt-defaults.ps1` в `defaults_local.go`; свои ключи
  из настроек имеют приоритет — см. память `project_yt_auth`), `hidewindow_*.go`
  (прятать окно ffmpeg на Windows).
- `covers/` — `covers.go`: поиск картинок.
- `format/` — детектор формата файла.

### 4.4 adapter/
- `storage/` — SQLite (`modernc.org/sqlite`, без cgo). `db.go`, `driver.go`,
  `schema.go`/`schema_v2.go` (миграции), репозитории `samples.go` (+ кэш пиков
  `peaks_json`/`peaks2_json` в БД), `projects.go`, `collections.go`, `analytics.go`,
  `analysis_cache.go` (кэш фич по content-hash — не декодировать повторно).
- `http/` — `server.go` (**реестр всех роутов** — начинай отсюда при вопросах об API;
  stdlib-роутинг Go 1.22 `GET /api/x/{id}`, CORS, `/debug/pprof/`), `handlers.go`,
  `midi_handlers.go`, `youtube_handlers.go`, `sse.go` (`GET /api/events`).

### 4.5 HTTP API (localhost, для фронта)
Полный список — в `server.go:routes()`. Основные группы:
`/api/health` · `/api/harvest` + `/api/jobs*` + `/api/events` (SSE) ·
`/api/samples*` (search/get/similar/peaks/spectrogram/audio/category/favorite/rating/tags/delete) ·
`/api/projects*` · `/api/collections*` · `/api/files` + `/api/rename/*` (Beat Manager) ·
`/api/packs` + `/api/export/folder` · `/api/analytics` · `/api/tags` · `/api/smartsearch` ·
`/api/settings` (GET/PUT) · `/api/youtube/*` (status/auth/disconnect/ffmpeg/upload/tags) ·
`/api/covers/{search,download}` · `/api/midi/*` (extract/clips/pack/dedup) · `/api/cache/clear`.

## 5. Tauri shell (Rust, src-tauri/src/)

- `main.rs` — тонкий вход, зовёт `lib.rs::run()`.
- `lib.rs` — жизненный цикл сайдкара (spawn, парсинг `PORT=`, kill при выходе),
  регистрация плагинов (`shell`, `dialog`, `drag` — нативный drag-out файлов в DAW),
  Tauri-команды: `get_backend_port`, `player_read_audio` (сырые байты файла в JS без
  JSON), плюс команды из `analyzer` и `watcher`.
- `analyzer.rs` (~1700 строк) — **аудио-анализатор вкладки Player** (независим от Go):
  - `probe_container` — быстрый зонд заголовка (формат/длительность/SR) → мгновенные
    partial-метаданные; затем ОДИН потоковый проход декодера (`stream_peaks_and_dsp`):
    одновременно пики волны (4096 точек) и моно-окно из середины для DSP.
  - **BPM** (`compute_bpm`/`bpm_from_onset`): onset-огибающая по STFT → автокорреляция
    + гармоническая поддержка + лог-приор вокруг 140 BPM (жанр — хип-хоп/трэп) против
    октавных ошибок; имя файла (`bpm_from_filename`) переопределяет DSP.
  - **Тональность** (`detect_key`/`chroma_features`/`match_key`): HPCP-хрома (окно 8192),
    поправка строя, гармоническое взвешивание, басовая хрома, подавление обертонов 808,
    профили Крумхансл–Шмуклер, уверенность по согласию блоков. Формат меток `Cmaj/Amin`.
  - Двухуровневый кэш `AnalyzerCache` (память + диск app_cache_dir, ключ = версия+путь+
    mtime+размер; `ANALYSIS_VERSION` бампается при смене алгоритма).
  - Команды: `player_analyze_file`, `player_analyze_batch` (rayon-параллель, стрим
    событий `player-analysis-progress`), `player_decode_to_wav`, `player_scan_folder`
    (рекурсия с лимитом глубины 64, НЕ раскрывает symlink/junction), `player_get_dates`.
  - **Ограничение** (см. память `project_player_batch_constraint`): не гонять
    `player_analyze_batch` по синканным junction-папкам — IPC-шторм = тихий краш.
- `watcher.rs` — live-слежение за папками плеера (`notify` + дебаунс 900мс),
  пересканирование деревьев, событие `player-fs-change {added, removed}`. Файл,
  который ещё копируется (размер меняется), не отдаётся, пока не стабилизируется.
- `capabilities/default.json` — права (shell-сайдкар, dialog, drag, события).
- `tauri.conf.json` — конфиг Tauri.

## 6. Frontend (React + TS + Vite, Feature-Sliced Design)

Псевдоним `@/` → `frontend/src/`. Точка входа `main.tsx` → `app/App.tsx`.

```
frontend/src/
├─ app/            App.tsx (оболочка, вкладки), styles/ (tokens.css — дизайн-токены/темы, fonts)
├─ pages/
│  ├─ samples/     SamplesPage.tsx (~1000 стр, главная вкладка Sounds), MidiSection.tsx
│  ├─ player/      PlayerPage.tsx (~2500 стр — вкладка Player, самый большой файл)
│  ├─ midi/        MidiPianoRoll.tsx, useMidiPlayer.ts
│  ├─ analytics/   AnalyticsPage.tsx (lazy)
│  └─ settings/    SettingsPage.tsx (~740 стр)
├─ widgets/        TopBar/ (вкладки), SoundTable/ (таблица звуков + WaveformCanvas)
└─ shared/
   ├─ ui/          Примитивы: Button, Card, CategoryTag, Checkbox, DropZone,
   │               PlayButton, ProgressBar, StatStrip, Tabs, Input, Icons (+ index.ts)
   ├─ api/         client.ts (HTTP-клиент, base url из порта), types.ts, events.ts (SSE)
   ├─ model/       zustand-стор: jobs.ts, player.ts (общий <audio>, toggle/seek),
   │               settings.ts (грузит с бэка, применяет тему/язык; fromServer-гард
   │               чтобы fallback не затёр реальные настройки)
   ├─ i18n/        ru.ts / en.ts / index.ts (переключение языка без перезапуска)
   ├─ config/      categories.ts (цвета/порядок категорий на фронте)
   └─ lib/         format.ts, tauri.ts (обёртки invoke/события), dragOut.ts (нативный drag-out)
```

**Активные вкладки (App.tsx):** `sounds` (SamplesPage), `player` (PlayerPage, lazy),
`settings`. AnalyticsPage существует и lazy-подгружаем, но в текущем `tabs` не выведен.
Значения `TabKey`: `sounds | analytics | player | settings`.

## 7. Ключевые инварианты и подводные камни

- **library.db стирается при каждом старте** — не рассчитывай на персистентность сэмплов.
- **Две системы анализа BPM/key:** Go-harvest (для библиотеки) и Rust-analyzer (для
  плеера) — раздельные реализации, при правке одной синхронизируй логику имён.
- **808 «bd»-конвенция:** «bd» в имени = TR-808 bass drum = `Cat808`, не Kick.
- **settings fromServer-гард:** фронт не шлёт PUT, пока не получил GET — иначе пустой
  fallback затирает реальные настройки (однажды так пропали YouTube-ключи).
- **YouTube дефолт-креды:** `defaults_local.go` генерируется скриптом (запускает
  пользователь), в git его нет; свои ключи из настроек в приоритете.
- **Обложки:** через Bing Images (не Pinterest API).
- **Player batch:** не гонять по junction-папкам / не превышать глубину скана.
- **seek плеера** использует harvest-длительность, а не `audio.duration` (VBR-mp3 врёт).
- Тесты покрывают чистые пакеты: audio, classify, dedup, flp, jobs, beatmanager,
  smartsearch, midi, archive, storage-search.

## 8. Куда смотреть при типичных задачах

| Задача | Файл(ы) |
|---|---|
| Добавить/изменить HTTP-роут | `backend/internal/adapter/http/server.go` + `*_handlers.go` |
| Логика импорта/дедупа | `backend/internal/usecase/harvest.go`, `infrastructure/dedup/` |
| Категоризация звука | `backend/internal/infrastructure/classify/rules.go` |
| Парсинг .flp | `backend/internal/infrastructure/flp/parser.go` |
| Схема БД / запросы | `backend/internal/adapter/storage/schema*.go`, `samples.go` |
| BPM/тональность плеера | `src-tauri/src/analyzer.rs` |
| Слежение за папками | `src-tauri/src/watcher.rs` |
| Жизненный цикл сайдкара / Tauri-команды | `src-tauri/src/lib.rs` |
| Главная вкладка звуков | `frontend/src/pages/samples/SamplesPage.tsx` |
| Плеер | `frontend/src/pages/player/PlayerPage.tsx` |
| Настройки (UI + модель) | `frontend/src/pages/settings/SettingsPage.tsx`, `shared/model/settings.ts` |
| Волна/таблица | `frontend/src/widgets/SoundTable/SoundTable.tsx` |
| Типы API / клиент | `frontend/src/shared/api/{types.ts,client.ts,events.ts}` |
| Переводы | `frontend/src/shared/i18n/{ru,en}.ts` |
```
