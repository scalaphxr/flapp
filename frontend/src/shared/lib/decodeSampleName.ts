// Декодирование Unicode-hex токенов в именах звуков (только для отображения).
// В именах файлов декоративные символы (✱ ✦ ❤ ★ …) иногда закодированы текстом
// вида `#$2731`: необязательная `#`, затем `$` и hex-код-поинт Unicode.
// `#$2731` → 0x2731 → «✱». Файлы на диске и данные не меняем — только показ.

const TOKEN = /#?\$([0-9A-Fa-f]{4,6})/g;

export function decodeSampleName(name: string): string {
  if (!name || name.indexOf("$") === -1) return name;
  return name.replace(TOKEN, (match, hex: string) => {
    const cp = parseInt(hex, 16);
    if (!Number.isFinite(cp) || cp > 0x10ffff) return match;
    try {
      return String.fromCodePoint(cp);
    } catch {
      // невалидный код-поинт (напр. суррогат) — оставляем исходный токен
      return match;
    }
  });
}
