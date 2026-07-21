// Faithful Rust port of backend/internal/infrastructure/classify/rules.go.
// Filename/folder-based sample categorization. The "bd" = 808 (TR-808 bass drum
// used as the sub instrument) convention is intentional and user-confirmed —
// NOT the generic GM "bass drum = kick" shorthand. Keep in sync with rules.go
// and the 11-category taxonomy in category.go.

use std::sync::OnceLock;

use regex::Regex;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Category {
    C808,
    Kick,
    Snare,
    Clap,
    HiHat,
    OpenHat,
    Perc,
    Vox,
    Fx,
    Loop,
    DrumLoop,
}

impl Category {
    pub fn label(self) -> &'static str {
        match self {
            Category::C808 => "808",
            Category::Kick => "Kick",
            Category::Snare => "Snare",
            Category::Clap => "Clap",
            Category::HiHat => "Hi-Hat",
            Category::OpenHat => "Open Hat",
            Category::Perc => "Perc",
            Category::Vox => "Vox",
            Category::Fx => "FX",
            Category::Loop => "Loop",
            Category::DrumLoop => "Drum Loop",
        }
    }

    pub const ALL: [Category; 11] = [
        Category::C808, Category::Kick, Category::Snare, Category::Clap,
        Category::HiHat, Category::OpenHat, Category::Perc, Category::Vox,
        Category::Fx, Category::Loop, Category::DrumLoop,
    ];
}

use Category::*;

// nameRules: evaluated top-to-bottom, substring matching (contains_term).
fn name_rules() -> &'static [(Category, &'static [&'static str])] {
    &[
        (DrumLoop, &[
            "drum loop", "drumloop", "drum_loop",
            "drum fill", "drumfill", "drum_fill",
            "top loop", "toploop", "top_loop",
            "beat loop", "groove loop",
            "breakbeat", "break beat", "amen break",
            "drums_top", "drums top",
            "stems", "[stem]",
        ]),
        (Loop, &[
            "melody loop", "melodyloop", "melodic loop",
            "music loop", "chord loop",
            " loop", "_loop", "-loop", "loop_", "loop-",
            "fullloop", "full loop", "full_loop",
            "[starter]", "[phrase]",
        ]),
        (C808, &[
            "808",
            "sub bass", "subbass", "sub_bass",
            "808 bass", "808bass",
            "_bd_", "_bd.", "-bd-", " bd_", " bd.",
            " sub ", "_sub_", "-sub-", "sub_", "-sub", "_sub.",
        ]),
        (Kick, &[
            "kick", "kik", "kck",
            "bassdrum", "bass drum", "base drum",
            "_kd_", "_kd.", "-kd-", "kd_", " kd_", " kd.",
            " boom", "_boom", "-boom",
        ]),
        (Snare, &[
            "snare", "snr", "snare roll",
            " sn ", "_sn_", "_sn.", "-sn-", "_sn-",
            "sn_", "sn-", "sn.",
        ]),
        (Clap, &[
            "clap", "claps", "handclap", "hand clap",
            "rimshot", "rim shot", "rim_shot",
            "sidestick", "side stick",
            " rim ", "_rim_", "_rim.", "rim_",
            "snap", "finger snap",
            "_cl_", "_cl.", " cl_", " cl.", "cl_", "cl-",
        ]),
        (OpenHat, &[
            "open hat", "openhat", "open_hat", "open-hat",
            "ophat", "op hat",
            "open hi hat", "open hihat", "open hi-hat",
            "crash", "ride", "cymbal", "cym_", "_cym",
            "splash", "china", "stack",
            "_oh_", "_oh.", "-oh-", "oh_", "-oh_", " oh_", " oh.", "_oh-",
            "_cr_", "_cr.", "-cr-", "cr_", " cr_", " cr.",
            "_rd_", "_rd.", "-rd-", "rd_", " rd_", " rd.",
        ]),
        (HiHat, &[
            "hihat", "hi-hat", "hi hat", "hi_hat",
            "closed hat", "closedhat", "closed_hat",
            "chh", "clhat",
            "_hh_", "_hh.", "-hh-", "hh_", "-hh_", "_hh-",
            "hh.", "hh-",
        ]),
        (Perc, &[
            "perc", "percussion",
            "tom", "floor tom", "rack tom",
            "shaker", "tamb", "tambourine",
            "conga", "bongo", "bongos",
            "cowbell", "woodblock", "wood block",
            "clave", "triangle", "agogo", "cabasa", "guiro",
            "maraca", "cajon", "djembe",
            "foley", "footstep",
            "vinyl crackle", "record crackle",
            " hit", "_hit", "-hit", "hit_", "hit-",
            "_pc_", "_pc.", " pc_", " pc.",
            "scratch", "scratches",
        ]),
        (Vox, &[
            "vocal", "vocals", "lead vox",
            "chant", "shout", "yell", "scream",
            "verse", "hook", "chorus",
            "acapella", "acapela", "a capella",
            "adlib", "ad lib", "ad-lib",
            "vocal chop", "vox chop",
            "voice", "voices",
            "moan", "breath",
            "_what", " what", "-what",
            "_yeah", " yeah", "-yeah",
            "_yah", " yah",
            "_hey", " hey",
            "_ayy", " ayy",
            "_brr", " brr",
            "_skrr", " skrr",
            "_ch_", "_ch.", " ch_", " ch.", "-ch-", "-ch_",
        ]),
        (Fx, &[
            "riser", "rise", "uplifter", "uplift",
            "build up", "buildup", "build_up",
            "downlifter", "downer",
            "drop fx", "dropfx",
            "sweep", "whoosh", "swoosh",
            "transition",
            "impact", "slam", "braam", "brahm",
            "hit fx", "hitfx",
            " fx", "_fx", "-fx", "fx_", "fx-", "sfx",
            "effect", "effects",
            "glitch", "stutter",
            "reverse", "reversed", "_rev", " rev ",
            "texture", "drone",
            "noise floor", "noise_floor",
            "ambience", "ambient",
            "atmosphere", "atmos",
            "midi", ".mid", "_midi", " midi",
            "crowd", "wobble", "wobb",
            "brass",
        ]),
        (Loop, &[
            "piano", "rhodes", "keys", "grand", "epiano", "e-piano", "wurli", "wurlitzer",
            "guitar", "gtr", "acoustic gtr",
            "bell", "bells", "glock", "glockenspiel", "chime", "kalimba", "marimba",
            "pluck", "plk",
            "synth", "synthesizer",
            "supersaw",
            " arp", "_arp", "-arp", "arpeggio",
            "bassline", "bass line", "bass_line",
            "reese",
            "melody", "melodic",
            "chord", "chords", "harmony",
            "progression", "topline", "top line",
            " pad", "_pad", "-pad", "pad_", "pad-",
            " saw", "_saw", "-saw",
            " stab", "_stab", "-stab",
            "strings", "string",
            " lead", "_lead", "-lead",
        ]),
    ]
}

fn abbreviation_rules() -> &'static [(Category, &'static [&'static str])] {
    &[
        (C808, &["bd", "sub"]),
        (Kick, &["kk"]),
        (Snare, &["sn", "sd"]),
        (Clap, &["cl", "sc"]),
        (OpenHat, &["oh", "cr", "rd"]),
        (HiHat, &["hh", "hat"]),
        (Vox, &["ch", "vox", "what", "yeah", "ayy", "hey", "brr"]),
    ]
}

fn folder_category_map() -> &'static [(&'static str, Category)] {
    &[
        ("808", C808), ("808s", C808),
        ("sub", C808), ("sub bass", C808), ("bd", C808),
        ("kick", Kick), ("kicks", Kick),
        ("bass drum", Kick), ("bassdrum", Kick),
        ("snare", Snare), ("snares", Snare),
        ("clap", Clap), ("claps", Clap),
        ("rimshot", Clap), ("rimshots", Clap), ("rimz", Clap),
        ("rims", Clap), ("rim", Clap), ("rim shots", Clap),
        ("hh", HiHat), ("hi hat", HiHat), ("hi hats", HiHat),
        ("hihat", HiHat), ("hihats", HiHat), ("hi_hat", HiHat),
        ("hi-hat", HiHat), ("hi-hats", HiHat),
        ("closed hat", HiHat), ("closedhat", HiHat),
        ("oh", OpenHat), ("open hat", OpenHat), ("open hats", OpenHat),
        ("openhat", OpenHat), ("open-hat", OpenHat), ("op hat", OpenHat),
        ("crash", OpenHat), ("crashes", OpenHat),
        ("cymbal", OpenHat), ("cymbals", OpenHat),
        ("oh & crashes", OpenHat),
        ("perc", Perc), ("percs", Perc), ("percussion", Perc),
        ("scratch", Perc), ("scratches", Perc),
        ("shaker", Perc), ("shakers", Perc),
        ("tom", Perc), ("toms", Perc),
        ("fx", Fx), ("fxs", Fx), ("sfx", Fx),
        ("effects", Fx), ("riser", Fx), ("risers", Fx),
        ("vox", Vox), ("vocals", Vox), ("vocal", Vox),
        ("chants", Vox), ("voices", Vox),
        ("loop", Loop), ("loops", Loop), ("loopkit", Loop),
        ("melody loops", Loop),
        ("pluck", Loop), ("plucks", Loop),
        ("pad", Loop), ("pads", Loop),
        ("lead", Loop), ("leads", Loop),
        ("synth", Loop), ("strings", Loop),
        ("melody", Loop), ("melodies", Loop),
        ("bell", Loop), ("bells", Loop),
        ("bass", C808),
        ("drum loops", DrumLoop), ("loops drums", DrumLoop),
    ]
}

fn folder_lookup(dir: &str) -> Option<Category> {
    folder_category_map().iter().find(|(k, _)| *k == dir).map(|(_, c)| *c)
}

// Folder keywords ordered longest-first (deterministic substring fallback).
fn folder_keywords_by_length() -> &'static [(&'static str, Category)] {
    static KEYS: OnceLock<Vec<(&'static str, Category)>> = OnceLock::new();
    KEYS.get_or_init(|| {
        let mut v: Vec<(&'static str, Category)> = folder_category_map().to_vec();
        v.sort_by(|a, b| b.0.len().cmp(&a.0.len()).then(a.0.cmp(b.0)));
        v
    })
}

fn classify_by_folder_path(rel_path: &str) -> Option<(Category, f64)> {
    let slashed = rel_path.replace('\\', "/");
    let parts: Vec<&str> = slashed.split('/').collect();
    let n = parts.len();
    if n < 2 {
        return None;
    }
    let start = n - 2;
    let mut i = start as isize;
    let lower_bound = (n as isize) - 5;
    while i >= 0 && i >= lower_bound {
        let dir = parts[i as usize].trim().to_ascii_lowercase();
        if let Some(cat) = folder_lookup(&dir) {
            let mut weight = 9.0 - ((n as isize - 2 - i) as f64) * 1.5;
            if weight < 4.0 {
                weight = 4.0;
            }
            return Some((cat, weight));
        }
        for (keyword, cat) in folder_keywords_by_length() {
            if keyword.len() >= 3 && dir.contains(keyword) {
                return Some((*cat, 5.0));
            }
        }
        i -= 1;
    }
    None
}

fn dash_prefix_rules() -> &'static [(&'static str, Category)] {
    &[
        ("open hat", OpenHat), ("hi hat", HiHat), ("hihat", HiHat),
        ("808", C808), ("snare", Snare), ("clap", Clap), ("kick", Kick),
        ("perc", Perc), ("sfx", Fx), ("fx", Fx), ("vox", Vox),
        ("strings", Loop), ("melody", Loop), ("synth", Loop),
        ("brass", Fx), ("pluck", Loop), ("lead", Loop), ("pad", Loop),
        ("bass", C808), ("hh", HiHat), ("oh", OpenHat), ("sn", Snare),
    ]
}

fn classify_by_dash_prefix(name: &str) -> Option<Category> {
    let lower = name.to_ascii_lowercase();
    for (prefix, cat) in dash_prefix_rules() {
        if lower.starts_with(&format!("{prefix} - ")) || lower.starts_with(&format!("{prefix} ")) {
            return Some(*cat);
        }
    }
    None
}

fn abbreviation_token_map() -> &'static [(&'static str, Category)] {
    &[
        ("bd", C808), ("sc", Clap), ("sn", Snare), ("sd", Snare),
        ("oh", OpenHat), ("hh", HiHat), ("kd", Kick), ("cr", OpenHat),
        ("rd", OpenHat), ("chh", HiHat),
    ]
}

fn classify_by_abbreviations(haystack: &str) -> Option<Category> {
    for (token, cat) in abbreviation_token_map() {
        if contains_word(haystack, token) {
            return Some(*cat);
        }
    }
    None
}

fn detect_loop_by_pattern(name: &str) -> (bool, bool) {
    static RES: OnceLock<[Regex; 6]> = OnceLock::new();
    let re = RES.get_or_init(|| {
        [
            Regex::new(r"\b\d{2,3}[\s_]?bpm\b").unwrap(),
            Regex::new(r"\b\d{2,3}[\s_]bpm[\s_]").unwrap(),
            Regex::new(r"\[starter\]").unwrap(),
            Regex::new(r"\[phrase\]").unwrap(),
            Regex::new(r"[\s_]drums?[\s_]top[\s_]?|drums_top").unwrap(),
            Regex::new(r"[\s_]drums?[\s_]").unwrap(),
        ]
    });
    let lower = name.to_ascii_lowercase();
    let has_bpm = re[0].is_match(&lower) || re[1].is_match(&lower);
    let has_starter = re[2].is_match(&lower);
    let has_phrase = re[3].is_match(&lower);
    let has_drums_top = re[4].is_match(&lower);
    let has_drums = re[5].is_match(&lower);

    if has_drums_top || (has_bpm && has_drums) {
        return (true, true);
    }
    if has_bpm || has_starter || has_phrase {
        return (true, false);
    }
    (false, false)
}

fn resolve_kick_vs_808(haystack: &str) -> Option<Category> {
    let has_kick_word = contains_term(haystack, "kick") || contains_term(haystack, "kik")
        || contains_term(haystack, "kck") || contains_term(haystack, "bassdrum")
        || contains_term(haystack, "bass drum") || contains_term(haystack, "base drum");
    if !has_kick_word {
        return None;
    }
    let has_sub_bass_word = contains_term(haystack, "sub bass") || contains_term(haystack, "subbass")
        || contains_term(haystack, "sub_bass") || contains_term(haystack, "808 bass")
        || contains_term(haystack, "808bass") || contains_term(haystack, "bassline")
        || contains_term(haystack, "bass line") || contains_term(haystack, "bass_line")
        || contains_word(haystack, "sub");
    if has_sub_bass_word {
        return None;
    }
    Some(Kick)
}

/// Six sequential passes. Returns (cat, score). Score 9.0 = folder → 5.0 = keyword.
pub fn classify_by_name(name: &str, rel_path: &str) -> Option<(Category, f64)> {
    if let Some((cat, score)) = classify_by_folder_path(rel_path) {
        return Some((cat, score));
    }
    let lower = format!("{name} {rel_path}").to_ascii_lowercase();
    if let Some(cat) = resolve_kick_vs_808(&lower) {
        return Some((cat, 8.0));
    }
    if let Some(cat) = classify_by_dash_prefix(name) {
        return Some((cat, 9.0));
    }
    if let Some(cat) = classify_by_abbreviations(&lower) {
        return Some((cat, 7.0));
    }
    let (is_loop, is_drum) = detect_loop_by_pattern(name);
    if is_loop {
        return Some((if is_drum { DrumLoop } else { Loop }, 7.0));
    }
    if let Some(cat) = keyword_classifier(&lower) {
        return Some((cat, 5.0));
    }
    None
}

fn keyword_classifier(haystack: &str) -> Option<Category> {
    for (cat, terms) in name_rules() {
        for term in *terms {
            if contains_term(haystack, term) {
                return Some(*cat);
            }
        }
    }
    for (cat, abbrs) in abbreviation_rules() {
        for abbr in *abbrs {
            if contains_word(haystack, abbr) {
                return Some(*cat);
            }
        }
    }
    None
}

// ── String helpers (byte-level, mirror rules.go) ────────────────────────────

fn contains_term(haystack: &str, term: &str) -> bool {
    haystack.contains(term)
}

fn contains_word(haystack: &str, word: &str) -> bool {
    let (hb, wb) = (haystack.as_bytes(), word.as_bytes());
    let (n, m) = (hb.len(), wb.len());
    if m == 0 || m > n {
        return false;
    }
    let mut i = 0;
    while i + m <= n {
        if &hb[i..i + m] == wb {
            let before_alpha = i > 0 && is_alpha(hb[i - 1]);
            let after_alpha = i + m < n && is_alpha(hb[i + m]);
            if !before_alpha && !after_alpha {
                return true;
            }
        }
        i += 1;
    }
    false
}

fn is_alpha(c: u8) -> bool {
    c.is_ascii_lowercase()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn c(name: &str) -> Category {
        classify_by_name(name, name).expect("classified").0
    }

    #[test]
    fn ported_go_cases() {
        assert_eq!(c("808 deep sub F.wav"), C808);
        assert_eq!(c("sub_bass_C.wav"), C808);
        assert_eq!(c("808 Bass.wav"), C808);
        assert_eq!(c("808 Sub Slide.wav"), C808);
        assert_eq!(c("bd_01.wav"), C808);
        assert_eq!(c("BD_heavy.wav"), C808);
        assert_eq!(c("BD 01.wav"), C808);

        assert_eq!(c("kick_punchy_01.wav"), Kick);
        assert_eq!(c("bassdrum_hard.wav"), Kick);
        assert_eq!(c("808 Kick.wav"), Kick);
        assert_eq!(c("Kick 808 Deep.wav"), Kick);
        assert_eq!(c("Trap Kick 808 Bright.wav"), Kick);
        assert_eq!(c("Kick_BD_808.wav"), Kick);

        assert_eq!(c("snare_01.wav"), Snare);
        assert_eq!(c("sn_tight.wav"), Snare);
        assert_eq!(c("SN.wav"), Snare);
        assert_eq!(c("sn 01.wav"), Snare);
        assert_eq!(c("drum_sn.wav"), Snare);
        assert_eq!(c("sn-crispy.wav"), Snare);

        assert_eq!(c("clap_layered.wav"), Clap);
        assert_eq!(c("cl_01.wav"), Clap);
        assert_eq!(c("rimshot_crack.wav"), Clap);

        assert_eq!(c("open_hat_long.wav"), OpenHat);
        assert_eq!(c("ride_warm.wav"), OpenHat);
        assert_eq!(c("crash_heavy.wav"), OpenHat);
        assert_eq!(c("oh_01.wav"), OpenHat);
        assert_eq!(c("OH.wav"), OpenHat);

        assert_eq!(c("hh_closed.wav"), HiHat);
        assert_eq!(c("hh01.wav"), HiHat);
        assert_eq!(c("hat_01.wav"), HiHat);
        assert_eq!(c("chh_01.wav"), HiHat);

        assert_eq!(c("ch_vox.wav"), Vox);
        assert_eq!(c("CH.wav"), Vox);
        assert_eq!(c("vox_adlib_yeah.wav"), Vox);
        assert_eq!(c("what_adlib.wav"), Vox);

        assert_eq!(c("top_drum_loop_140.wav"), DrumLoop);
        assert_eq!(c("piano_loop_140.wav"), Loop);
        assert_eq!(c("melodic loop Cmin.wav"), Loop);

        assert_eq!(c("riser_long_tail.wav"), Fx);
        assert_eq!(c("project_bounce.mid"), Fx);
    }

    #[test]
    fn folder_path_is_deterministic_kick_over_808() {
        // "808 Kicks" folder → length-sorted "kick" beats "808" every call.
        for _ in 0..20 {
            let got = classify_by_name("Deep 01.wav", "Drums/808 Kicks/Deep 01.wav");
            assert_eq!(got.map(|(c, _)| c), Some(Kick));
        }
    }
}
