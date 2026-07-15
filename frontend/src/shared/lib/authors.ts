// Парсер авторов бита из имени файла. Продюсеры уже указаны в имени, но у всех
// разные форматы — поэтому это эвристика по нескольким сигналам, а не жёсткий
// шаблон. Промахи закрываются редактируемыми чипсами + картой псевдонимов в UI.
//
// Сигналы (по надёжности): @-хендлы → группы, склеенные `+ & x` → токены-хвост
// после BPM. Имя бита (до BPM) в авторы не идёт; ведущий 4-значный индекс,
// шум-слова (bpm/prod/type…), тональности и числа отбрасываются.

export interface ParseAuthorsOpts {
  /** Свой ник из настроек: всегда попадает в список и ставится первым. */
  nick?: string;
  /** Карта правок: токен(lowercase) → каноничное имя; пустая строка = «не автор». */
  aliases?: Record<string, string>;
  /** BPM из анализа — приоритетный якорь для поиска числа темпа. */
  bpm?: number;
}

const NOISE = new Set([
  "bpm", "prod", "p", "type", "beat", "beats", "free", "exclusive", "feat", "ft",
  "mix", "master", "mastered", "remix", "wav", "mp3", "instrumental", "inst",
  "x", "min", "max", "maj", "minor", "major", "key",
]);

const lc = (s: string) => s.toLowerCase();

/** Похоже на обозначение тональности: A, F#, Bmin, Amaj, Gm… (не ник). */
function isKey(t: string): boolean {
  return /^[a-g](#|b)?(m|min|maj|minor|major|maj7|m7)?$/i.test(t) && t.length <= 6;
}

function isNoise(t: string): boolean {
  return t.length < 2 || NOISE.has(t) || isKey(t) || /^\d+$/.test(t);
}

/** Токены имени без разделителей: любой не-буквенно-цифровой символ = граница,
 * плюс расклейка границ цифра↔буква (success135nsm → success 135 nsm). */
function tokenize(stem: string): string[] {
  const s = stem
    .replace(/([a-zA-Z])([0-9])/g, "$1 $2")
    .replace(/([0-9])([a-zA-Z])/g, "$1 $2")
    .replace(/[^\p{L}\p{N}]+/gu, " ");
  return s.split(/\s+/).filter(Boolean);
}

// Коннектор соавторов: `+`, `&` или отдельно стоящий ` x `.
const CONNECTOR = /\s*[+&]\s*|\s+x\s+/gi;

/** Авторы из групп, склеенных явными коннекторами (KOLALOI+WSB+TAINIY,
 * «@a + @b») — сильный сигнал независимо от позиции в имени. */
function connectorAuthors(stem: string): string[] {
  const tok = "[@\\p{L}\\p{N}_]+";
  const conn = "(?:\\s*[+&]\\s*|\\s+x\\s+)";
  const group = new RegExp(`${tok}${conn}${tok}(?:${conn}${tok})*`, "giu");
  const out: string[] = [];
  for (const m of stem.matchAll(group)) {
    for (const part of m[0].split(CONNECTOR)) {
      const t = lc(part.replace(/^@/, "").trim());
      if (t) out.push(t);
    }
  }
  return out;
}

/** Индекс токена-BPM: сперва совпадение с проанализированным темпом, иначе
 * первое отдельное 2–3-значное число 40–260 (ведущий 4-значный индекс — мимо). */
function findBpmIndex(tokens: string[], bpm?: number): number {
  if (bpm && bpm > 0) {
    const r = String(Math.round(bpm));
    const i = tokens.indexOf(r);
    if (i >= 0) return i;
  }
  return tokens.findIndex((t) => /^\d{2,3}$/.test(t) && +t >= 40 && +t <= 260);
}

/** Распознаёт всех авторов бита. Возвращает упорядоченный список без повторов;
 * свой ник (если задан) — первым. */
export function parseAuthors(stem: string, opts: ParseAuthorsOpts = {}): string[] {
  const { nick = "", aliases = {}, bpm } = opts;

  const handles = [...stem.matchAll(/@([\p{L}\p{N}_]+)/gu)].map((m) => lc(m[1]));
  const connectors = connectorAuthors(stem);
  // Хендлы убираем до токенизации: иначе расклейка цифра↔буква рвёт «@wh1sper»
  // на «wh 1 sper» и обломки утекают в авторов. Сами хендлы уже сохранены.
  const stemNoHandles = stem.replace(/@[\p{L}\p{N}_]+/gu, " ");
  const tokens = tokenize(stemNoHandles);
  const bpmIdx = findBpmIndex(tokens, bpm);
  // Токены после BPM = авторы, только если перед BPM есть имя бита. Если BPM в
  // самом начале (напр. «(167) Space Cadet @nick»), имя стоит ПОСЛЕ — тогда
  // опираемся лишь на хендлы/коннекторы, а хвост в авторы не тащим.
  const positional = bpmIdx > 0 ? tokens.slice(bpmIdx + 1).map(lc) : [];

  const authorSet = new Set<string>([...handles, ...connectors, ...positional]);

  const ordered: string[] = [];
  const seen = new Set<string>();
  const consider = (raw: string) => {
    const t = lc(raw);
    if (!authorSet.has(t) || isNoise(t)) return;
    // Правка пользователя (переименование/удаление) имеет приоритет.
    let name = Object.prototype.hasOwnProperty.call(aliases, t) ? aliases[t] : t;
    name = name.trim();
    if (!name) return; // алиас на "" = «не автор»
    const key = lc(name);
    if (seen.has(key)) return;
    seen.add(key);
    ordered.push(name);
  };
  // Порядок появления в имени + подстраховка для хендлов/коннекторов.
  for (const tok of tokens) consider(tok);
  for (const h of [...connectors, ...handles]) consider(h);

  const n = nick.trim();
  if (!n) return ordered;
  const rest = ordered.filter((a) => lc(a) !== lc(n));
  return [n, ...rest];
}

/** Строка авторов для {authors} и надписи «prod.»: конвенция type-beat — ` x `. */
export function joinAuthors(authors: string[]): string {
  return authors.join(" x ");
}
