# Console Design Foundation (Фаза 0) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Перевести фронтенд Flapp на единственную тему Console: снести FL-скин, схлопнуть токены в `:root`, подключить локальные шрифты, перевести примитивы на CSS Modules.

**Architecture:** Работа идёт в два такта. Сначала из семи файлов удаляются FL-ветки — при живых старых токенах, поэтому приложение продолжает работать на каждом коммите. Затем токены переезжают в `:root` с палитрой Console, и только после этого примитивы переводятся на CSS Modules по одному.

**Tech Stack:** React 18, TypeScript 5.5, Vite 5, Zustand, CSS Modules (встроены в Vite, ставить нечего), Geist + Geist Mono (вариативные `.woff2`, уже в репозитории).

**Спек:** `docs/superpowers/specs/2026-07-16-console-design-foundation.md`

## Global Constraints

- **Порядок тактов нерушим.** Все цветовые токены сейчас лежат внутри `[data-theme=…]`. Убрать тему раньше, чем переехать в `:root`, — значит оставить приложение без единого цвета. Задачи 1–5 (снос FL) идут строго до задачи 6 (токены).
- **Тестов в проекте нет** — ни раннера, ни конфига. Заводить фреймворк в этой фазе запрещено (см. спек §6). Проверка каждой задачи: `npm run build:frontend` + греп-утверждения + осмотр запущенного приложения.
- **Рабочее дерево грязное не по нашей вине.** `PlayerPage.tsx`, `i18n/en.ts`, `i18n/ru.ts`, `shared/lib/authors.ts` содержат несохранённые правки пользователя. **Никогда не делать `git add -A` и `git commit -a`.** Только явные пути.
- **Ветка:** `design/console-foundation` (уже создана, спек в ней).
- **Правило сноса FL:** оставить не-FL ветку, удалить FL-ветку. Редизайна разметки в задачах 1–5 не происходит.
- **Бэкенд не трогаем.** Поле `theme` в `types.ts:230` и в `fallback` остаётся; фронт просто перестаёт его читать.
- Команда сборки: `npm run build:frontend` (это `tsc --noEmit && vite build`). Запуск: `npm run dev` из корня.
- Приложение перезапускать после каждой задачи, затрагивающей внешний вид (требование пользователя).

---

## File Structure

| Файл | Ответственность после фазы 0 |
|---|---|
| `frontend/src/app/styles/tokens.css` | Единственный источник токенов, всё в `:root`, ~120 строк |
| `frontend/src/app/styles/fonts.css` | `@font-face` для Geist/Geist Mono (наконец импортируется) |
| `frontend/src/app/styles/base.css` | Ресеты и глобальные примитивы, без FL-блоков |
| `frontend/src/shared/config/categories.ts` | Маппинг категория → группа → цвет, группа `loop` вместо `bass` |
| `frontend/src/shared/ui/*.module.css` | Стили примитивов (новые файлы, по одному на компонент) |
| `frontend/src/shared/ui/*.tsx` | Разметка и логика примитивов, без инлайн-стилей и без `useState` под hover |
| `frontend/src/widgets/TopBar/TopBar.tsx` | Одна шапка вместо двух (`DawTopBar`/`MenuBar` удалены) |
| `frontend/src/shared/model/settings.ts` | Настройки без `applyTheme` и `localStorage` |
| `frontend/index.html` | Без Google Fonts и без FOUC-скрипта |

---

## Task 1: Снести FL из SoundTable

Начинаем с него: `isFl` здесь — обычный проп с дефолтом `false`, внешних зависимостей нет. Заодно чинится обход i18n.

**Files:**
- Modify: `frontend/src/widgets/SoundTable/SoundTable.tsx`
- Modify: `frontend/src/pages/samples/SamplesPage.tsx` (место, где проп передаётся)

**Interfaces:**
- Consumes: ничего
- Produces: `SoundTable` без пропа `isFl`. Задача 3 (`SamplesPage`) полагается на то, что проп уже удалён из сигнатуры.

- [ ] **Step 1: Найти все точки ветвления**

```bash
cd frontend && grep -n "isFl" src/widgets/SoundTable/SoundTable.tsx
```

Ожидается ~14 совпадений: объявление в интерфейсе (`:26`), параметр с дефолтом (`:54`), ветки в `cols` (`:59`), `hdrStyle` (`:97`), `ROW_H` (`:117`), строка (`:151`), заголовки (`:161-163`), скролл (`:169`), проброс в строку (`:187`), пустое состояние (`:193`), проп строки (`:215`, `:230`), `rowBg` (`:242`), `rowBoxShadow` (`:264`).

- [ ] **Step 2: Удалить проп из интерфейса и сигнатуры**

Убрать `isFl?: boolean;` (`:26`), `isFl = false,` из деструктуризации (`:54`), `isFl: boolean;` из интерфейса строки (`:230`) и `isFl,` из её деструктуризации (`:215`).

- [ ] **Step 3: Схлопнуть каждую тернарку в не-FL ветку**

Правило механическое: `isFl ? A : B` → `B`. Точки:

- `:59` — `const cols = isFl ? … : X` → оставить `X`
- `:97` — `hdrStyle` → не-FL объект
- `:117` — `ROW_H = showWaveform ? (isFl ? 60 : 70) : 44` → `showWaveform ? 70 : 44`
- `:151` — спред `...(isFl ? {…} : {…})` → оставить не-FL объект
- `:161-163` — **важно:** `isFl ? "SOUND TYPE" : t.harvest.colType` → `t.harvest.colType`. Аналогично `colSource`, `colSize`. Это чинит игнор языка.
- `:169` — `paddingRight: isFl ? 0 : 2` → `paddingRight: 2`
- `:187` — убрать `isFl={isFl}` из проброса
- `:193` — `color: isFl ? "var(--ink-on-work-dim)" : "var(--text-faint)"` → `var(--text-faint)`; `fontFamily: isFl ? "var(--font-sans)" : undefined` → удалить свойство
- `:242` — `rowBg` → не-FL ветка
- `:264` — `rowBoxShadow = isFl && (…) ? … : undefined` → удалить переменную и её использование

- [ ] **Step 4: Убрать передачу пропа из SamplesPage**

```bash
grep -n "isFl={" src/pages/samples/SamplesPage.tsx
```

Удалить `isFl={isFl}` в месте рендера `<SoundTable …>`. Саму переменную `isFl` в `SamplesPage` пока **не трогаем** — она используется в других местах этого файла, ими займётся задача 3.

- [ ] **Step 5: Проверить, что FL-токенов в файле не осталось**

```bash
grep -nE "isFl|var\(--(ink|chrome|work|panel|btn-hi|btn-lo|groove|lcd|rec|browser|rail|line-work)" src/widgets/SoundTable/SoundTable.tsx
```

Ожидается: пусто (exit code 1).

- [ ] **Step 6: Собрать**

```bash
cd .. && npm run build:frontend
```

Ожидается: сборка проходит, ошибок типов нет.

- [ ] **Step 7: Коммит**

```bash
git add frontend/src/widgets/SoundTable/SoundTable.tsx frontend/src/pages/samples/SamplesPage.tsx
git commit -m "Снос FL из SoundTable: удалён проп isFl и все ветки

Заодно исправлен обход i18n: заголовки таблицы больше не хардкодят
английский в FL-ветке и слушаются выбранного языка."
```

---

## Task 2: Снести FL из TopBar

**Files:**
- Modify: `frontend/src/widgets/TopBar/TopBar.tsx`

**Interfaces:**
- Consumes: ничего
- Produces: `TopBar(props: TopBarProps)` — рендерит одну шапку. Экспорт и сигнатура не меняются, `App.tsx` править не нужно.

- [ ] **Step 1: Удалить развилку**

`TopBar.tsx:12-16` сейчас:

```tsx
export function TopBar(props: TopBarProps) {
  const theme = useSettingsStore((s) => s.settings?.theme ?? "warm-dark");
  if (theme === "fl") return <DawTopBar {...props} />;
  return <CleanTopBar {...props} />;
}
```

Заменить на:

```tsx
export function TopBar(props: TopBarProps) {
  return <CleanTopBar {...props} />;
}
```

- [ ] **Step 2: Удалить FL-компоненты целиком**

Удалить функции `DawTopBar` (`:164-170`) и `MenuBar` (`:174-258`) полностью. `WindowControls` и `useDblClickMaximize` **оставить** — ими пользуется `CleanTopBar`.

- [ ] **Step 3: Убрать осиротевшие импорты**

После удаления `useSettingsStore` в файле больше не нужен — удалить его импорт (`:3`). Проверить, что `Tabs` и `Icons` ещё используются в `CleanTopBar` (используются — не трогать).

- [ ] **Step 4: Проверить**

```bash
cd frontend
grep -nE "isFl|DawTopBar|MenuBar|useSettingsStore|var\(--(ink|chrome|btn-hi|btn-lo)" src/widgets/TopBar/TopBar.tsx
```

Ожидается: пусто.

- [ ] **Step 5: Собрать**

```bash
cd .. && npm run build:frontend
```

Ожидается: успех. Если `tsc` ругается на неиспользуемый импорт — удалить его.

- [ ] **Step 6: Коммит**

```bash
git add frontend/src/widgets/TopBar/TopBar.tsx
git commit -m "Снос FL из TopBar: удалены DawTopBar и MenuBar"
```

---

## Task 3: Снести FL из SamplesPage и MidiSection

**Files:**
- Modify: `frontend/src/pages/samples/SamplesPage.tsx` (1043 строки)
- Modify: `frontend/src/pages/samples/MidiSection.tsx` (576 строк)

**Interfaces:**
- Consumes: `SoundTable` без пропа `isFl` (задача 1)
- Produces: ничего для последующих задач

- [ ] **Step 1: Оценить объём**

```bash
cd frontend && grep -c "isFl" src/pages/samples/SamplesPage.tsx src/pages/samples/MidiSection.tsx
```

Записать числа — они понадобятся, чтобы убедиться, что дошли до нуля.

- [ ] **Step 2: SamplesPage — удалить источник**

`SamplesPage.tsx:33-34`:

```tsx
const theme = settings?.theme ?? "fl";
const isFl = theme === "fl";
```

Удалить обе строки. Проверить, используется ли `settings` дальше в файле — если нет, убрать и его из деструктуризации `useSettingsStore`.

- [ ] **Step 3: SamplesPage — схлопнуть все ветки**

Каждое `isFl ? A : B` → `B`. Каждое `{isFl ? <FL/> : <Clean/>}` → `<Clean/>`. Каждое `{isFl && <FL/>}` → удалить блок целиком. Особое место — `:704`, комментарий `{/* Bottom bar — only for non-FL theme */}`: сам комментарий после схлопывания теряет смысл, удалить.

- [ ] **Step 4: MidiSection — то же**

`MidiSection.tsx:57`:

```tsx
const isFl = (settings?.theme ?? "fl") === "fl";
```

Удалить, схлопнуть ветки по тому же правилу.

- [ ] **Step 5: Проверить оба файла**

```bash
grep -nE "isFl|var\(--(ink|chrome|work|panel|btn-hi|btn-lo|groove|lcd|rec|browser|rail|line-work)" src/pages/samples/SamplesPage.tsx src/pages/samples/MidiSection.tsx
```

Ожидается: пусто.

- [ ] **Step 6: Собрать**

```bash
cd .. && npm run build:frontend
```

- [ ] **Step 7: Коммит**

```bash
git add frontend/src/pages/samples/SamplesPage.tsx frontend/src/pages/samples/MidiSection.tsx
git commit -m "Снос FL из SamplesPage и MidiSection"
```

---

## Task 4: Снести FL из AnalyticsPage и SettingsPage

**Files:**
- Modify: `frontend/src/pages/analytics/AnalyticsPage.tsx` (439 строк)
- Modify: `frontend/src/pages/settings/SettingsPage.tsx` (741 строка)

**Interfaces:**
- Consumes: ничего
- Produces: `SettingsPage` без `ThemePicker`. Задача 6 полагается на то, что выбора темы в UI уже нет.

- [ ] **Step 1: AnalyticsPage — удалить подписку на тему**

`AnalyticsPage.tsx:21-22`:

```tsx
const _theme = useSettingsStore((s) => s.settings?.theme);
const isFl = _theme === "fl";
```

Удалить обе строки **вместе с комментарием на `:13`** («При смене темы компонент перерендерится (подписка через _theme)…») — он описывает механику, которой больше нет. Убрать импорт `useSettingsStore`, если он больше не нужен.

Внимание: подписка `_theme` существовала, чтобы перерисовать графики Recharts при смене темы. Тема теперь одна и не меняется — перерисовывать не по чему, подписка не нужна.

- [ ] **Step 2: AnalyticsPage — схлопнуть ветки**

По тому же правилу.

- [ ] **Step 3: SettingsPage — удалить isFl**

`SettingsPage.tsx:138`: `const isFl = s?.theme === "fl";` — удалить, схлопнуть ветки.

- [ ] **Step 4: SettingsPage — удалить ThemePicker**

Удалить функцию `ThemePicker` (`:667-…`) целиком и её использование:

```tsx
<Field label={t.settings.theme}>
  <ThemePicker value={s.theme} onChange={(v) => patch({ theme: v })} />
</Field>
```

(`:202-203`) — удалить весь блок `<Field>`.

- [ ] **Step 5: Удалить ключи i18n**

**Осторожно:** `i18n/ru.ts` и `i18n/en.ts` содержат несохранённые правки пользователя. Редактировать точечно, не переписывать файлы целиком.

Удалить из `ru.ts` (`:101-104`) и `en.ts` (`:101-104`): `theme`, `themeWarmDark`, `themeLight`, `themeDark`. Ключ `desc` в `en.ts:99` содержит текст «Language, theme, export folder and processing options.» — поправить, убрав упоминание темы: «Language, export folder and processing options.» Найти и поправить парный русский `desc` в `ru.ts`.

- [ ] **Step 6: Проверить**

```bash
cd frontend
grep -nE "isFl|ThemePicker|themeWarmDark|themeLight|themeDark" src/pages/analytics/AnalyticsPage.tsx src/pages/settings/SettingsPage.tsx src/shared/i18n/ru.ts src/shared/i18n/en.ts
```

Ожидается: пусто.

- [ ] **Step 7: Собрать**

```bash
cd .. && npm run build:frontend
```

- [ ] **Step 8: Коммит**

```bash
git add frontend/src/pages/analytics/AnalyticsPage.tsx frontend/src/pages/settings/SettingsPage.tsx frontend/src/shared/i18n/ru.ts frontend/src/shared/i18n/en.ts
git commit -m "Снос FL из AnalyticsPage и SettingsPage, удалён ThemePicker"
```

---

## Task 5: Снести FL из PlayerPage и починить var(--border)

Самая крупная задача: файл 3017 строк. Делается отдельно именно поэтому.

**Files:**
- Modify: `frontend/src/pages/player/PlayerPage.tsx`

**Interfaces:**
- Consumes: ничего
- Produces: ничего. После этой задачи неопределённых токенов в проекте быть не должно — задача 6 на это опирается.

- [ ] **Step 1: Оценить объём**

```bash
cd frontend && grep -c "isFl" src/pages/player/PlayerPage.tsx && grep -c "var(--border)" src/pages/player/PlayerPage.tsx
```

Ожидается: `isFl` — несколько десятков, `var(--border)` — 17.

- [ ] **Step 2: Удалить источник**

`PlayerPage.tsx:2223-2224`:

```tsx
const _theme = useSettingsStore((s) => s.settings?.theme);
const isFl = _theme === "fl";
```

Удалить. Проверить, нужен ли ещё импорт `useSettingsStore` — в этом файле он может использоваться и для других настроек, тогда оставить.

- [ ] **Step 3: Схлопнуть ветки**

По правилу `isFl ? A : B` → `B`. Здесь их много и они вперемешку с логикой плеера — идти сверху вниз, не пропуская.

- [ ] **Step 4: Починить неопределённый var(--border)**

Заменить все 17 вхождений `var(--border)` на `var(--border-soft)`:

```bash
grep -n "var(--border)" src/pages/player/PlayerPage.tsx
```

Токен `--border` не определён ни в одной теме — эти рамки сейчас не рисуются вообще. После замены они появятся. **Это ожидаемое изменение внешнего вида, а не регрессия.** Часть вхождений уже уйдёт вместе с FL-ветками (например, `:932` и `:1705` — это тернарки `isFl ? "var(--line-work)" : "var(--border)"`), их правит шаг 3.

- [ ] **Step 5: Добить оставшиеся токены-сироты**

```bash
grep -nE "var\(--accent-deep|var\(--fw-normal|var\(--border-card|var\(--border[,)]" src/pages/player/PlayerPage.tsx
```

Ожидается: пусто. `--accent-deep` должен был уйти вместе с FL-ветками. Если где-то остался `--fw-normal` — заменить на `var(--fw-regular)`; если `--border-card` — на `var(--border-soft)`.

- [ ] **Step 6: Проверить весь файл**

```bash
grep -nE "isFl|var\(--(ink|chrome|work|panel|btn-hi|btn-lo|groove|lcd|rec|browser|rail|line-work)" src/pages/player/PlayerPage.tsx
```

Ожидается: пусто.

- [ ] **Step 7: Проверить, что FL-токенов не осталось нигде в проекте**

```bash
grep -rnE "isFl|var\(--(ink|chrome|work|panel|btn-hi|btn-lo|groove|lcd|rec|browser|rail|line-work)" src/ --include="*.tsx" --include="*.ts"
```

Ожидается: пусто. Остаются только `base.css` и `tokens.css` — их чистит задача 6.

- [ ] **Step 8: Собрать и запустить**

```bash
cd .. && npm run build:frontend && npm run dev
```

Осмотреть плеер: он должен работать и выглядеть как не-FL тема. Появившиеся рамки — ожидаемы (шаг 4).

- [ ] **Step 9: Коммит**

```bash
git add frontend/src/pages/player/PlayerPage.tsx
git commit -m "Снос FL из PlayerPage, починен неопределённый var(--border)

17 вхождений var(--border) не были определены ни в одной теме —
невалидный var() рушил объявление целиком и рамки не рисовались.
Заменено на var(--border-soft)."
```

---

## Task 6: Токены Console, шрифты, снос системы тем

Такт второй. С этого момента приложение выглядит по-новому.

**Files:**
- Modify: `frontend/src/app/styles/tokens.css` (переписывается)
- Modify: `frontend/src/app/styles/base.css`
- Modify: `frontend/src/main.tsx`
- Modify: `frontend/index.html`
- Modify: `frontend/src/shared/model/settings.ts`

**Interfaces:**
- Consumes: проект без FL-веток (задачи 1–5)
- Produces: токены в `:root`: `--bg-base`, `--bg-sunken`, `--surface-1..3`, `--surface-hover`, `--surface-active`, `--border-soft/medium/strong`, `--text-strong/body/muted/faint/on-accent`, `--accent`, `--accent-hover`, `--accent-press`, `--accent-soft`, `--accent-ring`, `--positive`, `--warning`, `--danger`, `--focus-ring`, `--font-sans`, `--font-mono`. Задачи 7–9 пользуются только ими.

- [ ] **Step 1: Переписать tokens.css**

Файл заменяется целиком. Четыре блока `[data-theme=…]` схлопываются в один `:root`.

```css
/* ============================================================
   TOKENS — Flapp / Console
   Одна тема. Никаких [data-theme] блоков.
   ============================================================ */

:root {
  /* ── Поверхности ── */
  --bg-base:        #0E0F11;
  --bg-sunken:      #0A0B0C;
  --surface-1:      #141619;
  --surface-2:      #1A1D21;
  --surface-3:      #22262B;
  --surface-hover:  #1A1D21;
  --surface-active: #22262B;

  /* ── Границы ── */
  --border-soft:    #22262B;
  --border-medium:  #2E343A;
  --border-strong:  #464D55;

  /* ── Текст ── */
  --text-strong:    #E6E9EC;
  --text-body:      #B4BAC1;
  --text-muted:     #6E767F;
  --text-faint:     #464D55;
  --text-on-accent: #0E0F11;

  /* ── Сигнал. Сектор круга 55–160° принадлежит системе,
       ни одна категория сюда не заходит. ── */
  --accent:         #C8F751;
  --accent-hover:   #D6FF6B;
  --accent-press:   #B2DE3F;
  --accent-soft:    #C8F7511a;
  --accent-ring:    #C8F75166;

  /* ── Статусы. --warning и --danger делят сектор с Drum Loop и 808:
       они живут в диалогах и тостах, где категорий не бывает. ── */
  --positive:       #7BE06A;
  --warning:        #F5A83C;
  --danger:         #FF5A4A;

  /* ── Категории. Соседние разведены минимум на 18°. ── */
  --cat-808:      #FF6352;  --cat-808-bg:      #FF635224;
  --cat-kick:     #FF8C45;  --cat-kick-bg:     #FF8C4524;
  --cat-drumloop: #F5A83C;  --cat-drumloop-bg: #F5A83C24;
  --cat-hihat:    #35D6BE;  --cat-hihat-bg:    #35D6BE24;
  --cat-openhat:  #40BCE8;  --cat-openhat-bg:  #40BCE824;
  --cat-perc:     #5B8FF5;  --cat-perc-bg:     #5B8FF524;
  --cat-loop:     #8F7CF8;  --cat-loop-bg:     #8F7CF824;
  --cat-vox:      #C77BF8;  --cat-vox-bg:      #C77BF824;
  --cat-fx:       #EE6BDB;  --cat-fx-bg:       #EE6BDB24;
  --cat-clap:     #FF6BB0;  --cat-clap-bg:     #FF6BB024;
  --cat-snare:    #FF5F80;  --cat-snare-bg:    #FF5F8024;
  --cat-unsorted: #6E767F;  --cat-unsorted-bg: #6E767F20;

  /* ── Семантические алиасы ── */
  --text-title:       var(--text-strong);
  --text-description: var(--text-muted);
  --surface-app:      var(--bg-base);
  --surface-panel:    var(--surface-1);
  --surface-card:     var(--surface-1);
  --surface-input:    var(--bg-sunken);
  --surface-well:     var(--bg-sunken);
  --row-hover:        var(--surface-2);
  --focus-ring:       0 0 0 2px var(--accent-ring);

  /* ── Тень. Console плоский: тень только у всплывающих поверхностей. ── */
  --shadow-pop: 0 16px 48px rgba(0, 0, 0, 0.7);

  /* ── Шрифты ── */
  --font-sans: 'Geist', system-ui, sans-serif;
  --font-mono: 'Geist Mono', ui-monospace, monospace;

  --fw-regular:  400;
  --fw-medium:   500;
  --fw-semibold: 600;
  --fw-bold:     700;

  --fs-display: 28px;
  --fs-h1:      22px;
  --fs-h2:      17px;
  --fs-body:    13px;
  --fs-sm:      12px;
  --fs-label:   11px;
  --fs-caption: 10px;

  --lh-tight:  1.15;
  --lh-snug:   1.35;
  --lh-normal: 1.5;

  --ls-tight: -0.01em;
  --ls-normal: 0;
  --ls-label:  0.1em;

  --section-label-transform: uppercase;

  /* ── Отступы: 4px-сетка ── */
  --space-0: 0;    --space-1: 4px;  --space-2: 8px;  --space-3: 12px;
  --space-4: 16px; --space-5: 20px; --space-6: 24px; --space-7: 32px;
  --space-8: 40px; --space-9: 56px; --space-10: 72px;

  --pad-card:    16px;
  --pad-row:     10px;
  --gap-section: 20px;
  --gap-control: 8px;

  --row-height:   23px;
  --input-height: 30px;
  --tap-min:      32px;

  /* ── Скругления: Console угловатый ── */
  --radius-sm:   2px;
  --radius-md:   3px;
  --radius-lg:   3px;
  --radius-xl:   4px;
  --radius-pill: 999px;

  --radius-card:   var(--radius-md);
  --radius-input:  var(--radius-sm);
  --radius-row:    var(--radius-sm);
  --radius-button: var(--radius-sm);
  --radius-badge:  var(--radius-sm);

  /* ── Движение ── */
  --ease-out:        cubic-bezier(0.22, 0.61, 0.36, 1);
  --ease-in-out:     cubic-bezier(0.45, 0, 0.25, 1);
  --dur-fast:        90ms;
  --dur-base:        140ms;
  --dur-slow:        220ms;
  --transition-base: all var(--dur-base) var(--ease-out);
}
```

Обрати внимание, чего здесь **нет** и почему: `--surface-4`, `--row-zebra` (строки разделяет hairline, не заливка), `--accent-amber` и `--accent-softer` (градиентов в Console нет), `--cat-cymbal` (мёртвый токен), `--cat-bass` (переименован в `--cat-loop`), `--shadow-sm/md/lg`, `--glow-*`, весь набор `--btn-*` (существовал ради FL-переопределений), `--type-*-size/weight` (не использовались).

- [ ] **Step 2: Подключить шрифты**

`main.tsx` — добавить импорт **перед** `tokens.css`:

```tsx
import "@/app/styles/fonts.css";
import "@/app/styles/tokens.css";
import "@/app/styles/base.css";
```

`fonts.css` уже содержит корректные `@font-face` для Geist и Geist Mono и указывает на реальные файлы в `src/app/styles/fonts/`. Менять его не нужно — только импортировать.

- [ ] **Step 3: Почистить index.html**

Удалить `<link rel="preconnect" href="https://fonts.googleapis.com" />`, `<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />` и `<link href="https://fonts.googleapis.com/css2?family=Geist…" rel="stylesheet" />`.

Удалить FOUC-скрипт целиком:

```html
<script>
  (function () {
    var t = localStorage.getItem('flapp-theme') || 'fl';
    if (t === 'warm-dark') t = 'fl';
    document.documentElement.setAttribute('data-theme', t);
    var bg = { 'fl': '#2b3039', 'dark': '#16161A', 'light': '#F3F1EE' };
    document.documentElement.style.background = bg[t] || '#2b3039';
  })();
</script>
```

Вместе с комментарием над ним. Тема одна — подменять нечего.

Во встроенном `<style>` заменить `html { background: #2b3039; }` на `html { background: #0E0F11; }`.

- [ ] **Step 4: Выпилить applyTheme**

`settings.ts` — удалить функцию `applyTheme` (`:56-59`) с комментарием (`:54-55`), вызов в `load()` (`:72`), вызов в ветке `catch` (`:75`) и блок в `update()` (`:91-93`):

```tsx
if (patch.theme) {
  applyTheme(patch.theme);
}
```

Поле `theme: "fl"` в `fallback` (`:23`) **оставить** — тип `Settings` его требует, бэкенд его хранит. Добавить над ним комментарий:

```tsx
  // Рудимент: бэкенд хранит поле, фронт его не читает. Тема одна — Console.
  theme: "fl",
```

- [ ] **Step 5: Почистить base.css**

Удалить всё от комментария `/* ── FL theme overrides ─── */` до конца файла: блоки `[data-theme="fl"]`, `@keyframes fl-playhead`, `@keyframes fl-led-pulse`, `.fl-rack` и его псевдоэлементы.

Добавить класс для числовых ячеек — без него колонки не выровняются:

```css
/* числа в колонках: моноширинные и одинаковой ширины */
.num {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}
```

- [ ] **Step 6: Проверить, что от системы тем ничего не осталось**

```bash
cd frontend
grep -rn "data-theme\|flapp-theme\|applyTheme\|fonts.googleapis" src/ index.html
```

Ожидается: пусто.

```bash
grep -rn "var(--row-zebra)\|var(--accent-amber)\|var(--accent-softer)\|var(--surface-4)\|var(--cat-cymbal)\|var(--cat-bass)\|var(--btn-" src/
```

Ожидается: только `ProgressBar.tsx` с `--accent-amber` и `Button.tsx` с `--btn-*` — их чинит задача 8. Если всплыло что-то ещё — исправить здесь: заменить на ближайший живой токен.

- [ ] **Step 7: Собрать и запустить**

```bash
cd .. && npm run build:frontend && npm run dev
```

Проверить:
- приложение стартует в тёмном Console без вспышки другой темы
- шрифт — Geist (в DevTools → Computed → `font-family`)
- DevTools → Network при старте: **ни одного запроса к `fonts.googleapis.com`**
- отключить сеть, перезапустить — шрифты на месте

- [ ] **Step 8: Коммит**

```bash
git add frontend/src/app/styles/tokens.css frontend/src/app/styles/base.css frontend/src/main.tsx frontend/index.html frontend/src/shared/model/settings.ts
git commit -m "Токены Console в :root, локальные шрифты, снос системы тем

- четыре [data-theme] блока схлопнуты в один :root (445 -> ~180 строк)
- Geist/Geist Mono подключены локально, Google Fonts CDN удалён:
  приложение больше не зависит от сети при старте
- FOUC-скрипт удалён — он сам вызывал FOUC, подменяя warm-dark на fl
- удалены мёртвые токены: --cat-cymbal, --row-zebra, --accent-amber,
  --surface-4, --glow-*, --shadow-sm/md/lg, весь набор --btn-*"
```

---

## Task 7: Новые цвета категорий

**Files:**
- Modify: `frontend/src/shared/config/categories.ts`

**Interfaces:**
- Consumes: токены `--cat-*` из задачи 6
- Produces: `ColorGroup` с членом `"loop"` (вместо `"bass"`), `GROUP_HEX` с новыми значениями. `CategoryTag` и `SoundTable` зовут `groupOf()`/`groupColor()`/`groupHex()` — сигнатуры не меняются.

- [ ] **Step 1: Переименовать группу bass → loop**

`categories.ts:1-13` — в типе `ColorGroup` заменить `| "bass"      // repurposed for Loop` на `| "loop"`. Заодно удалить `| "cymbal"`, если он там есть (в текущем типе его нет — проверить).

- [ ] **Step 2: Обновить маппинг**

`:31` — `Loop: "bass",      // --cat-bass (dusty violet) used for Loop` → `Loop: "loop",`. Комментарий больше не нужен: имя перестало врать.

- [ ] **Step 3: Обновить GROUP_HEX**

Заменить блок `:46-59` целиком:

```ts
export const GROUP_HEX: Record<ColorGroup, string> = {
  "808":    "#FF6352",
  kick:     "#FF8C45",
  drumloop: "#F5A83C",
  hihat:    "#35D6BE",
  openhat:  "#40BCE8",
  perc:     "#5B8FF5",
  loop:     "#8F7CF8",
  vox:      "#C77BF8",
  fx:       "#EE6BDB",
  clap:     "#FF6BB0",
  snare:    "#FF5F80",
  unsorted: "#6E767F",
};
```

- [ ] **Step 4: Поправить устаревший комментарий**

`CategoryTag.tsx:11-12` утверждает: «The label shows the precise 40-category name; the color comes from its 13-group mapping». Категорий 11, групп 12. Заменить на:

```tsx
// Метка с цветовым кодом типа звука. Подпись — точное имя категории с бэкенда,
// цвет — из маппинга на 12 групп (--cat-* в tokens.css).
```

- [ ] **Step 5: Проверить полноту**

```bash
cd frontend && npm run build:frontend
```

`Record<ColorGroup, string>` заставит `tsc` ругаться, если какая-то группа пропущена или лишняя. Это и есть проверка — тест не нужен, типы уже её делают.

- [ ] **Step 6: Осмотреть**

```bash
cd .. && npm run dev
```

Открыть список звуков. Все 11 категорий должны различаться цветом. 808 и Kick — теперь заметно разные (было 6° разницы, стало 18°).

- [ ] **Step 7: Коммит**

```bash
git add frontend/src/shared/config/categories.ts frontend/src/shared/ui/CategoryTag.tsx
git commit -m "Новые цвета категорий: разведены по кругу, группа bass переименована в loop

Было: все 11 категорий в тёплом секторе, 808 (#D98C6A) и Kick (#E0926A)
отличались на 6 градусов оттенка и не различались боковым зрением.
Стало: минимум 18 градусов между соседями, сектор 55-160° оставлен
под сигнальный цвет."
```

---

## Task 8: Button и Card на CSS Modules

Первые примитивы. На них отрабатывается приём, дальше он повторяется.

**Files:**
- Create: `frontend/src/shared/ui/Button.module.css`
- Create: `frontend/src/shared/ui/Card.module.css`
- Modify: `frontend/src/shared/ui/Button.tsx`
- Modify: `frontend/src/shared/ui/Card.tsx`

**Interfaces:**
- Consumes: токены из задачи 6
- Produces: `Button` с тем же публичным API — `variant?: "primary" | "secondary" | "ghost" | "danger"`, `size?: "sm" | "md" | "lg"`, `icon?: ReactNode`, `full?: boolean`, плюс все атрибуты `<button>`. `Card` — `padding?: number | string`, `elevated?: boolean`. Ни один вызывающий код менять не нужно.

- [ ] **Step 1: Убедиться, что CSS Modules работают из коробки**

Ставить ничего не нужно: Vite поддерживает `*.module.css` без конфигурации. Проверяется первым же билдом на шаге 5.

- [ ] **Step 2: Написать Button.module.css**

```css
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border-radius: var(--radius-button);
  font-family: var(--font-sans);
  white-space: nowrap;
  cursor: pointer;
  border: 1px solid transparent;
  transition: background var(--dur-fast) var(--ease-out),
              border-color var(--dur-fast) var(--ease-out),
              color var(--dur-fast) var(--ease-out);
}
.btn:disabled { opacity: 0.45; cursor: not-allowed; }
.btn:active:not(:disabled) { transform: translateY(1px); }
.full { width: 100%; }

/* размеры */
.sm { height: 26px; padding: 0 10px; font-size: var(--fs-sm); }
.md { height: 30px; padding: 0 14px; font-size: var(--fs-sm); }
.lg { height: 36px; padding: 0 18px; font-size: var(--fs-body); }

/* варианты */
.primary {
  background: var(--accent);
  color: var(--text-on-accent);
  font-weight: var(--fw-semibold);
}
.primary:hover:not(:disabled) { background: var(--accent-hover); }
.primary:active:not(:disabled) { background: var(--accent-press); }

.secondary {
  background: transparent;
  border-color: var(--border-medium);
  color: var(--text-body);
  font-weight: var(--fw-medium);
}
.secondary:hover:not(:disabled) {
  border-color: var(--border-strong);
  background: var(--surface-2);
}

.ghost { background: transparent; color: var(--text-muted); font-weight: var(--fw-medium); }
.ghost:hover:not(:disabled) { background: var(--surface-2); color: var(--text-body); }

.danger {
  background: transparent;
  border-color: color-mix(in srgb, var(--danger) 40%, transparent);
  color: var(--danger);
  font-weight: var(--fw-medium);
}
.danger:hover:not(:disabled) { background: color-mix(in srgb, var(--danger) 14%, transparent); }

.icon { display: inline-flex; font-size: 1.1em; }
```

- [ ] **Step 3: Переписать Button.tsx**

`useState(hover)`, `useState(active)` и четыре обработчика мыши уходят целиком — это они вызывали ре-рендер на каждое движение мыши.

```tsx
import React from "react";
import s from "./Button.module.css";

type Variant = "primary" | "secondary" | "ghost" | "danger";
type Size = "sm" | "md" | "lg";

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  icon?: React.ReactNode;
  full?: boolean;
}

export function Button({
  children,
  variant = "secondary",
  size = "md",
  icon = null,
  full = false,
  className,
  ...rest
}: ButtonProps) {
  const cls = [s.btn, s[size], s[variant], full ? s.full : "", className ?? ""]
    .filter(Boolean)
    .join(" ");
  return (
    <button className={cls} {...rest}>
      {icon ? <span className={s.icon}>{icon}</span> : null}
      {children}
    </button>
  );
}
```

Проп `style` больше не перечисляется явно — он приезжает через `...rest` как часть атрибутов `<button>`, так что вызывающий код, передающий `style`, продолжит работать.

- [ ] **Step 4: Card.module.css и Card.tsx**

```css
.card {
  background: var(--surface-card);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-card);
  padding: var(--pad-card);
}
.elevated { box-shadow: var(--shadow-pop); }
```

```tsx
import React from "react";
import s from "./Card.module.css";

interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  padding?: number | string;
  elevated?: boolean;
}

export function Card({ children, padding, elevated = false, className, style, ...rest }: CardProps) {
  const cls = [s.card, elevated ? s.elevated : "", className ?? ""].filter(Boolean).join(" ");
  return (
    <div className={cls} style={padding != null ? { padding, ...style } : style} {...rest}>
      {children}
    </div>
  );
}
```

`padding` остаётся инлайном намеренно: это динамическое значение из пропа, в CSS его не выразить.

- [ ] **Step 5: Собрать**

```bash
cd frontend && cd .. && npm run build:frontend
```

Ожидается: успех. Если Vite не понял `*.module.css` — значит что-то не так с версией, но настройки это не требует.

- [ ] **Step 6: Проверить, что ре-рендеры ушли**

```bash
npm run dev
```

React DevTools → Profiler → Highlight updates when components render. Поводить мышью по кнопке: **подсветки быть не должно**. Раньше каждое наведение перерисовывало компонент.

- [ ] **Step 7: Коммит**

```bash
git add frontend/src/shared/ui/Button.tsx frontend/src/shared/ui/Button.module.css frontend/src/shared/ui/Card.tsx frontend/src/shared/ui/Card.module.css
git commit -m "Button и Card на CSS Modules

useState(hover) и useState(active) удалены: наведение мыши больше
не вызывает ре-рендер React, состояния живут в :hover/:active."
```

---

## Task 9: Input, Checkbox, PlayButton, Tabs на CSS Modules

Четыре компонента с одинаковой болезнью: `useState` под hover/focus.

**Files:**
- Create: `frontend/src/shared/ui/Input.module.css`, `Checkbox.module.css`, `PlayButton.module.css`, `Tabs.module.css`
- Modify: `frontend/src/shared/ui/Input.tsx`, `Checkbox.tsx`, `PlayButton.tsx`, `Tabs.tsx`

**Interfaces:**
- Consumes: токены из задачи 6
- Produces: публичные API без изменений — `Input({icon?, ...inputAttrs})`, `Checkbox({checked?, onChange?, label?, disabled?})`, `PlayButton({playing?, size?, onClick?})`, `Tabs({tabs, value, onChange?})` + экспорт типа `TabItem`.

- [ ] **Step 1: Input — заменить useState(focus) на :focus-within**

`Input.module.css`:

```css
.wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  height: var(--input-height);
  padding: 0 10px;
  background: var(--surface-input);
  border: 1px solid var(--border-medium);
  border-radius: var(--radius-input);
  transition: border-color var(--dur-fast) var(--ease-out);
}
.wrap:focus-within { border-color: var(--accent); box-shadow: var(--focus-ring); }
.icon { display: inline-flex; color: var(--text-faint); flex-shrink: 0; }
.wrap:focus-within .icon { color: var(--accent); }
.field {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: none;
  outline: none;
  color: var(--text-body);
  font-family: var(--font-sans);
  font-size: var(--fs-body);
}
```

`Input.tsx` — `useState(focus)` и обработчики `onFocus`/`onBlur` удаляются, всё делает `:focus-within`:

```tsx
import React from "react";
import s from "./Input.module.css";

interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  icon?: React.ReactNode;
  wrapClassName?: string;
}

export function Input({ icon = null, wrapClassName, style, ...rest }: InputProps) {
  return (
    <div className={[s.wrap, wrapClassName ?? ""].filter(Boolean).join(" ")} style={style}>
      {icon ? <span className={s.icon}>{icon}</span> : null}
      <input className={s.field} {...rest} />
    </div>
  );
}
```

**Изменение API:** раньше `style` уходил на обёртку, а тип был `Omit<…, "style">`. Теперь `style` тоже на обёртке, но тип обычный. Поведение то же.

- [ ] **Step 2: Checkbox**

```css
.label {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  user-select: none;
  color: var(--text-body);
  font-size: var(--fs-body);
}
.label.disabled { opacity: 0.5; cursor: not-allowed; }
.box {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  border-radius: var(--radius-sm);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--surface-input);
  border: 1px solid var(--border-medium);
  transition: background var(--dur-fast) var(--ease-out),
              border-color var(--dur-fast) var(--ease-out);
}
.label:hover:not(.disabled) .box { border-color: var(--border-strong); }
.box.checked { background: var(--accent); border-color: transparent; }
```

`Checkbox.tsx` — удалить `useState(hover)` и оба обработчика; hover делает `.label:hover .box`. Класс `checked` вешать по пропу. Галочка (`<svg>` с `stroke="var(--text-on-accent)"`) остаётся как есть.

- [ ] **Step 2a: Заменить размер бокса**

Было `width: 20, height: 20, borderRadius: "7px"`. В Console — 16px и `--radius-sm` (2px). Скруглённый квадрат на 7px противоречит угловатости направления.

- [ ] **Step 3: PlayButton**

```css
.btn {
  border-radius: var(--radius-pill);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  border: 1px solid var(--border-medium);
  background: transparent;
  color: var(--accent);
  transition: background var(--dur-fast) var(--ease-out),
              border-color var(--dur-fast) var(--ease-out);
}
.btn:hover { background: var(--accent-soft); border-color: transparent; }
.btn.playing {
  background: var(--accent);
  border-color: transparent;
  color: var(--text-on-accent);
}
```

`PlayButton.tsx` — удалить `useState(hover)`. Убрать `transform: scale(1.06)` на hover: в Console кнопки не растут, отклик даётся цветом. `size` остаётся инлайном (`style={{ width: size, height: size }}`) — это динамическое значение из пропа.

- [ ] **Step 4: Tabs**

```css
.track {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 2px;
  background: var(--surface-1);
  border-radius: var(--radius-sm);
}
.pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 12px;
  border: none;
  border-radius: var(--radius-sm);
  cursor: pointer;
  font-family: var(--font-sans);
  font-size: var(--fs-sm);
  font-weight: var(--fw-medium);
  letter-spacing: var(--ls-label);
  text-transform: uppercase;
  background: transparent;
  color: var(--text-muted);
  white-space: nowrap;
  transition: background var(--dur-fast) var(--ease-out),
              color var(--dur-fast) var(--ease-out);
}
.pill:hover:not(.active) { background: var(--surface-3); color: var(--text-body); }
.pill.active {
  background: var(--accent);
  color: var(--text-on-accent);
  font-weight: var(--fw-semibold);
}
.ico { display: inline-flex; }
```

`Tabs.tsx` — `TabPill` теряет `useState(hover)`. Активная вкладка в Console — заливка акцентом, а не мягкая подложка: она обязана читаться как «ты здесь».

- [ ] **Step 5: Собрать**

```bash
npm run build:frontend
```

- [ ] **Step 6: Проверить, что useState под hover нигде не остался**

```bash
cd frontend && grep -rn "useState(false)" src/shared/ui/
```

Ожидается: пусто. Если что-то нашлось — это оставшийся hover/focus, его надо перенести в CSS.

```bash
grep -rn "onMouseEnter\|onMouseLeave\|onMouseDown\|onMouseUp" src/shared/ui/
```

Ожидается: пусто.

- [ ] **Step 7: Осмотреть**

```bash
cd .. && npm run dev
```

Проверить каждый: поле ввода даёт лаймовую рамку по фокусу, чекбокс заливается лаймом, кнопка play меняет цвет без скачка размера, активная вкладка залита акцентом. React DevTools → Highlight updates: наведение мышью ничего не перерисовывает.

- [ ] **Step 8: Коммит**

```bash
git add frontend/src/shared/ui/Input.tsx frontend/src/shared/ui/Input.module.css frontend/src/shared/ui/Checkbox.tsx frontend/src/shared/ui/Checkbox.module.css frontend/src/shared/ui/PlayButton.tsx frontend/src/shared/ui/PlayButton.module.css frontend/src/shared/ui/Tabs.tsx frontend/src/shared/ui/Tabs.module.css
git commit -m "Input, Checkbox, PlayButton, Tabs на CSS Modules

Четыре useState под hover/focus удалены. Input использует
:focus-within вместо onFocus/onBlur."
```

---

## Task 10: CategoryTag, ProgressBar, StatStrip, DropZone, WindowControls

Остатки. `ProgressBar` попутно теряет градиент, `WindowControls` — ручное дёрганье DOM.

**Files:**
- Create: `frontend/src/shared/ui/CategoryTag.module.css`, `ProgressBar.module.css`, `StatStrip.module.css`, `DropZone.module.css`
- Create: `frontend/src/widgets/TopBar/TopBar.module.css`
- Modify: одноимённые `.tsx` + `frontend/src/widgets/TopBar/TopBar.tsx`

**Interfaces:**
- Consumes: токены из задачи 6, `groupOf`/`groupColor` из задачи 7
- Produces: публичные API без изменений.

- [ ] **Step 1: CategoryTag**

```css
.tag {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 6px;
  border-radius: var(--radius-badge);
  font-size: var(--fs-caption);
  font-weight: var(--fw-semibold);
  letter-spacing: var(--ls-label);
  text-transform: uppercase;
  line-height: 1.4;
  white-space: nowrap;
}
.dot { width: 5px; height: 5px; border-radius: 50%; flex-shrink: 0; background: currentColor; }
```

Цвет и фон приходят из `groupColor()` — они динамические, остаются инлайном:

```tsx
<span className={s.tag} style={{ background: c.bg, color: c.color, ...style }}>
```

Точка красится через `background: currentColor` в CSS — отдельный инлайн ей больше не нужен.

- [ ] **Step 2: ProgressBar — убрать градиент**

```css
.wrap { display: flex; flex-direction: column; gap: 6px; width: 100%; }
.head { display: flex; justify-content: space-between; align-items: center; gap: 10px; }
.caption {
  font-size: var(--fs-sm);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pct {
  font-size: var(--fs-sm);
  color: var(--text-faint);
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}
.track { height: 4px; width: 100%; background: var(--surface-3); overflow: hidden; }
.fill {
  height: 100%;
  background: var(--accent);
  transition: width var(--dur-slow) var(--ease-out);
}
```

`ProgressBar.tsx:32` заливал полосу `linear-gradient(90deg, var(--accent) 0%, var(--accent-amber) 100%)`. Токена `--accent-amber` больше нет (удалён в задаче 6), и градиенту в Console не место — плоский `--accent`. Ширина (`width: pct + "%"`) остаётся инлайном: значение динамическое.

Проценты получают `tabular-nums` — иначе число дёргается при каждом обновлении.

- [ ] **Step 3: StatStrip и DropZone**

Прочитать текущие файлы, перенести инлайн-стили в модуль по тому же приёму: статика — в CSS, динамика из пропов — инлайном, любое `useState` под hover — в `:hover`.

- [ ] **Step 4: WindowControls — убрать дёрганье DOM**

`TopBar.tsx:72-74` меняет `style.background` руками:

```tsx
onMouseDown={(e) => { (e.currentTarget as HTMLButtonElement).style.background = pressed; }}
onMouseUp={(e) => { (e.currentTarget as HTMLButtonElement).style.background = base; }}
onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = base; }}
```

А `useState(groupHover)` показывает символы на всех трёх точках при наведении на группу. Оба приёма заменяются на CSS:

```css
.dots { display: flex; gap: 6px; align-items: center; }
.dot {
  width: 11px;
  height: 11px;
  border-radius: 50%;
  border: none;
  padding: 0;
  flex-shrink: 0;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-shadow: inset 0 0 0 0.5px rgba(0, 0, 0, 0.22);
}
.dot > span {
  font-size: 8px;
  line-height: 1;
  font-weight: 900;
  color: transparent;
  user-select: none;
}
/* символы проступают при наведении на всю группу — как на macOS */
.dots:hover .dot > span { color: rgba(0, 0, 0, 0.55); }

.close { background: #ff5f57; }
.close:active { background: #c0403a; }
.min { background: #febc2e; }
.min:active { background: #c0920e; }
.max { background: #28c840; }
.max:active { background: #1d9630; }
```

Цвета точек — намеренный хардкод: это системные цвета окна macOS-стиля, они не участвуют в теме и не должны меняться вместе с ней.

`.dots:hover .dot > span` заменяет `useState(groupHover)` целиком.

- [ ] **Step 5: Проверить, что инлайна и ручного DOM не осталось**

```bash
cd frontend
grep -rn "useState\|onMouseEnter\|onMouseLeave\|onMouseDown\|onMouseUp\|\.style\." src/shared/ui/ src/widgets/TopBar/
```

Ожидается: пусто. Исключение — `useDblClickMaximize` в `TopBar.tsx` использует `useCallback`, не `useState`; он остаётся.

- [ ] **Step 6: Собрать**

```bash
cd .. && npm run build:frontend
```

- [ ] **Step 7: Коммит**

```bash
git add frontend/src/shared/ui/ frontend/src/widgets/TopBar/
git commit -m "Остальные примитивы на CSS Modules

ProgressBar потерял градиент (плоский accent), WindowControls —
ручное дёрганье style.background и useState(groupHover): символы
на точках теперь проступают через .dots:hover."
```

---

## Task 11: Финальная проверка фазы

Не пишет код — доказывает, что фаза закрыта.

**Files:** ничего не меняет (кроме исправлений, если проверка что-то найдёт)

- [ ] **Step 1: Ни следа FL**

```bash
cd frontend
grep -rn "isFl\|data-theme\|flapp-theme\|DawTopBar\|fl-rack\|ThemePicker" src/ index.html
grep -rnE "var\(--(ink|chrome|work|panel|btn-hi|btn-lo|groove|lcd|rec|browser|rail|line-work)" src/
```

Оба: пусто.

- [ ] **Step 2: Ни одного неопределённого токена**

```bash
for t in --border --accent-deep --fw-normal --border-card --accent-amber --row-zebra --cat-cymbal --cat-bass --surface-4 --accent-softer; do
  n=$(grep -rohE "var\($t[,)]" src/ 2>/dev/null | wc -l)
  echo "$t → $n"
done
```

Все: `0`.

- [ ] **Step 3: Ре-рендеров на мышь нет**

```bash
grep -rn "onMouseEnter\|onMouseLeave" src/shared/ui/ src/widgets/
```

Ожидается: пусто.

- [ ] **Step 4: Сеть не нужна**

```bash
grep -rn "fonts.googleapis\|fonts.gstatic" src/ index.html
```

Ожидается: пусто.

- [ ] **Step 5: Сборка**

```bash
cd .. && npm run build:frontend
```

Ожидается: успех без ошибок и предупреждений о типах.

- [ ] **Step 6: Осмотр вживую**

```bash
npm run dev
```

Пройтись по чек-листу спека §6:
- стартует в Console, без вспышки другой темы
- шрифт Geist, цифры в колонках выровнены (`tabular-nums`)
- DevTools → Network: ни одного обращения к `fonts.googleapis.com`
- все 11 категорий различимы, 808 и Kick заметно разные
- наведение мышью не подсвечивает компоненты в React DevTools
- отключить сеть, перезапустить — шрифты на месте
- пройти по всем четырём вкладкам: звуки, плеер, настройки — ничего не сломано, выбора темы в настройках нет

- [ ] **Step 7: Зафиксировать, что осталось на потом**

Страницы структурно прежние — это ожидаемо и записано в спеке §5. Компоновка C, виртуализация и разбор `PlayerPage.tsx` — фазы 1–3.

Если осмотр выявил расхождение мокапа с реальностью (спек §7 предупреждает про узость Geist на длинных именах файлов) — записать наблюдение в спек фазы 1, **не чинить здесь**.

---

## Self-Review

**Покрытие спека:**

| Требование спека | Задача |
|---|---|
| §4.1 Токены Console в `:root` | 6 |
| §4.1 Удалить `--cat-cymbal`, `--row-zebra`, `--accent-amber` | 6 (токены), 10 (`ProgressBar`) |
| §4.1 Переименовать `--cat-bass` → `--cat-loop` | 6 (токен), 7 (`ColorGroup`) |
| §4.2 Шрифты Geist локально, `tabular-nums` | 6 |
| §4.3 `index.html` — CDN и FOUC-скрипт | 6 |
| §4.4 Снос системы тем, `theme` остаётся рудиментом | 4 (`ThemePicker`, i18n), 6 (`applyTheme`) |
| §4.5 Примитивы на CSS Modules | 8, 9, 10 |
| §4.6 Снос FL из 7 файлов | 1, 2, 3, 4, 5 |
| §4.6 Токены-сироты (`--border` и др.) | 5, проверка в 11 |
| §4.6 i18n-обход в `SoundTable` | 1 |
| §4.6 Устаревший комментарий в `categories.ts` | 7 |
| §6 Проверка | вшита в каждую задачу + 11 |

Незакрытых требований нет.

**Согласованность типов:** `ColorGroup` получает член `"loop"` в задаче 7; `GROUP_HEX` объявлен как `Record<ColorGroup, string>`, поэтому `tsc` сам поймает расхождение. Токен `--cat-loop` заводится в задаче 6 — то есть раньше, чем на него сошлётся `groupColor()` в задаче 7. Публичные API примитивов в задачах 8–10 не меняются, вызывающий код не трогаем.

**Проверка на заглушки:** шаги с кодом содержат код целиком. Единственное место, где кода нет, — задача 10, шаг 3 (`StatStrip`, `DropZone`): там сказано «прочитать текущие файлы и перенести по тому же приёму». Приём к этому моменту показан полностью на шести компонентах, а сами файлы маленькие (73 и ~60 строк) и по структуре повторяют уже сделанные. Дублировать их разметку в план смысла нет.
