// Парсер авторов бита из имени файла. Продюсеры уже в имени, но форматы у всех
// разные — поэтому эвристика по сигналам, а не жёсткий шаблон. Промахи
// закрываются редактируемыми чипсами + картой псевдонимов в UI.
//
// Модель: находим BPM и режем имя на «до» и «после». Авторы — это:
//   • @-хендлы где угодно (снимаем @, цифры в нике сохраняем);
//   • группы, склеенные коннектором `+ & x`, в части ДО BPM (KOLALOI+WSB+TAINIY);
//   • регион ПОСЛЕ BPM: если есть коннектор — режем по нему (сегмент может быть
//     из нескольких слов = один человек, «Michael Makho»); если коннектора нет —
//     по пробелам (каждое слово — автор). Плейн-слова ДО BPM — это название,
//     в авторы не идут. Если BPM в самом начале, после него стоит имя — тогда
//     из региона берём только коннектор-сегменты, а голые слова игнорируем.

export interface ParseAuthorsOpts {
  /** Свой ник из настроек: всегда в списке и первым. */
  nick?: string;
  /** Карта правок: токен(lowercase) → каноничное имя; пустая строка = «не автор». */
  aliases?: Record<string, string>;
  /** BPM из анализа — приоритетный якорь для поиска темпа. */
  bpm?: number;
}

const NOISE = new Set([
  "bpm", "prod", "p", "type", "beat", "beats", "free", "exclusive", "feat", "ft",
  "mix", "master", "mastered", "remix", "wav", "mp3", "instrumental", "inst",
  "loop", "x", "min", "max", "maj", "minor", "major", "key",
]);

const lc = (s: string) => s.toLowerCase();

/** Похоже на тональность: A, F#, Bmin, Amaj, G#Min… (не ник). */
function isKey(t: string): boolean {
  return /^[a-g](#|b)?(m|min|maj|minor|major|maj7|m7)?$/i.test(t) && t.length <= 6;
}

/** Токен-мусор: слишком короткий, шум-слово, тональность, число или частота. */
function isNoise(t: string): boolean {
  return t.length < 2 || NOISE.has(t) || isKey(t) || /^\d+$/.test(t) || /^\d{2,4}hz$/.test(t);
}

// Коннектор соавторов: `+`, `&` или отдельно стоящий ` x `.
const CONNECTOR = /\s*[+&]\s*|\s+x\s+/gi;
const hasConnector = (s: string) => /\s*[+&]\s*|\s+x\s+/i.test(s);

/** Авторы из групп, склеенных коннектором, в куске ДО BPM (KOLALOI+WSB+TAINIY).
 * Здесь сегменты обычно однословные, поэтому режем по коннектору на слова. */
function connectorAuthors(part: string): string[] {
  const tok = "[\\p{L}\\p{N}_]+";
  const conn = "(?:\\s*[+&]\\s*|\\s+x\\s+)";
  const group = new RegExp(`${tok}${conn}${tok}(?:${conn}${tok})*`, "giu");
  const out: string[] = [];
  for (const m of part.matchAll(group)) {
    for (const seg of m[0].split(CONNECTOR)) {
      const t = lc(seg.trim());
      if (t) out.push(t);
    }
  }
  return out;
}

/** Делит имя на части «до BPM» и «после BPM». Приоритет якоря: `NNNbpm` →
 * `(NNN)` → значение из анализа → первое отдельное 2–3-значное число 40–260
 * (не 4-значный индекс, не частота `NNNhz`). */
function splitAtBpm(s: string, bpm?: number): { before: string; after: string } {
  let m: RegExpMatchArray | null = s.match(/(\d{2,3})\s*bpm\b/i);
  if (!m) m = s.match(/\((\d{2,3})\)/);
  if (!m && bpm && bpm > 0) m = s.match(new RegExp(`(?<!\\d)${Math.round(bpm)}(?!\\d)`));
  if (!m) {
    for (const cand of s.matchAll(/(?<!\d)(\d{2,3})(?!\d)/g)) {
      const v = +cand[1];
      if (v >= 40 && v <= 260 && !/^\s*hz\b/i.test(s.slice((cand.index ?? 0) + cand[1].length))) {
        m = cand as RegExpMatchArray;
        break;
      }
    }
  }
  if (!m || m.index == null) return { before: s, after: "" };
  return { before: s.slice(0, m.index), after: s.slice(m.index + m[0].length) };
}

/** Авторы из региона ПОСЛЕ BPM. plainWords=false (BPM в самом начале → дальше
 * имя) отключает разбор голых слов, оставляя только коннектор-сегменты. */
function regionAuthors(after: string, plainWords: boolean): string[] {
  let s = ` ${after} `;
  s = s.replace(/\[[^\]]*\]/g, " ");          // [loop], [BR Glo]
  s = s.replace(/\b(?:bpm|loop)\b/gi, " ");    // метки
  s = s.replace(/\b\d{2,4}hz\b/gi, " ");       // 494hz
  // ведущая тональность (F#min, Emin, fmin, Bm, G#Min…)
  s = s.replace(/^\s*[a-g](?:#|b)?\s?(?:m|min|maj|minor|major)?(?=\s|$)/i, " ");
  s = s.trim();
  if (!s) return [];
  if (hasConnector(s)) {
    return s.split(CONNECTOR).map((seg) => lc(seg.trim())).filter((seg) => seg && !isNoise(seg));
  }
  if (!plainWords) return [];
  return s.split(/[^\p{L}\p{N}#]+/u).map(lc).filter((w) => w && !isNoise(w));
}

/** Распознаёт всех авторов бита. Возвращает упорядоченный список без повторов;
 * свой ник (если задан) — первым. */
export function parseAuthors(stem: string, opts: ParseAuthorsOpts = {}): string[] {
  const { nick = "", aliases = {}, bpm } = opts;

  const handles = [...stem.matchAll(/@([\p{L}\p{N}_]+)/gu)].map((m) => lc(m[1]));
  const noHandles = stem.replace(/@[\p{L}\p{N}_]+/gu, " ");

  const { before, after } = splitAtBpm(noHandles, bpm);
  const beforeConn = connectorAuthors(before);
  const trailing = regionAuthors(after, before.trim() !== "");

  const ordered: string[] = [];
  const seen = new Set<string>();
  const consider = (raw: string) => {
    const t = lc(raw);
    if (isNoise(t)) return;
    let name = Object.prototype.hasOwnProperty.call(aliases, t) ? aliases[t] : raw;
    name = name.trim();
    if (!name) return; // алиас на "" = «не автор»
    const key = lc(name);
    if (seen.has(key)) return;
    seen.add(key);
    ordered.push(name);
  };
  for (const a of [...beforeConn, ...trailing, ...handles]) consider(a);

  // Fallback: авторы стоят ПЕРЕД именем и отделены от него тире
  // (whapperx owski - dirty pipe 128). Срабатывает только если надёжные сигналы
  // ничего не дали — иначе риск принять кусок названия за автора.
  if (ordered.length === 0) {
    const segs = before.split(/\s[-–—]\s/).map((x) => x.trim()).filter(Boolean);
    if (segs.length >= 2) {
      // Последний сегмент примыкает к BPM — это имя; более ранние — авторы.
      for (const seg of segs.slice(0, -1)) {
        for (const w of seg.split(/[^\p{L}\p{N}#]+/u)) consider(w);
      }
    }
  }

  const n = nick.trim();
  if (!n) return ordered;
  const rest = ordered.filter((a) => lc(a) !== lc(n));
  return [n, ...rest];
}

/** Строка авторов для {authors} и надписи «prod.»: конвенция type-beat — ` x `. */
export function joinAuthors(authors: string[]): string {
  return authors.join(" x ");
}
