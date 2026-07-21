// Дедуп: точные дубли (по контент-хэшу) + акустические (близость перцептивных
// отпечатков по Хэммингу ≤ порога). Порт dedup/index.go. Контент-хэш —
// быстрый (размер + первые/последние 64 КБ), без крипто-зависимостей.

use std::collections::HashMap;
use std::fs::File;
use std::hash::{Hash, Hasher};
use std::io::{Read, Seek, SeekFrom};
use std::path::Path;

/// Макс. расстояние Хэмминга (из 496 бит), при котором звуки считаются
/// акустически идентичными. Калибровка из dedup/index.go: ре-энкод/умеренный
/// гейн/трим держится <~64 бит, близкий-но-иной питч ~110, разные звуки 200+.
pub const DEFAULT_ACOUSTIC_THRESHOLD: i32 = 80;

const QUICK_BLOCK: usize = 65536; // 64 КБ спереди + 64 КБ сзади

/// Быстрый контент-хэш: размер + первые и последние 64 КБ (≤128 КБ чтения).
/// Достаточно для точного дедупа личной библиотеки; коллизии реальных
/// аудиофайлов астрономически маловероятны. None при ошибке I/O.
pub fn quick_hash(path: &Path) -> Option<String> {
    let mut f = File::open(path).ok()?;
    let size = f.metadata().ok()?.len();

    let mut h = std::collections::hash_map::DefaultHasher::new();
    size.hash(&mut h);

    let mut front = vec![0u8; QUICK_BLOCK];
    let n = read_up_to(&mut f, &mut front);
    front[..n].hash(&mut h);

    if size > QUICK_BLOCK as u64 && f.seek(SeekFrom::End(-(QUICK_BLOCK as i64))).is_ok() {
        let mut back = vec![0u8; QUICK_BLOCK];
        let m = read_up_to(&mut f, &mut back);
        back[..m].hash(&mut h);
    }
    Some(format!("q{:016x}", h.finish()))
}

fn read_up_to(f: &mut File, buf: &mut [u8]) -> usize {
    let mut filled = 0;
    while filled < buf.len() {
        match f.read(&mut buf[filled..]) {
            Ok(0) => break,
            Ok(k) => filled += k,
            Err(_) => break,
        }
    }
    filled
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DupKind {
    Unique,
    Exact,
    Acoustic,
}

struct FpRef {
    fp: String,
    name: String,
}

/// Индекс виденных (сохранённых) уникальных файлов текущего прохода. Не
/// потокобезопасен — решения о дублях сериализуются вызывающим.
pub struct DedupIndex {
    by_hash: HashMap<String, String>, // контент-хэш → имя (lowercase)
    prints: Vec<FpRef>,
    threshold: i32,
    deep: bool,
}

impl DedupIndex {
    pub fn new(deep: bool, threshold: i32) -> Self {
        Self {
            by_hash: HashMap::new(),
            prints: Vec::new(),
            threshold: if threshold <= 0 { DEFAULT_ACOUSTIC_THRESHOLD } else { threshold },
            deep,
        }
    }

    /// Совпадает ли кандидат с уже проиндексированным. Возвращает вид совпадения
    /// и имя существующей записи. Acoustic — вероятностный: вызывающий обычно
    /// дополнительно требует равенства имён (см. dedup/index.go).
    pub fn check(&self, content_hash: &str, fingerprint: &str, name: &str) -> (DupKind, Option<String>) {
        let _ = name;
        if !content_hash.is_empty() {
            if let Some(existing) = self.by_hash.get(content_hash) {
                return (DupKind::Exact, Some(existing.clone()));
            }
        }
        if self.deep && !fingerprint.is_empty() {
            if let Some(existing) = self.nearest(fingerprint) {
                return (DupKind::Acoustic, Some(existing));
            }
        }
        (DupKind::Unique, None)
    }

    fn nearest(&self, fp: &str) -> Option<String> {
        let mut best: Option<(&str, i32)> = None;
        for r in &self.prints {
            let d = flapp_dsp::hamming_hex(fp, &r.fp);
            if d < 0 {
                continue; // несопоставимы
            }
            if d <= self.threshold && best.is_none_or(|(_, bd)| d < bd) {
                best = Some((r.name.as_str(), d));
            }
        }
        best.map(|(n, _)| n.to_string())
    }

    /// Записать сохранённый уникальный файл, чтобы позднее кандидаты матчились.
    pub fn add(&mut self, content_hash: &str, fingerprint: &str, name: &str) {
        let low = name.to_ascii_lowercase();
        if !content_hash.is_empty() {
            self.by_hash.insert(content_hash.to_string(), low.clone());
        }
        if self.deep && !fingerprint.is_empty() {
            self.prints.push(FpRef { fp: fingerprint.to_string(), name: low });
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn hex(byte: u8) -> String {
        // 62-byte (496-bit) fingerprint of a repeated byte → 124 hex chars.
        std::iter::repeat(format!("{byte:02x}")).take(62).collect()
    }

    #[test]
    fn exact_by_content_hash() {
        let mut ix = DedupIndex::new(true, 0);
        ix.add("qcafe", "", "kick.wav");
        assert_eq!(ix.check("qcafe", "", "other.wav").0, DupKind::Exact);
        assert_eq!(ix.check("qbeef", "", "other.wav").0, DupKind::Unique);
    }

    #[test]
    fn acoustic_within_threshold_else_unique() {
        let a = hex(0x00);
        let near = hex(0x01); // differs 1 bit/byte × 62 = 62 bits ≤ 80 → acoustic
        let far = hex(0xff); // differs 8 bits/byte × 62 = 496 bits → unique
        let mut ix = DedupIndex::new(true, DEFAULT_ACOUSTIC_THRESHOLD);
        ix.add("", &a, "loop_a.wav");

        let (kind, existing) = ix.check("", &near, "loop_b.wav");
        assert_eq!(kind, DupKind::Acoustic);
        assert_eq!(existing.as_deref(), Some("loop_a.wav"));

        assert_eq!(ix.check("", &far, "loop_c.wav").0, DupKind::Unique);
    }

    #[test]
    fn shallow_mode_ignores_fingerprints() {
        let a = hex(0x00);
        let near = hex(0x01);
        let mut ix = DedupIndex::new(false, 0); // deep = false
        ix.add("", &a, "x.wav");
        assert_eq!(ix.check("", &near, "y.wav").0, DupKind::Unique);
    }
}
