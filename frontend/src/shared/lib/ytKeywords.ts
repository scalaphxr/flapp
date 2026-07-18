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
    if (!s || seen.has(s.toLowerCase())) continue; // артист уже в ростере — не трогаем
    add(s);
    add(`${s} type beat`);
  }
  return list.join(", ");
}
