# Сборка звуков

Кроссплатформенное десктоп-приложение для битмейкеров: автоматически достаёт, анализирует, сортирует и помогает управлять звуками из проектов и архивов. Перетащил `.zip` / `.flp` / папку — получил разобранную, очищенную от дублей библиотеку сэмплов с поиском, превью, переименованием и сборкой паков.

**Стек:** Tauri v2 (оболочка на Rust) + Go (бэкенд) + React + TypeScript + Vite + SQLite.
**Архитектура:** Clean Architecture, SOLID, Feature-Sliced Design на фронте.

---

## Что реально работает, а что — база с возможностью апгрейда

Это честный раздел. Приложение собирается, запускается и делает то, что описано ниже, но часть «умных» функций сделана на разумных эвристиках, а не на тяжёлых ML-моделях.

**Работает «из коробки» (настоящий код, а не заглушки):**
- Полный конвейер: распаковка архивов → парсинг `.flp` → извлечение аудио → анализ → категоризация → дедупликация → запись в SQLite, всё в фоновом пуле воркеров с прогрессом через SSE.
- Парсер `.flp`: читает реальный бинарный формат (события FLhd/FLdt) — пути к сэмплам, BPM, заголовок, каналы, плагины.
- Анализ аудио: WAV и AIFF декодируются полностью (FFT, RMS, спектральный центроид, ZCR, атака и т.д.); для mp3/flac/ogg/m4a извлекается длительность по заголовку.
- Дедупликация: точные дубли по MD5/SHA-256 + перцептивный аудио-отпечаток (496-битный хэш, расстояние Хэмминга) для «похоже звучащих».
- Beat Manager: движок массового переименования (UPPER/lower/Title, чистка цифр и спецсимволов, префикс/суффикс, вставка до/после BPM, find&replace, regex, «умное» маркетинговое имя) с предпросмотром и применением на диске.
- Сборка паков: экспорт выбранных звуков в ZIP с раскладкой по папкам категорий.
- Аналитика, поиск с фильтрами (FTS5), избранное, рейтинги, теги, коллекции, умный NL-поиск (RU+EN).
- Дизайн и UX — полностью по макету.

**База, которую стоит улучшать под себя:**
- **Классификатор типов звука** — это правила (по имени файла, папке и аудио-признакам), а не обученная нейросеть. Работает хорошо на типичных названиях, но это не «магия».
- **Акустическое сходство** — перцептивный хэш, а не обученные эмбеддинги. Находит близкое, но не понимает «вайб».
- **Отпечаток для сжатых форматов** — для mp3/flac/ogg/m4a сейчас только длительность по заголовку; дедуп для них падает обратно на MD5/SHA-256 (т.е. ловит только точные дубли). Для полноценного акустического дедупа сжатых файлов нужен декодер (ffmpeg/cgo).
- **Импорт сторонних библиотек** — это индексация папок, а не интеграции с конкретными платформами.

Коротко: это не «законченный коммерческий продукт», а крепкая, честно работающая основа с правильной архитектурой, на которую легко наращивать ML.

---

## Почему Go запускается как сайдкар, а не «бэкенд Tauri»

Ядро Tauri — это Rust; Go не может быть бэкендом Tauri напрямую. Поэтому здесь тонкая Rust-оболочка запускает скомпилированный Go-бинарник как **сайдкар** — локальный HTTP-сервер на `127.0.0.1` (случайный порт) с потоковым прогрессом по SSE. React общается с ним по HTTP.

```
┌─────────────────────────────────────────────┐
│  Tauri (Rust)  — окно, сайдкар, IPC          │
│    └─ spawns ──────────────┐                 │
│                            ▼                 │
│  Go sidecar (sborka-core)  — HTTP + SSE      │
│    усечает PORT=NNNN в stdout, Rust его ловит│
│    domain / usecase / infrastructure / http  │
│    └─ SQLite (library.db, FTS5)              │
│                                              │
│  React + TS (Vite, FSD)  — UI в webview      │
│    fetch http://127.0.0.1:NNNN/api/...       │
└─────────────────────────────────────────────┘
```

При запуске Go печатает `PORT=NNNN`, Rust читает эту строку, сохраняет порт и отдаёт его фронту командой `get_backend_port` + событием `backend-ready`. При закрытии окна Rust убивает сайдкар.

---

## Требования

- **Go** 1.23+
- **Node.js** 18+ и npm
- **Rust** (stable) + системные зависимости Tauri v2 — см. https://v2.tauri.app/start/prerequisites/
  - Linux: `webkit2gtk-4.1`, `libgtk-3-dev`, `librsvg2-dev`, `libayatana-appindicator3-dev`, `build-essential` и пр.
  - macOS: Xcode Command Line Tools.
  - Windows: WebView2 (обычно уже стоит) + MSVC Build Tools.

---

## Сборка

### 1. Собрать Go-бэкенд (сайдкар)

Сайдкар должен называться с суффиксом target-triple, который ожидает Tauri. Скрипт делает это сам.

```bash
# macOS / Linux
bash scripts/build-sidecar.sh

# Windows (PowerShell)
powershell -ExecutionPolicy Bypass -File scripts\build-sidecar.ps1
```

Скрипт выполнит `go mod tidy`, соберёт бинарник и положит его в `src-tauri/binaries/sborka-core-<target-triple>[.exe]`.

> При первой сборке `go mod tidy` скачает зависимости: `modernc.org/sqlite` (чистый Go, без cgo), `github.com/bodgit/sevenzip`, `github.com/nwaples/rardecode/v2`.

### 2. Установить зависимости фронтенда

```bash
npm --prefix frontend install
```

### 3. Собрать десктоп-приложение

```bash
# из корня проекта
npm run build
# (= build:sidecar, затем tauri build)
```

Готовые установщики появятся в `src-tauri/target/release/bundle/` (`.dmg`/`.app`, `.msi`/`.exe`, `.deb`/`.AppImage`).

### Запуск в режиме разработки

```bash
# 1) собери сайдкар хотя бы раз
bash scripts/build-sidecar.sh
# 2) запусти dev (Vite + окно Tauri с HMR)
npm run dev
```

Чисто фронтенд в браузере (без Tauri) тоже поднимается (`npm --prefix frontend run dev`), но тогда нужно запустить бэкенд вручную на фиксированном порту и указать его фронту:

```bash
# терминал 1
cd backend && go run ./cmd/sborka-core --port 8765
# терминал 2
cd frontend && VITE_API_BASE=http://127.0.0.1:8765 npm run dev
```

---

## Иконки

В `src-tauri/icons/` лежит полный набор (PNG/ICO/ICNS). Если захочешь заменить лого — положи свой `icon.png` (1024×1024) и выполни `npm run tauri icon` (или `cargo tauri icon`), Tauri перегенерирует все размеры.

---

## Структура проекта

```
sborka-zvukov/
├─ backend/                  Go-бэкенд (Clean Architecture)
│  ├─ cmd/sborka-core/       main: сборка графа зависимостей, HTTP, graceful shutdown
│  └─ internal/
│     ├─ domain/             сущности + порты (интерфейсы), без зависимостей
│     ├─ usecase/            сценарии: harvest, library, beatmanager, packbuilder, smartsearch, analytics
│     ├─ infrastructure/     реализации: audio, classify, dedup, flp, jobs, tagging, archive, settings
│     └─ adapter/
│        ├─ storage/         SQLite-репозитории (+ FTS5)
│        └─ http/            HTTP-роуты, хэндлеры, SSE
├─ src-tauri/                оболочка Tauri v2 (Rust)
│  ├─ src/lib.rs             запуск сайдкара, проброс порта во фронт
│  ├─ binaries/              сюда кладётся скомпилированный Go-сайдкар
│  ├─ icons/                 иконки приложения
│  ├─ capabilities/          права (shell-сайдкар, dialog, события)
│  └─ tauri.conf.json
├─ frontend/                 React + TS + Vite (Feature-Sliced Design)
│  └─ src/
│     ├─ app/                оболочка, роутинг по вкладкам, стили (дизайн-токены)
│     ├─ pages/              harvest, library, beat-manager, analytics, pack-builder, settings
│     ├─ widgets/            TopBar, SoundTable
│     └─ shared/             ui (примитивы), api (клиент+SSE+типы), model (zustand), i18n, config, lib
└─ scripts/                  build-sidecar.sh / .ps1
```

---

## Возможности

Сбор из `.zip`/`.rar`/`.7z`, `.flp`, папок и отдельных файлов · форматы wav/mp3/flac/ogg/aiff/m4a · 40 категорий звука с цветовой раскладкой по 13 группам · многоуровневая дедупликация со статистикой · библиотека с поиском (FTS5), превью и волной · Beat Manager (массовое переименование, 14 операций, предпросмотр) · аналитика с графиками · сборка паков в ZIP · умный поиск на естественном языке (RU/EN, «тёмные 808 на 140 bpm») · автогенерация тегов · избранное, рейтинги, коллекции · фоновая очередь задач с прогрессом · настройки (язык RU/EN без перезапуска, тема, папка экспорта, число потоков, чувствительность дедупа) · поддержка больших библиотек (тысячи проектов / десятки тысяч файлов).

---

## API (локальный, для справки)

`POST /api/harvest` · `GET /api/jobs/{id}` · `POST /api/jobs/{id}/cancel` · `GET /api/events` (SSE) · `GET /api/samples` · `GET /api/samples/{id}/audio` · `POST /api/samples/{id}/favorite|rating|tags` · `GET /api/samples/{id}/similar` · `GET /api/projects` · `GET/POST /api/collections` · `POST /api/rename/preview|apply` · `POST /api/packs` · `GET /api/analytics` · `GET /api/tags` · `POST /api/smartsearch` · `GET/PUT /api/settings`.

---

## Тесты бэкенда

```bash
cd backend && go test ./internal/...
```

Покрыты чистые пакеты: анализ аудио, классификатор, дедуп, парсер FLP, очередь задач (в т.ч. `-race`), движок переименования и парсер умного поиска.
