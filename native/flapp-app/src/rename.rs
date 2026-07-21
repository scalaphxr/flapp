// Движок массового переименования (Beat Manager). Чистый порт ядра
// usecase/beatmanager.go: applyRename/applyOp/titleCase/insertAroundBPM/
// smartMarketingName. Только трансформация имени (без диска/БД — это UI-слой).

use std::sync::OnceLock;

use regex::Regex;

/// Контекст сэмпла для операций, зависящих от метаданных (smart, вставка у BPM).
#[derive(Default, Clone)]
pub struct RenameCtx {
    pub name: String,
    pub bpm: u32,          // 0 = неизвестно
    pub key: String,       // "" = неизвестно
    pub category: String,  // метка категории, "" = неизвестно
    pub tags: Vec<String>,
}

/// Одна операция конвейера переименования.
#[derive(Clone)]
pub enum RenameOp {
    Upper,
    Lower,
    Title,
    StripLeadingNum,
    StripTrailingNum,
    RemoveSpecial,
    Trim,
    Prefix(String),
    Suffix(String),
    Replace { from: String, to: String, regex: bool },
    RegexReplace { from: String, to: String },
    InsertBeforeBpm(String),
    InsertAfterBpm(String),
    SmartMarketing,
}

fn re(cell: &'static OnceLock<Regex>, pat: &str) -> &'static Regex {
    cell.get_or_init(|| Regex::new(pat).unwrap())
}

fn leading_digits() -> &'static Regex {
    static R: OnceLock<Regex> = OnceLock::new();
    re(&R, r"^[\s0-9._-]+")
}
fn trailing_digits() -> &'static Regex {
    static R: OnceLock<Regex> = OnceLock::new();
    re(&R, r"[\s0-9._-]+$")
}
fn special() -> &'static Regex {
    static R: OnceLock<Regex> = OnceLock::new();
    re(&R, r"[^\p{L}\p{N}\s_-]+")
}
fn multi_space() -> &'static Regex {
    static R: OnceLock<Regex> = OnceLock::new();
    re(&R, r"\s{2,}")
}
fn bpm_token() -> &'static Regex {
    static R: OnceLock<Regex> = OnceLock::new();
    re(&R, r"(?i)\b\d{2,3}\s?bpm\b")
}

/// Разбивает имя на (stem, ext-с-точкой), как filepath.Ext + TrimSuffix.
/// Ведущая точка (`.hidden`) трактуется как отсутствие расширения.
fn split_ext(name: &str) -> (&str, &str) {
    match name.rfind('.') {
        Some(i) if i > 0 => (&name[..i], &name[i..]),
        _ => (name, ""),
    }
}

/// Прогоняет весь конвейер по имени; расширение сохраняется, трансформы
/// работают только по stem.
pub fn apply_rename(ctx: &RenameCtx, ops: &[RenameOp]) -> String {
    let (stem0, ext) = split_ext(&ctx.name);
    let mut stem = stem0.to_string();
    for op in ops {
        stem = apply_op(&stem, op, ctx);
    }
    let trimmed = stem.trim();
    let stem = if trimmed.is_empty() { stem0 } else { trimmed };
    format!("{stem}{ext}")
}

fn apply_op(stem: &str, op: &RenameOp, ctx: &RenameCtx) -> String {
    match op {
        RenameOp::Upper => stem.to_uppercase(),
        RenameOp::Lower => stem.to_lowercase(),
        RenameOp::Title => title_case(stem),
        RenameOp::StripLeadingNum => leading_digits().replace(stem, "").into_owned(),
        RenameOp::StripTrailingNum => trailing_digits().replace(stem, "").into_owned(),
        RenameOp::RemoveSpecial => {
            let s = special().replace_all(stem, " ");
            multi_space().replace_all(&s, " ").trim().to_string()
        }
        RenameOp::Trim => multi_space().replace_all(stem, " ").trim().to_string(),
        RenameOp::Prefix(t) => format!("{t}{stem}"),
        RenameOp::Suffix(t) => format!("{stem}{t}"),
        RenameOp::Replace { from, to, regex } => {
            if from.is_empty() {
                return stem.to_string();
            }
            if *regex {
                match Regex::new(from) {
                    Ok(r) => r.replace_all(stem, to.as_str()).into_owned(),
                    Err(_) => stem.to_string(),
                }
            } else {
                stem.replace(from, to)
            }
        }
        RenameOp::RegexReplace { from, to } => {
            if from.is_empty() {
                return stem.to_string();
            }
            match Regex::new(from) {
                Ok(r) => r.replace_all(stem, to.as_str()).into_owned(),
                Err(_) => stem.to_string(),
            }
        }
        RenameOp::InsertBeforeBpm(t) => insert_around_bpm(stem, t, ctx, true),
        RenameOp::InsertAfterBpm(t) => insert_around_bpm(stem, t, ctx, false),
        RenameOp::SmartMarketing => smart_marketing(ctx),
    }
}

/// Заглавная первая буква каждого слова; разделители — пробел, `_`, `-`.
fn title_case(s: &str) -> String {
    let mut at_boundary = true;
    s.chars()
        .map(|r| {
            if r.is_whitespace() || r == '_' || r == '-' {
                at_boundary = true;
                r
            } else if at_boundary {
                at_boundary = false;
                r.to_uppercase().next().unwrap_or(r)
            } else {
                r.to_lowercase().next().unwrap_or(r)
            }
        })
        .collect()
}

fn insert_around_bpm(stem: &str, text: &str, ctx: &RenameCtx, before: bool) -> String {
    if text.is_empty() {
        return stem.to_string();
    }
    if let Some(m) = bpm_token().find(stem) {
        if before {
            format!("{}{} {}", &stem[..m.start()], text, &stem[m.start()..])
        } else {
            format!("{} {}{}", &stem[..m.end()], text, &stem[m.end()..])
        }
    } else if ctx.bpm > 0 {
        if before {
            format!("{} {} {}BPM", stem.trim(), text, ctx.bpm)
        } else {
            format!("{} {}BPM {}", stem.trim(), ctx.bpm, text)
        }
    } else {
        format!("{} {}", stem.trim(), text)
    }
}

/// Готовое к витрине имя из метаданных: "Dark 808 - 140BPM - Am".
fn smart_marketing(ctx: &RenameCtx) -> String {
    let mut parts: Vec<String> = Vec::new();
    if let Some(t) = ctx.tags.first() {
        parts.push(title_case(t));
    }
    if !ctx.category.is_empty() {
        parts.push(ctx.category.clone());
    }
    let mut head = parts.join(" ");
    if head.is_empty() {
        head = split_ext(&ctx.name).0.to_string();
    }
    let mut segs = vec![head];
    if ctx.bpm > 0 {
        segs.push(format!("{}BPM", ctx.bpm));
    }
    if !ctx.key.is_empty() {
        segs.push(ctx.key.clone());
    }
    segs.join(" - ")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ctx(name: &str) -> RenameCtx {
        RenameCtx {
            name: name.into(),
            bpm: 140,
            key: "Am".into(),
            category: "Drum Loop".into(),
            tags: vec!["dark".into()],
            ..Default::default()
        }
    }

    // Ported from usecase/rename_search_test.go (sample "01_Dark Trap Loop 02.wav").
    #[test]
    fn pipeline_ops() {
        let c = ctx("01_Dark Trap Loop 02.wav");
        use RenameOp::*;
        let cases: &[(&[RenameOp], &str)] = &[
            (&[Upper], "01_DARK TRAP LOOP 02.wav"),
            (&[Lower], "01_dark trap loop 02.wav"),
            (&[Lower, Title], "01_Dark Trap Loop 02.wav"),
            (&[StripLeadingNum], "Dark Trap Loop 02.wav"),
            (&[StripTrailingNum], "01_Dark Trap Loop.wav"),
            (&[Prefix("MyKit_".into())], "MyKit_01_Dark Trap Loop 02.wav"),
            (&[Suffix("_DRY".into())], "01_Dark Trap Loop 02_DRY.wav"),
            (
                &[Replace { from: "Trap".into(), to: "Drill".into(), regex: false }],
                "01_Dark Drill Loop 02.wav",
            ),
            (
                &[RegexReplace { from: r"\d+".into(), to: "#".into() }],
                "#_Dark Trap Loop #.wav",
            ),
            (&[StripLeadingNum, StripTrailingNum, Trim], "Dark Trap Loop.wav"),
        ];
        for (ops, want) in cases {
            assert_eq!(apply_rename(&c, ops), *want, "ops -> {want}");
        }
    }

    #[test]
    fn remove_special() {
        let c = RenameCtx { name: "Kick!!! @#$ (final).wav".into(), ..Default::default() };
        assert_eq!(apply_rename(&c, &[RenameOp::RemoveSpecial]), "Kick final.wav");
    }

    #[test]
    fn insert_around_bpm_cases() {
        let with = RenameCtx { name: "Melody 140bpm.wav".into(), ..Default::default() };
        assert_eq!(
            apply_rename(&with, &[RenameOp::InsertBeforeBpm("Dark".into())]),
            "Melody Dark 140bpm.wav"
        );
        assert_eq!(
            apply_rename(&with, &[RenameOp::InsertAfterBpm("Am".into())]),
            "Melody 140bpm Am.wav"
        );
        // No BPM token → fall back to ctx.bpm.
        let no = RenameCtx { name: "Melody.wav".into(), bpm: 150, ..Default::default() };
        assert_eq!(
            apply_rename(&no, &[RenameOp::InsertAfterBpm("X".into())]),
            "Melody 150BPM X.wav"
        );
    }

    #[test]
    fn smart_marketing_name() {
        let c = RenameCtx {
            name: "raw_808_thing.wav".into(),
            category: "808".into(),
            bpm: 140,
            key: "Am".into(),
            tags: vec!["dark".into(), "trap".into()],
        };
        assert_eq!(apply_rename(&c, &[RenameOp::SmartMarketing]), "Dark 808 - 140BPM - Am.wav");
    }
}
