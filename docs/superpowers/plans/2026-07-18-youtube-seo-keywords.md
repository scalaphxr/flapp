# YouTube SEO Keywords Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Auto-generate a large SEO keyword wall (`{keywords}`) and a `#ArtistTypeBeat` hashtag line (`{hashtags}`) inside the YouTube description, sourced from the beat's own artists plus a persistent, auto-growing personal roster.

**Architecture:** All expansion is pure frontend TypeScript in a new dependency-free module (`shared/lib/ytKeywords.ts`) wired into the existing `renderYtVars` template engine, so the dialog preview and the actual upload are byte-identical and offline. The backend only gains two settings fields and a new default description template. The roster auto-grows on publish.

**Tech Stack:** Go 1.23 (settings), React + TypeScript (dialog + logic), Node 22 `--experimental-strip-types` for unit tests (no vitest — the frontend has no test runner and we are not adding one).

## Global Constraints

- Preview and upload MUST use the same expansion code path (`renderYtVars`) — no divergent renders.
- No network calls in the keyword wall — it is deterministic/offline. The existing tags-field ✨ button (`GenerateTags`, live YouTube suggestions) stays untouched.
- `{keywords}` capped to a ~4000-char budget (YouTube description limit is 5000).
- `{hashtags}` capped at 15 tags (YouTube hides descriptions with spammy hashtag counts).
- Type-artists = a beat's recognized authors MINUS the producer's own nick (`bapebrazy type beat` is not a search term; `bankroll fresh type beat` is).
- Dedup everywhere is case-insensitive.
- Existing template placeholders (`{name} {type} {bpm} {key} {nick} {authors}`) and their behavior MUST NOT change.
- Do NOT perform a real YouTube upload — it is irreversible and reserved for the user.

---

### Task 1: Backend settings — roster fields + new default description

**Files:**
- Modify: `backend/internal/infrastructure/settings/settings.go` (Settings struct ~line 44-52; Defaults ~line 55-82; `defaultDescription` const ~line 86-95)
- Test: `backend/internal/infrastructure/settings/settings_seo_test.go` (create)

**Interfaces:**
- Produces: `Settings.YtKeywordRoster string` (json `ytKeywordRoster`), `Settings.YtRosterAutoGrow bool` (json `ytRosterAutoGrow`); `defaultDescription` now contains `{keywords}` and `{hashtags}`.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/infrastructure/settings/settings_seo_test.go`:

```go
package settings

import "strings"
import "testing"

func TestDefaultsSEO(t *testing.T) {
	d := Defaults()
	if !d.YtRosterAutoGrow {
		t.Error("YtRosterAutoGrow should default to true")
	}
	if d.YtKeywordRoster != "" {
		t.Errorf("YtKeywordRoster should default to empty, got %q", d.YtKeywordRoster)
	}
	for _, ph := range []string{"{keywords}", "{hashtags}", "{nick}", "{bpm}"} {
		if !strings.Contains(d.YtDescription, ph) {
			t.Errorf("default description missing %s", ph)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/infrastructure/settings/ -run TestDefaultsSEO -v`
Expected: FAIL (compile error: `YtRosterAutoGrow`/`YtKeywordRoster` undefined).

- [ ] **Step 3: Add the struct fields**

In `settings.go`, after the `YtPrivacy` field (~line 51) inside the `Settings` struct, add:

```go
	YtKeywordRoster  string `json:"ytKeywordRoster"`  // пул ключевиков для {keywords}, через запятую/перенос
	YtRosterAutoGrow bool   `json:"ytRosterAutoGrow"` // пополнять ростер артистами опубликованных битов
```

- [ ] **Step 4: Set the defaults**

In `Defaults()`, after the `YtPrivacy: "public",` line (~line 80), add:

```go
			YtKeywordRoster:  "",
			YtRosterAutoGrow: true,
```

- [ ] **Step 5: Replace the default description**

Replace the entire `defaultDescription` const (~line 86-95) with:

```go
const defaultDescription = `{type} Type Beat "{name}"

• BPM: {bpm}  |  Key: {key}
• Prod. {nick} — leave a like if you enjoyed 💯🤝
• Email for WAV / exclusive: your@email.com

[FREE] for non-profit use — you MUST credit (prod. {nick}) in your title.
For profit / exclusive rights: contact me.
Unauthorized use (no lease/exclusive rights) is copyright infringement, subject to DMCA takedown.

IGNORE ↓
________________________________________

{keywords}

{hashtags}`
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd backend && go test ./internal/infrastructure/settings/ -run TestDefaultsSEO -v && go build ./...`
Expected: PASS, build clean.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/infrastructure/settings/settings.go backend/internal/infrastructure/settings/settings_seo_test.go
git commit -m "YouTube: настройки ростера ключевиков + описание с {keywords}/{hashtags}"
```

---

### Task 2: Frontend settings type + fallback defaults

**Files:**
- Modify: `frontend/src/shared/api/types.ts` (Settings interface — add two fields near `ytTags`)
- Modify: `frontend/src/shared/model/settings.ts` (`fallback` object ~line 20-53)

**Interfaces:**
- Consumes: `Settings.YtKeywordRoster`/`YtRosterAutoGrow` json shape from Task 1.
- Produces: TS `Settings.ytKeywordRoster: string`, `Settings.ytRosterAutoGrow: boolean`.

- [ ] **Step 1: Add fields to the Settings type**

In `frontend/src/shared/api/types.ts`, inside the `Settings` interface, next to `ytTags`/`ytPrivacy`, add:

```ts
  ytKeywordRoster: string;
  ytRosterAutoGrow: boolean;
```

- [ ] **Step 2: Add fallback defaults**

In `frontend/src/shared/model/settings.ts`, in the `fallback` object after `ytTags: ...` (~line 51), add:

```ts
  ytKeywordRoster: "",
  ytRosterAutoGrow: true,
```

- [ ] **Step 3: Verify types compile**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/shared/api/types.ts frontend/src/shared/model/settings.ts
git commit -m "YouTube: поля ytKeywordRoster/ytRosterAutoGrow на фронте"
```

---

### Task 3: `ytKeywords.ts` — pure keyword/hashtag/roster logic (TDD)

**Files:**
- Create: `frontend/src/shared/lib/ytKeywords.ts`
- Test: `frontend/src/shared/lib/ytKeywords.test.ts` (create; run with Node, excluded from bundle by not being imported)

**Interfaces:**
- Produces:
  - `parseRoster(raw: string): string[]`
  - `buildKeywords(artists: string[], roster: string, year: number, budget?: number): string`
  - `buildHashtags(artists: string[], max?: number): string`
  - `mergeRoster(raw: string, artists: string[]): string`

- [ ] **Step 1: Write the failing test**

Create `frontend/src/shared/lib/ytKeywords.test.ts`:

```ts
import { buildHashtags, buildKeywords, mergeRoster, parseRoster } from "./ytKeywords.ts";

function eq(got: unknown, want: unknown, msg: string): void {
  const G = JSON.stringify(got), W = JSON.stringify(want);
  if (G !== W) throw new Error(`FAIL ${msg}\n  got:  ${G}\n  want: ${W}`);
  console.log(`ok - ${msg}`);
}

// parseRoster: split on comma/newline, trim, drop empty, case-insensitive dedup, keep order
eq(parseRoster("jeezy, Chief Keef\n jeezy ,,\nbuy rap beats"),
   ["jeezy", "Chief Keef", "buy rap beats"], "parseRoster splits & dedups");

// buildKeywords: front slice (per artist) → roster → evergreen tail, lowercased, deduped
const kw = buildKeywords(["Bankroll Fresh", "MexikoDro"], "jeezy, chief keef", 2026);
const parts = kw.split(", ");
eq(parts[0], "bankroll fresh type beat", "keywords front slice first");
eq(parts[1], "free bankroll fresh type beat", "keywords free variant");
eq(parts[2], "bankroll fresh type beat 2026", "keywords year variant");
eq(parts[3], "bankroll fresh", "keywords bare artist");
eq(parts.includes("jeezy"), true, "keywords include roster");
eq(parts.includes("chief keef"), true, "keywords include roster 2");
eq(parts.includes("type beat"), true, "keywords include evergreen tail");
eq(new Set(parts).size, parts.length, "keywords deduped");

// buildKeywords budget: never exceed the char budget
const big = Array.from({ length: 500 }, (_, i) => `artist${i}`).join(", ");
const capped = buildKeywords([], big, 2026, 300);
eq(capped.length <= 300, true, `keywords respect budget (len=${capped.length})`);

// buildKeywords: empty roster still works from artists + evergreen
const noRoster = buildKeywords(["Gunna"], "", 2026).split(", ");
eq(noRoster[0], "gunna type beat", "keywords work with empty roster");
eq(noRoster.includes("instrumental"), true, "keywords evergreen with empty roster");

// buildHashtags: CamelCase #ArtistTypeBeat, alnum only, dedup, cap 15
eq(buildHashtags(["bankroll fresh", "MexikoDro", "warhol.ss"]),
   "#BankrollFreshTypeBeat #MexikodroTypeBeat #WarholSsTypeBeat", "hashtags camelcase");
eq(buildHashtags(["jeezy", "Jeezy"]), "#JeezyTypeBeat", "hashtags dedup ci");
eq(buildHashtags(Array.from({ length: 30 }, (_, i) => `a${i}`)).split(" ").length, 15,
   "hashtags cap 15");

// mergeRoster: append bare + "{artist} type beat" for new artists only
eq(mergeRoster("jeezy", ["Bankroll Fresh", "jeezy"]),
   "jeezy, Bankroll Fresh, Bankroll Fresh type beat", "mergeRoster adds new both forms, skips existing");

console.log("all passed");
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && node --experimental-strip-types src/shared/lib/ytKeywords.test.ts`
Expected: FAIL — cannot find module `./ytKeywords.ts`.

- [ ] **Step 3: Write the implementation**

Create `frontend/src/shared/lib/ytKeywords.ts`:

```ts
// Генерация SEO-ключевиков и хэштегов для описания YouTube. Чистые функции без
// зависимостей и сети: превью в диалоге и то, что уходит в загрузку, считаются
// одним и тем же кодом. Спека: docs/superpowers/specs/2026-07-18-youtube-seo-keywords-design.md

// Вечнозелёный хвост — общие высокочастотные запросы, которыми добиваем стену.
const EVERGREEN = [
  "type beat", "free type beat", "instrumental", "buy rap beats",
  "rap beats with hooks for sale", "trap instrumental", "rap instrumental", "beats",
];

/** Разбирает ростер (запятые/переносы) в список: trim, отброс пустых, дедуп без
 *  учёта регистра, порядок сохраняется. */
export function parseRoster(raw: string): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const part of raw.split(/[,\n]/)) {
    const v = part.trim();
    if (!v) continue;
    const k = v.toLowerCase();
    if (seen.has(k)) continue;
    seen.add(k);
    out.push(v);
  }
  return out;
}

/** Ключевые фразы бита: «{artist} type beat», «free …», «… {год}», голое имя. */
function beatPhrases(artists: string[], year: number): string[] {
  const out: string[] = [];
  for (const a of artists) {
    const s = a.trim();
    if (!s) continue;
    out.push(`${s} type beat`, `free ${s} type beat`, `${s} type beat ${year}`, s);
  }
  return out;
}

/** Стена ключевиков: фронт-слайс бита → ростер → вечнозелёный хвост. Всё в
 *  нижнем регистре (как в эталоне), дедуп без учёта регистра, обрезка по бюджету
 *  длины готовой строки (через ", "). */
export function buildKeywords(artists: string[], roster: string, year: number, budget = 4000): string {
  const seen = new Set<string>();
  const out: string[] = [];
  let len = 0;
  const add = (raw: string): void => {
    const v = raw.trim().toLowerCase().replace(/\s+/g, " ");
    if (!v || seen.has(v)) return;
    const extra = (out.length ? 2 : 0) + v.length; // ", " + фраза
    if (len + extra > budget) return;
    seen.add(v);
    out.push(v);
    len += extra;
  };
  for (const p of beatPhrases(artists, year)) add(p);
  for (const r of parseRoster(roster)) add(r);
  for (const e of EVERGREEN) add(e);
  return out.join(", ");
}

/** Хэштеги бита: тип-артисты → «#ArtistTypeBeat» (CamelCase, только буквы/цифры).
 *  Дедуп без учёта регистра, лимит max (YouTube прячет спам-количество). */
export function buildHashtags(artists: string[], max = 15): string {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const a of artists) {
    const words = a.split(/[^a-z0-9]+/i).filter(Boolean);
    if (!words.length) continue;
    const camel = words.map((w) => w.charAt(0).toUpperCase() + w.slice(1).toLowerCase()).join("");
    const tag = `#${camel}TypeBeat`;
    const k = tag.toLowerCase();
    if (seen.has(k)) continue;
    seen.add(k);
    out.push(tag);
    if (out.length >= max) break;
  }
  return out.join(" ");
}

/** Авторост ростера: для новых артистов добавляет обе формы — голое имя и
 *  «{artist} type beat». Возвращает нормализованную строку через ", " (дедуп ci). */
export function mergeRoster(raw: string, artists: string[]): string {
  const list = parseRoster(raw);
  const seen = new Set(list.map((v) => v.toLowerCase()));
  const add = (v: string): void => {
    const k = v.toLowerCase();
    if (!k || seen.has(k)) return;
    seen.add(k);
    list.push(v);
  };
  for (const a of artists) {
    const s = a.trim();
    if (!s) continue;
    add(s);
    add(`${s} type beat`);
  }
  return list.join(", ");
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && node --experimental-strip-types src/shared/lib/ytKeywords.test.ts`
Expected: prints `ok - ...` lines then `all passed`.

- [ ] **Step 5: Verify the module type-checks in the project**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors. (`.test.ts` uses only `console`, available via the DOM lib — no `@types/node` needed.)

- [ ] **Step 6: Commit**

```bash
git add frontend/src/shared/lib/ytKeywords.ts frontend/src/shared/lib/ytKeywords.test.ts
git commit -m "YouTube: модуль генерации ключевиков/хэштегов + тесты"
```

---

### Task 4: Wire `{keywords}`/`{hashtags}` into the template engine

**Files:**
- Modify: `frontend/src/pages/player/PlayerPage.tsx` — imports (top), `renderYtVars` (~line 1176-1187), `resolveBeat` (~line 1720-1729)

**Interfaces:**
- Consumes: `buildKeywords`, `buildHashtags` from Task 3.
- Produces: `renderYtVars(tpl, b, nick, authors, roster?)` now expands `{keywords}`/`{hashtags}`; `resolveBeat` passes the roster.

- [ ] **Step 1: Import the module**

Near the other `@/shared/lib` imports at the top of `PlayerPage.tsx`, add:

```ts
import { buildKeywords, buildHashtags, mergeRoster } from "@/shared/lib/ytKeywords";
```

- [ ] **Step 2: Extend `renderYtVars`**

Replace the `renderYtVars` function (~line 1176-1187) with:

```ts
function renderYtVars(tpl: string, b: YtBeat, nick: string, authors: string[], roster = ""): string {
  const stem = b.name.replace(/\.[^.]+$/, "");
  const parsed = beatTitleFromStem(stem, b.bpm);
  const bpm = b.bpm ? Math.round(b.bpm) : parsed.bpm;
  const nlc = nick.trim().toLowerCase();
  const typeArtists = authors.filter((a) => a.trim().toLowerCase() !== nlc);
  const year = new Date().getFullYear();
  return tpl
    .split("{name}").join(stripNick(parsed.title, nick))
    .split("{type}").join(b.typeName)
    .split("{bpm}").join(bpm ? String(bpm) : "")
    .split("{key}").join(b.key ?? "")
    .split("{nick}").join(nick.trim())
    .split("{authors}").join(joinAuthors(authors))
    .split("{keywords}").join(buildKeywords(typeArtists, roster, year))
    .split("{hashtags}").join(buildHashtags(typeArtists));
}
```

- [ ] **Step 3: Add roster state and thread it into `resolveBeat`**

Just after the `const [tags, setTags] = ...` state declaration (~line 1466), add:

```ts
  const [roster, setRoster] = React.useState(settings?.ytKeywordRoster ?? "");
  const [rosterAutoGrow, setRosterAutoGrow] = React.useState(settings?.ytRosterAutoGrow ?? true);
```

Then in `resolveBeat` (~line 1720-1729), change the description line and deps:

```ts
  const resolveBeat = React.useCallback((b: YtBeat) => {
    const e = edits[b.path] ?? {};
    const authors = beatAuthors(b, nick, aliases, extras[b.path] ?? []);
    return {
      title: e.title ?? renderYtTemplate(tpl, b, nick, authors),
      description: e.desc ?? renderYtVars(desc, b, nick, authors, roster),
      tags: e.tags ?? tags,
      privacy: e.privacy ?? privacy,
    };
  }, [edits, tpl, desc, tags, privacy, nick, aliases, extras, roster]);
```

- [ ] **Step 4: Verify it compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors. (`setRoster`/`setRosterAutoGrow`/`mergeRoster`/`rosterAutoGrow` are used in Tasks 5-6; `noUnusedLocals` is false so this compiles now.)

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/player/PlayerPage.tsx
git commit -m "YouTube: {keywords}/{hashtags} в renderYtVars + ростер в resolveBeat"
```

---

### Task 5: Roster UI (Shared-by-all tab) + persistence

**Files:**
- Modify: `frontend/src/pages/player/PlayerPage.tsx` — the save call (~line 1989) and the shared-tab JSX after the tags block (~line 2545+)
- Modify: `frontend/src/shared/i18n/ru.ts`, `frontend/src/shared/i18n/en.ts` (player section)

**Interfaces:**
- Consumes: `roster`/`setRoster`/`rosterAutoGrow`/`setRosterAutoGrow` (Task 4), `buildKeywords`/`buildHashtags` (Task 3), `labelStyle`/`inputStyle` (already in scope in the dialog).
- Produces: roster editing + live preview UI; roster persisted via `updateSettings`.

- [ ] **Step 1: Add i18n labels**

In both `frontend/src/shared/i18n/ru.ts` and `en.ts`, inside the `player` object (near the other `yt...` keys), add — RU:

```ts
    ytKeywordsSection: "Ключевые слова",
    ytRosterLabel: "Ростер (артисты и фразы для {keywords})",
    ytRosterHint: "Вставь свою стену тегов через запятую. Артисты бита добавятся впереди автоматически.",
    ytRosterAutoGrow: "Пополнять артистами опубликованных битов",
    ytKeywordsPreview: "Превью {keywords}",
    ytHashtagsPreview: "Превью {hashtags}",
```

EN:

```ts
    ytKeywordsSection: "Keywords",
    ytRosterLabel: "Roster (artists & phrases for {keywords})",
    ytRosterHint: "Paste your tag wall, comma-separated. The beat's artists are prepended automatically.",
    ytRosterAutoGrow: "Grow from published beats",
    ytKeywordsPreview: "{keywords} preview",
    ytHashtagsPreview: "{hashtags} preview",
```

- [ ] **Step 2: Persist roster on publish**

In the `updateSettings({...})` call inside `start()` (~line 1989), add these keys to the object:

```ts
      ytKeywordRoster: roster, ytRosterAutoGrow: rosterAutoGrow,
```

- [ ] **Step 3: Add the roster block to the Shared tab**

Locate the tags block in the shared/`{(!isBatch || tab === "shared") && (...)}` section (search for `t.player.ytTags` used as a `labelStyle` span, ~line 2545). Immediately after that tags block's closing element, insert:

```tsx
            <div style={{ display: "flex", flexDirection: "column", gap: 6, marginTop: 12 }}>
              <span style={labelStyle}>{t.player.ytKeywordsSection}</span>
              <label style={{ fontSize: 12, color: "var(--text-faint)" }}>{t.player.ytRosterLabel}</label>
              <textarea
                value={roster}
                onChange={(e) => setRoster(e.target.value)}
                placeholder={t.player.ytRosterHint}
                spellCheck={false}
                style={{ ...inputStyle, minHeight: 90, resize: "vertical", fontFamily: "var(--font-mono)", fontSize: 12 }}
              />
              <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12, cursor: "pointer" }}>
                <input type="checkbox" checked={rosterAutoGrow} onChange={(e) => setRosterAutoGrow(e.target.checked)} />
                {t.player.ytRosterAutoGrow}
              </label>
            </div>
```

- [ ] **Step 4: Verify it compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/player/PlayerPage.tsx frontend/src/shared/i18n/ru.ts frontend/src/shared/i18n/en.ts
git commit -m "YouTube: UI ростера ключевиков во вкладке «Общее» + сохранение"
```

---

### Task 6: Auto-grow the roster on publish

**Files:**
- Modify: `frontend/src/pages/player/PlayerPage.tsx` — end of the `start()` upload loop (~after line 2021)

**Interfaces:**
- Consumes: `mergeRoster` (Task 3), `rosterAutoGrow`/`roster`/`setRoster` (Task 4), `beatAuthors` (existing), `updateSettings` (existing).

- [ ] **Step 1: Append auto-grow after the upload loop**

In `start()`, after the `for (const b of beats) { ... }` loop closes (the line with `setJobMap({ ...map });` is the last statement inside the loop; the loop's closing `}` is ~line 2021) and BEFORE the function's closing `}` (~line 2022), insert:

```ts
    // Авторост ростера: артисты успешно поставленных в очередь битов пополняют
    // пул для будущих видео. Артисты текущей пачки и так во фронт-слайсе.
    if (rosterAutoGrow) {
      const nlc = nick.trim().toLowerCase();
      let grown = roster;
      for (const b of beats) {
        if (!map[b.path]) continue; // не ушёл в загрузку
        const ta = beatAuthors(b, nick, aliases, extras[b.path] ?? [])
          .filter((a) => a.trim().toLowerCase() !== nlc);
        grown = mergeRoster(grown, ta);
      }
      if (grown !== roster) {
        setRoster(grown);
        void updateSettings({ ytKeywordRoster: grown });
      }
    }
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/player/PlayerPage.tsx
git commit -m "YouTube: авторост ростера артистами опубликованных битов"
```

---

### Task 7: Live keyword/hashtag preview in the dialog

**Files:**
- Modify: `frontend/src/pages/player/PlayerPage.tsx` — add a memo near the other author memos (~line 1639), render preview inside the roster block from Task 5

**Interfaces:**
- Consumes: `buildKeywords`/`buildHashtags` (Task 3), `focusBeat`/`nick`/`aliases`/`extras`/`roster` (existing).

- [ ] **Step 1: Compute the preview**

Near `repAuthors`/`chips` memos (~line 1639), add:

```ts
  // Живое превью развёрнутых {keywords}/{hashtags} для сфокусированного бита —
  // чтобы видеть стену тегов до публикации.
  const kwPreview = React.useMemo(() => {
    if (!focusBeat) return { keywords: "", hashtags: "" };
    const nlc = nick.trim().toLowerCase();
    const ta = beatAuthors(focusBeat, nick, aliases, extras[focusBeat.path] ?? [])
      .filter((a) => a.trim().toLowerCase() !== nlc);
    return { keywords: buildKeywords(ta, roster, new Date().getFullYear()), hashtags: buildHashtags(ta) };
  }, [focusBeat, nick, aliases, extras, roster]);
```

- [ ] **Step 2: Render the preview inside the roster block**

In the roster `<div>` from Task 5, just before its closing `</div>`, insert:

```tsx
              <label style={{ fontSize: 12, color: "var(--text-faint)", marginTop: 4 }}>{t.player.ytKeywordsPreview}</label>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--text-faint)", maxHeight: 90, overflowY: "auto", whiteSpace: "pre-wrap", wordBreak: "break-word", border: "1px solid var(--stroke, #333)", borderRadius: 4, padding: 6 }}>
                {kwPreview.keywords || "—"}
              </div>
              <label style={{ fontSize: 12, color: "var(--text-faint)", marginTop: 4 }}>{t.player.ytHashtagsPreview}</label>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--accent, #4ea1ff)", wordBreak: "break-word" }}>
                {kwPreview.hashtags || "—"}
              </div>
```

- [ ] **Step 3: Verify it compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Rebuild the sidecar and run the app for manual verification**

Run: `powershell -ExecutionPolicy Bypass -File scripts\build-sidecar.ps1`
Then confirm the dev app relaunches (tauri watcher). In the YouTube dialog, open a batch, go to "Общее для всех" → Keywords: paste a small roster, watch the `{keywords}` preview update, switch focus beats and confirm the front slice changes per beat, confirm hashtags are `#ArtistTypeBeat`. Check the description field's `{keywords}`/`{hashtags}` expand in the "This video" resolved output.
Expected: preview matches the resolved description; per-beat artists differ. DO NOT click the final upload button.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/player/PlayerPage.tsx
git commit -m "YouTube: живое превью {keywords}/{hashtags} в диалоге"
```

---

## Self-Review

**Spec coverage:**
- Roster data (`YtKeywordRoster`, `YtRosterAutoGrow`) → Task 1, 2. ✓
- Paste seeding → Task 5 (textarea). ✓
- Auto-grow (bare + "type beat" both forms) → Task 3 `mergeRoster` + Task 6. ✓
- `{keywords}` hybrid order (front slice → roster → evergreen), lowercase, dedup, budget → Task 3 + Task 4. ✓
- `{hashtags}` CamelCase, cap 15, exclude nick → Task 3 `buildHashtags` + Task 4 `typeArtists`. ✓
- New default description → Task 1. ✓
- UI in Shared tab + live preview → Task 5, 7. ✓
- Frontend-only expansion, preview==upload → `renderYtVars` used by both `resolveBeat` (upload) and `kwPreview`/preview (Task 7). ✓
- Offline, tags-field ✨ untouched → no change to `GenerateTags`/`api.ytTags`. ✓

**Placeholder scan:** No TBD/TODO; every code step shows full code. ✓

**Type consistency:** `buildKeywords(artists, roster, year, budget?)`, `buildHashtags(artists, max?)`, `mergeRoster(raw, artists)`, `parseRoster(raw)` — signatures identical across Tasks 3, 4, 6, 7. `renderYtVars(..., roster = "")` optional param keeps `renderYtTemplate`'s existing 4-arg call valid. ✓

**Note:** `settings?.ytKeywordRoster` / `ytRosterAutoGrow` are typed after Task 2, so Task 4's state init is type-safe. Task 4 Step 4 explicitly notes the temporarily-unused setters compile because `noUnusedLocals` is false.
