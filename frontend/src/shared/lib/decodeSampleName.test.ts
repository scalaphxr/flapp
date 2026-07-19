import { decodeSampleName } from "./decodeSampleName.ts";

function eq(got: unknown, want: unknown, msg: string): void {
  const G = JSON.stringify(got), W = JSON.stringify(want);
  if (G !== W) throw new Error(`FAIL ${msg}\n  got:  ${G}\n  want: ${W}`);
  console.log(`ok - ${msg}`);
}

// базовый кейс из задачи: #$2731 → ✱ (heavy asterisk, U+2731)
eq(decodeSampleName("#$2731 bass fuckemup.wav"), "✱ bass fuckemup.wav", "decode #$2731");

// без решётки — просто $2731
eq(decodeSampleName("$2731 loop.wav"), "✱ loop.wav", "decode $2731 без #");

// несколько токенов подряд: ✱ + ❤ (U+2764)
eq(decodeSampleName("#$2731#$2764 x"), "✱❤ x", "decode нескольких токенов");

// цена — 2 цифры, не трогаем
eq(decodeSampleName("$50 loop.wav"), "$50 loop.wav", "цена $50 без изменений");

// имя без токенов
eq(decodeSampleName("plain kick.wav"), "plain kick.wav", "имя без токенов");

// пустая строка
eq(decodeSampleName(""), "", "пустая строка");

// эмодзи (5 hex, U+1F525 🔥)
eq(decodeSampleName("#$1F525 fire.wav"), "\u{1F525} fire.wav", "decode эмодзи 1F525");

// код-поинт вне диапазона Unicode — оставляем токен как есть
eq(decodeSampleName("$FFFFFF x"), "$FFFFFF x", "невалидный код-поинт оставлен");

console.log("all decodeSampleName tests passed");
