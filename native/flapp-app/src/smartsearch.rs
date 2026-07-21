// NL-поиск: «тёмные агрессивные 808 на 140 bpm» → структурный запрос
// (категории + теги + диапазон BPM), нераспознанное → свободный текст. Порт
// usecase/smartsearch.go (RU+EN словари настроений/инструментов/темпа).

use std::collections::HashMap;
use std::sync::OnceLock;

use regex::Regex;

use crate::classify::Category;
use Category::*;

/// Разобранный запрос: что удалось понять + свободный текст-остаток.
#[derive(Debug, Default, Clone, PartialEq)]
pub struct Parsed {
    pub categories: Vec<Category>,
    pub tags: Vec<String>,
    pub min_bpm: i32,
    pub max_bpm: i32,
    pub free_text: String,
}

fn instrument_terms() -> &'static HashMap<&'static str, Category> {
    static M: OnceLock<HashMap<&'static str, Category>> = OnceLock::new();
    M.get_or_init(|| {
        [
            ("808", C808), ("сабы", C808), ("саб", C808),
            ("kick", Kick), ("кик", Kick), ("бочка", Kick), ("бочки", Kick),
            ("snare", Snare), ("снейр", Snare), ("снэйр", Snare), ("малый", Snare),
            ("clap", Clap), ("клэп", Clap), ("клап", Clap), ("хлопок", Clap),
            ("hat", HiHat), ("hihat", HiHat), ("хэт", HiHat), ("хайхэт", HiHat), ("хай-хэт", HiHat),
            ("openhat", OpenHat), ("оупенхэт", OpenHat),
            ("crash", OpenHat), ("креш", OpenHat),
            ("ride", OpenHat), ("райд", OpenHat),
            ("cymbal", OpenHat), ("тарелка", OpenHat), ("тарелки", OpenHat),
            ("perc", Perc), ("перк", Perc), ("перкуссия", Perc), ("перкуссии", Perc),
            ("rim", Perc), ("римшот", Perc),
            ("tom", Perc), ("том", Perc),
            ("foley", Perc), ("фоли", Perc),
            ("vox", Vox), ("vocal", Vox), ("вокал", Vox), ("вокалы", Vox),
            ("chant", Vox), ("чант", Vox),
            ("fx", Fx), ("эффект", Fx), ("эффекты", Fx),
            ("sweep", Fx), ("свип", Fx),
            ("impact", Fx), ("импакт", Fx),
            ("riser", Fx), ("райзер", Fx),
            ("midi", Fx), ("миди", Fx),
            ("texture", Fx), ("текстура", Fx),
            ("ambience", Fx), ("эмбиенс", Fx),
            ("piano", Loop), ("пиано", Loop), ("пианино", Loop),
            ("guitar", Loop), ("гитара", Loop), ("гитары", Loop),
            ("bell", Loop), ("белл", Loop), ("колокол", Loop),
            ("pluck", Loop), ("плак", Loop),
            ("synth", Loop), ("синт", Loop), ("синтезатор", Loop),
            ("pad", Loop), ("пэд", Loop), ("пад", Loop),
            ("bass", Loop), ("бас", Loop), ("басс", Loop),
            ("melody", Loop), ("мелодия", Loop), ("мелодии", Loop),
            ("loop", Loop), ("луп", Loop), ("лупы", Loop),
            ("drumloop", DrumLoop), ("драмлуп", DrumLoop),
        ]
        .into_iter()
        .collect()
    })
}

fn mood_terms() -> &'static HashMap<&'static str, &'static str> {
    static M: OnceLock<HashMap<&'static str, &'static str>> = OnceLock::new();
    M.get_or_init(|| {
        [
            ("dark", "dark"), ("тёмные", "dark"), ("темные", "dark"), ("тёмный", "dark"), ("темный", "dark"),
            ("aggressive", "aggressive"), ("агрессивные", "aggressive"), ("агрессивный", "aggressive"), ("злые", "aggressive"),
            ("emotional", "emotional"), ("эмоциональные", "emotional"), ("эмоциональный", "emotional"),
            ("sad", "sad"), ("грустные", "sad"), ("грустный", "sad"), ("печальные", "sad"),
            ("melodic", "melodic"), ("мелодичные", "melodic"), ("мелодичный", "melodic"),
            ("trap", "trap"), ("трэп", "trap"), ("трап", "trap"),
            ("rage", "rage"), ("рейдж", "rage"), ("рэйдж", "rage"),
            ("drill", "drill"), ("дрилл", "drill"),
            ("cloud", "cloud"), ("клауд", "cloud"),
            ("jersey", "jersey"), ("джерси", "jersey"),
            ("pluggnb", "pluggnb"), ("плагг", "pluggnb"), ("плаг", "pluggnb"),
            ("futuristic", "futuristic"), ("футуристичные", "futuristic"), ("футуристичный", "futuristic"),
            ("hard", "hard"), ("жёсткие", "hard"), ("жесткие", "hard"),
            ("soft", "soft"), ("мягкие", "soft"), ("мягкий", "soft"),
            ("warm", "warm"), ("тёплые", "warm"), ("теплые", "warm"),
            ("bright", "bright"), ("яркие", "bright"), ("яркий", "bright"),
            ("lofi", "lofi"), ("лоуфай", "lofi"), ("лофай", "lofi"),
        ]
        .into_iter()
        .collect()
    })
}

fn bpm_re() -> &'static Regex {
    static R: OnceLock<Regex> = OnceLock::new();
    R.get_or_init(|| Regex::new(r"(?:(?:на|at)\s+)?(\d{2,3})\s*(?:bpm|бпм)").unwrap())
}
fn bare_num_re() -> &'static Regex {
    static R: OnceLock<Regex> = OnceLock::new();
    R.get_or_init(|| Regex::new(r"\b(\d{2,3})\b").unwrap())
}

/// Токенизация: разбить по не-алфанумерике, сохраняя a-z, 0-9, а-я, ё и дефис
/// (чтобы «хай-хэт» осталось одним токеном).
fn tokenize(s: &str) -> Vec<String> {
    let keep = |r: char| -> bool {
        r == '-' || r.is_ascii_lowercase() || r.is_ascii_digit() || ('а'..='я').contains(&r) || r == 'ё'
    };
    s.split(|c: char| !keep(c)).filter(|t| !t.is_empty()).map(|t| t.to_string()).collect()
}

/// Разбирает свободный текст в структурный запрос.
pub fn parse(text: &str) -> Parsed {
    let mut lower = text.trim().to_lowercase();
    let mut p = Parsed::default();

    // Темп: сначала явный «<n> bpm», иначе голое 2–3-значное число.
    if let Some(caps) = bpm_re().captures(&lower) {
        if let Ok(n) = caps[1].parse::<i32>() {
            p.min_bpm = n - 3;
            p.max_bpm = n + 3;
        }
        lower = bpm_re().replace_all(&lower, " ").into_owned();
    } else if let Some(caps) = bare_num_re().captures(&lower) {
        if &caps[1] != "808" {
            if let Ok(n) = caps[1].parse::<i32>() {
                if (40..=220).contains(&n) {
                    p.min_bpm = n - 3;
                    p.max_bpm = n + 3;
                }
            }
        }
    }
    if lower.contains("медленны") || lower.contains("slow") {
        p.max_bpm = 95;
    }
    if lower.contains("быстры") || lower.contains("fast") {
        p.min_bpm = 140;
    }

    let mut leftover: Vec<String> = Vec::new();
    for tok in tokenize(&lower) {
        if let Some(&cat) = instrument_terms().get(tok.as_str()) {
            if !p.categories.contains(&cat) {
                p.categories.push(cat);
            }
            continue;
        }
        if let Some(&tag) = mood_terms().get(tok.as_str()) {
            if !p.tags.iter().any(|t| t == tag) {
                p.tags.push(tag.to_string());
            }
            continue;
        }
        leftover.push(tok);
    }

    // Нераспознанные токены → в текст. Если структурного нет вообще — исходная
    // строка (частичные CJK/эмодзи/редкие имена всё равно найдутся).
    if !leftover.is_empty() {
        p.free_text = leftover.join(" ");
    } else if p.categories.is_empty() && p.tags.is_empty() && p.min_bpm == 0 && p.max_bpm == 0 {
        p.free_text = text.trim().to_string();
    }
    p
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn structured_ru_query() {
        let p = parse("тёмные агрессивные 808 на 140 bpm");
        assert_eq!(p.categories, vec![C808]);
        assert!(p.tags.iter().any(|t| t == "dark"));
        assert!(p.tags.iter().any(|t| t == "aggressive"));
        assert_eq!((p.min_bpm, p.max_bpm), (137, 143));
        assert_eq!(p.free_text, ""); // structured → no free text
    }

    #[test]
    fn instrument_to_loop_category() {
        let p = parse("dark melodic piano");
        assert_eq!(p.categories, vec![Loop]);
        assert!(p.tags.iter().any(|t| t == "dark"));
        assert!(p.tags.iter().any(|t| t == "melodic"));
    }

    #[test]
    fn free_text_fallback() {
        let p = parse("zzz weird name");
        assert!(!p.free_text.is_empty());
        assert!(p.categories.is_empty() && p.tags.is_empty());
    }

    #[test]
    fn tempo_words_and_808_not_tempo() {
        let slow = parse("slow 808");
        assert_eq!(slow.max_bpm, 95);
        assert_eq!(slow.categories, vec![C808]); // 808 is an instrument, not a tempo
    }
}
