// Парсер FL Studio .flp (порт infrastructure/flp/parser.go). Формат — RIFF-подобный:
//   "FLhd" <u32 len=6> <u16 format> <u16 nChannels> <u16 ppq>
//   "FLdt" <u32 len>   <поток событий TLV>
// Событие: id(1 байт) → класс размера: <64 байт, <128 u16, <192 u32, >=192
// varlen. Достаём то, что нужно: BPM, заголовок/автор, каналы, пути сэмплов,
// плагины, ноты пианоролла. Неизвестные события безопасно пропускаются.

// Well-known event ids.
const EV_CHAN_TYPE: u8 = 21;
const EV_NEW_CHAN: u8 = 64;
const EV_NEW_PATTERN: u8 = 65;
const EV_TEMPO_LEGACY: u8 = 66;
const EV_FINE_TEMPO: u8 = 156;
const EV_FINE_TEMPO_ALT: u8 = 157;
const EV_PATTERN_NAME: u8 = 193;
const EV_TEXT_TITLE: u8 = 194;
const EV_TEXT_CHAN_NAME: u8 = 195;
const EV_TEXT_SAMPLE_PATH: u8 = 196;
const EV_VERSION: u8 = 199;
const EV_TEXT_DEF_PLUGIN: u8 = 201;
const EV_TEXT_PLUGIN: u8 = 203;
const EV_TEXT_GENRE: u8 = 206;
const EV_TEXT_AUTHOR: u8 = 207;
const EV_PATTERN_NOTES: u8 = 224;

#[derive(Debug, Default, Clone)]
#[allow(dead_code)] // полная модель проекта; часть полей — метаданные
pub struct FlpChannel {
    pub index: i32,
    pub kind: String,
    pub name: String,
    pub sample_path: String,
    pub plugin: String,
    pub is_empty_sampler: bool,
}

#[derive(Debug, Default, Clone)]
#[allow(dead_code)] // полная модель проекта; часть полей — метаданные
pub struct FlpNote {
    pub position: u32,
    pub length: u32,
    pub rack_chan: u16,
    pub key: u8,
    pub velocity: u8,
    pub pattern_index: i32,
    pub pattern_name: String,
}

#[derive(Debug, Default, Clone)]
#[allow(dead_code)] // полная модель проекта; часть полей — метаданные
pub struct FlpProject {
    pub name: String,
    pub ppq: i32,
    pub bpm: f64,
    pub title: String,
    pub artist: String,
    pub flp_version: String,
    pub sample_paths: Vec<String>,
    pub plugins: Vec<String>,
    pub channels: Vec<FlpChannel>,
    pub notes: Vec<FlpNote>,
    pub tags: Vec<String>,
}

struct Cursor<'a> {
    data: &'a [u8],
    pos: usize,
}

impl<'a> Cursor<'a> {
    fn new(data: &'a [u8]) -> Self {
        Self { data, pos: 0 }
    }
    fn remaining(&self) -> usize {
        self.data.len().saturating_sub(self.pos)
    }
    fn u8(&mut self) -> Option<u8> {
        let b = *self.data.get(self.pos)?;
        self.pos += 1;
        Some(b)
    }
    fn u16(&mut self) -> Option<u16> {
        if self.remaining() < 2 {
            return None;
        }
        let v = u16::from_le_bytes([self.data[self.pos], self.data[self.pos + 1]]);
        self.pos += 2;
        Some(v)
    }
    fn u32(&mut self) -> Option<u32> {
        if self.remaining() < 4 {
            return None;
        }
        let v = u32::from_le_bytes([
            self.data[self.pos],
            self.data[self.pos + 1],
            self.data[self.pos + 2],
            self.data[self.pos + 3],
        ]);
        self.pos += 4;
        Some(v)
    }
    fn take(&mut self, n: usize) -> Option<&'a [u8]> {
        if self.remaining() < n {
            return None;
        }
        let s = &self.data[self.pos..self.pos + n];
        self.pos += n;
        Some(s)
    }
    /// FL 7-bit little-endian varint.
    fn varlen(&mut self) -> Option<usize> {
        let (mut value, mut shift) = (0usize, 0u32);
        loop {
            let b = self.u8()?;
            value |= ((b & 0x7F) as usize) << shift;
            if b & 0x80 == 0 {
                return Some(value);
            }
            shift += 7;
            if shift > 28 {
                return Some(value);
            }
        }
    }
}

/// Парсит .flp из сырых байт. `base_name` — имя файла (для имени проекта).
pub fn parse_flp(raw: &[u8], base_name: &str) -> Option<FlpProject> {
    if raw.len() < 8 || &raw[0..4] != b"FLhd" {
        return None;
    }
    let hdr_len = u32::from_le_bytes([raw[4], raw[5], raw[6], raw[7]]) as usize;
    let pos = 8 + hdr_len;
    if pos + 8 > raw.len() {
        return None;
    }
    let ppq = if hdr_len >= 6 && raw.len() >= 14 {
        u16::from_le_bytes([raw[12], raw[13]])
    } else {
        0
    };
    if &raw[pos..pos + 4] != b"FLdt" {
        return None;
    }
    let data_len = u32::from_le_bytes([raw[pos + 4], raw[pos + 5], raw[pos + 6], raw[pos + 7]]) as usize;
    let start = pos + 8;
    let end = (start + data_len).min(raw.len());
    let data = &raw[start..end.max(start)];

    let mut proj = FlpProject {
        name: strip_ext(base_name).to_string(),
        ppq: ppq as i32,
        ..Default::default()
    };
    decode_events(data, &mut proj);

    if !proj.title.is_empty() {
        proj.name = proj.title.clone();
    } else {
        proj.name = strip_archive_prefix(&proj.name).to_string();
    }
    dedupe(&mut proj.sample_paths);
    dedupe(&mut proj.plugins);
    Some(proj)
}

fn decode_events(data: &[u8], proj: &mut FlpProject) {
    let mut r = Cursor::new(data);
    let mut cur: Option<FlpChannel> = None;
    let mut fine_tempo: u32 = 0;
    let mut legacy_tempo: u16 = 0;
    let mut current_pattern: i32 = 0;
    let mut patterns: std::collections::HashMap<i32, String> = std::collections::HashMap::new();
    let mut pending_notes: Vec<FlpNote> = Vec::new();

    fn flush(cur: &mut Option<FlpChannel>, proj: &mut FlpProject) {
        if let Some(mut c) = cur.take() {
            c.is_empty_sampler = c.plugin.is_empty()
                && c.sample_path.is_empty()
                && (c.kind == "sampler" || c.kind == "channel");
            proj.channels.push(c);
        }
    }

    while r.remaining() > 0 {
        let Some(id) = r.u8() else { break };
        if id < 64 {
            let Some(b) = r.u8() else { break };
            if id == EV_CHAN_TYPE {
                if let Some(c) = cur.as_mut() {
                    c.kind = channel_kind(b).to_string();
                }
            }
        } else if id < 128 {
            let Some(w) = r.u16() else { break };
            match id {
                EV_NEW_CHAN => {
                    flush(&mut cur, proj);
                    cur = Some(FlpChannel { index: w as i32, kind: "channel".into(), ..Default::default() });
                }
                EV_NEW_PATTERN => current_pattern = w as i32,
                EV_TEMPO_LEGACY => legacy_tempo = w,
                _ => {}
            }
        } else if id < 192 {
            let Some(d) = r.u32() else { break };
            if (id == EV_FINE_TEMPO || id == EV_FINE_TEMPO_ALT)
                && fine_tempo == 0
                && (10_000..=600_000).contains(&d)
            {
                fine_tempo = d;
            }
        } else {
            let Some(n) = r.varlen() else { break };
            let Some(buf) = r.take(n) else { break };
            match id {
                EV_PATTERN_NAME => {
                    patterns.insert(current_pattern, decode_text(buf));
                }
                EV_PATTERN_NOTES => {
                    pending_notes.extend(parse_pattern_notes(buf, current_pattern));
                }
                _ => apply_text(id, buf, proj, &mut cur),
            }
        }
    }
    flush(&mut cur, proj);

    for note in &mut pending_notes {
        if let Some(name) = patterns.get(&note.pattern_index) {
            if !name.is_empty() {
                note.pattern_name = name.clone();
            }
        }
        if note.pattern_name.is_empty() {
            note.pattern_name = format!("Pattern {}", note.pattern_index + 1);
        }
    }
    proj.notes = pending_notes;

    if fine_tempo > 0 {
        proj.bpm = fine_tempo as f64 / 1000.0;
    } else if legacy_tempo > 0 {
        proj.bpm = legacy_tempo as f64;
    }
}

fn parse_pattern_notes(buf: &[u8], pattern_idx: i32) -> Vec<FlpNote> {
    if buf.is_empty() {
        return Vec::new();
    }
    let rec_size = if buf.len() % 24 == 0 {
        24
    } else if buf.len() % 20 == 0 {
        20
    } else {
        return Vec::new();
    };
    let n = buf.len() / rec_size;
    let mut notes = Vec::with_capacity(n);
    for i in 0..n {
        let rec = &buf[i * rec_size..i * rec_size + rec_size];
        let position = u32::from_le_bytes([rec[0], rec[1], rec[2], rec[3]]);
        let rack_chan = u16::from_le_bytes([rec[6], rec[7]]);
        let length = u32::from_le_bytes([rec[8], rec[9], rec[10], rec[11]]);
        let key_word = u32::from_le_bytes([rec[12], rec[13], rec[14], rec[15]]);
        let key = (key_word & 0x7F) as u8;
        let mut velocity: u8 = 100;
        if rec_size >= 21 {
            velocity = rec[20];
            if velocity == 0 {
                velocity = 100;
            }
        }
        notes.push(FlpNote {
            position,
            length,
            rack_chan,
            key,
            velocity,
            pattern_index: pattern_idx,
            pattern_name: String::new(),
        });
    }
    notes
}

fn apply_text(id: u8, buf: &[u8], proj: &mut FlpProject, cur: &mut Option<FlpChannel>) {
    match id {
        EV_VERSION => proj.flp_version = String::from_utf8_lossy(buf).trim_end_matches('\0').to_string(),
        EV_TEXT_TITLE => proj.title = decode_text(buf),
        EV_TEXT_AUTHOR => proj.artist = decode_text(buf),
        EV_TEXT_GENRE => {
            let g = decode_text(buf);
            if !g.is_empty() {
                add_unique(&mut proj.tags, g.to_ascii_lowercase());
            }
        }
        EV_TEXT_CHAN_NAME => {
            if let Some(c) = cur.as_mut() {
                c.name = decode_text(buf);
            }
        }
        EV_TEXT_SAMPLE_PATH => {
            let path = decode_text(buf);
            if path.is_empty() {
                return;
            }
            proj.sample_paths.push(path.clone());
            if let Some(c) = cur.as_mut() {
                c.sample_path = path;
                if c.kind == "channel" {
                    c.kind = "sampler".into();
                }
            }
        }
        EV_TEXT_PLUGIN | EV_TEXT_DEF_PLUGIN => {
            let name = decode_text(buf);
            if name.is_empty() {
                return;
            }
            proj.plugins.push(name.clone());
            if let Some(c) = cur.as_mut() {
                if c.plugin.is_empty() {
                    c.plugin = name;
                    if c.kind == "channel" {
                        c.kind = "plugin".into();
                    }
                }
            }
        }
        _ => {}
    }
}

fn channel_kind(b: u8) -> &'static str {
    match b {
        0 => "sampler",
        2 => "plugin",
        3 => "layer",
        4 => "automation",
        _ => "channel",
    }
}

fn decode_text(b: &[u8]) -> String {
    if looks_utf16le(b) {
        let u: Vec<u16> = b.chunks_exact(2).map(|c| u16::from_le_bytes([c[0], c[1]])).collect();
        String::from_utf16_lossy(&u).trim_end_matches('\0').to_string()
    } else {
        String::from_utf8_lossy(b).trim_end_matches('\0').to_string()
    }
}

fn looks_utf16le(b: &[u8]) -> bool {
    if b.len() < 2 || b.len() % 2 != 0 {
        return false;
    }
    let (mut pairs, mut zeros) = (0, 0);
    let mut i = 0;
    while i + 1 < b.len() {
        pairs += 1;
        if b[i + 1] == 0 {
            zeros += 1;
        }
        i += 2;
    }
    pairs > 0 && zeros * 2 >= pairs
}

fn strip_ext(name: &str) -> &str {
    match name.rfind('.') {
        Some(i) if i > 0 => &name[..i],
        _ => name,
    }
}

fn strip_archive_prefix(name: &str) -> &str {
    let bytes = name.as_bytes();
    let mut i = 0;
    while i < bytes.len() && bytes[i].is_ascii_digit() {
        i += 1;
    }
    if i > 0 && i < bytes.len() && bytes[i] == b'_' {
        &name[i + 1..]
    } else {
        name
    }
}

fn dedupe(v: &mut Vec<String>) {
    let mut seen = std::collections::HashSet::new();
    v.retain(|s| seen.insert(s.clone()));
}

fn add_unique(v: &mut Vec<String>, s: String) {
    if !v.contains(&s) {
        v.push(s);
    }
}

/// Найти .flp в папке, распарсить, вернуть существующие на диске пути сэмплов,
/// на которые ссылаются проекты (харвест сэмплов из FL-проектов).
pub fn harvest_flp_sample_paths(dir: &str) -> Vec<String> {
    use std::path::Path;
    let mut flps = Vec::new();
    find_flps(Path::new(dir), &mut flps, 0);
    let mut out = Vec::new();
    for flp in &flps {
        let Ok(raw) = std::fs::read(flp) else { continue };
        let base = Path::new(flp).file_name().and_then(|n| n.to_str()).unwrap_or("project.flp");
        if let Some(proj) = parse_flp(&raw, base) {
            for sp in proj.sample_paths {
                if Path::new(&sp).is_file() {
                    out.push(sp);
                }
            }
        }
    }
    out
}

fn find_flps(dir: &std::path::Path, out: &mut Vec<String>, depth: usize) {
    if depth > 32 {
        return;
    }
    let Ok(rd) = std::fs::read_dir(dir) else { return };
    for e in rd.flatten() {
        let Ok(ft) = e.file_type() else { continue };
        let p = e.path();
        if ft.is_dir() {
            find_flps(&p, out, depth + 1);
        } else if p.extension().and_then(|x| x.to_str()).is_some_and(|x| x.eq_ignore_ascii_case("flp")) {
            out.push(p.to_string_lossy().into_owned());
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Port of flpBuilder from parser_test.go.
    #[derive(Default)]
    struct Builder {
        ev: Vec<u8>,
    }
    impl Builder {
        fn byte_ev(&mut self, id: u8, v: u8) {
            self.ev.push(id);
            self.ev.push(v);
        }
        fn word_ev(&mut self, id: u8, v: u16) {
            self.ev.push(id);
            self.ev.extend_from_slice(&v.to_le_bytes());
        }
        fn dword_ev(&mut self, id: u8, v: u32) {
            self.ev.push(id);
            self.ev.extend_from_slice(&v.to_le_bytes());
        }
        fn varlen(&mut self, mut n: usize) {
            loop {
                let mut c = (n & 0x7F) as u8;
                n >>= 7;
                if n != 0 {
                    c |= 0x80;
                }
                self.ev.push(c);
                if n == 0 {
                    break;
                }
            }
        }
        fn text_ascii(&mut self, id: u8, s: &str) {
            self.ev.push(id);
            let mut payload = s.as_bytes().to_vec();
            payload.push(0);
            self.varlen(payload.len());
            self.ev.extend_from_slice(&payload);
        }
        fn text_utf16(&mut self, id: u8, s: &str) {
            self.ev.push(id);
            let mut payload = Vec::new();
            for u in s.encode_utf16() {
                payload.extend_from_slice(&u.to_le_bytes());
            }
            payload.extend_from_slice(&[0, 0]);
            self.varlen(payload.len());
            self.ev.extend_from_slice(&payload);
        }
        fn bytes(&self) -> Vec<u8> {
            let mut out = Vec::new();
            out.extend_from_slice(b"FLhd");
            out.extend_from_slice(&6u32.to_le_bytes());
            out.extend_from_slice(&0u16.to_le_bytes()); // format
            out.extend_from_slice(&4u16.to_le_bytes()); // nChannels
            out.extend_from_slice(&96u16.to_le_bytes()); // ppq
            out.extend_from_slice(b"FLdt");
            out.extend_from_slice(&(self.ev.len() as u32).to_le_bytes());
            out.extend_from_slice(&self.ev);
            out
        }
    }

    #[test]
    fn parse_synthetic_project() {
        let mut b = Builder::default();
        b.text_ascii(EV_VERSION, "20.8.3.2304");
        b.dword_ev(EV_FINE_TEMPO, 140_000);
        b.text_utf16(EV_TEXT_TITLE, "Midnight Drive");
        b.text_utf16(EV_TEXT_AUTHOR, "Producer X");
        b.text_utf16(EV_TEXT_GENRE, "Trap");

        b.word_ev(EV_NEW_CHAN, 0);
        b.byte_ev(EV_CHAN_TYPE, 0);
        b.text_utf16(EV_TEXT_CHAN_NAME, "808 Sub");
        b.text_utf16(EV_TEXT_SAMPLE_PATH, r"C:\Samples\808\deep_808.wav");

        b.word_ev(EV_NEW_CHAN, 1);
        b.byte_ev(EV_CHAN_TYPE, 2);
        b.text_utf16(EV_TEXT_CHAN_NAME, "Lead");
        b.text_utf16(EV_TEXT_PLUGIN, "Serum");

        let proj = parse_flp(&b.bytes(), "test.flp").expect("parsed");
        assert_eq!(proj.ppq, 96);
        assert_eq!(proj.flp_version, "20.8.3.2304");
        assert_eq!(proj.bpm, 140.0);
        assert_eq!(proj.title, "Midnight Drive");
        assert_eq!(proj.artist, "Producer X");
        assert_eq!(proj.name, "Midnight Drive"); // title wins
        assert_eq!(proj.sample_paths, vec![r"C:\Samples\808\deep_808.wav".to_string()]);
        assert!(proj.plugins.contains(&"Serum".to_string()));
        assert_eq!(proj.channels.len(), 2);
        assert_eq!(proj.channels[0].kind, "sampler");
        assert_eq!(proj.channels[0].sample_path, r"C:\Samples\808\deep_808.wav");
        assert_eq!(proj.channels[1].kind, "plugin");
        assert!(proj.tags.contains(&"trap".to_string()));
    }

    #[test]
    fn rejects_non_flp() {
        assert!(parse_flp(b"not an flp file", "x.flp").is_none());
    }

    // Реальная проверка на .flp пользователя:
    //   FLAPP_FLP_DIR="E:\BEATS\FLPs & ZIPs" cargo test -p flapp-app real_flp_smoke -- --ignored --nocapture
    #[test]
    #[ignore]
    fn real_flp_smoke() {
        let Ok(dir) = std::env::var("FLAPP_FLP_DIR") else {
            eprintln!("set FLAPP_FLP_DIR");
            return;
        };
        let mut flps = Vec::new();
        super::find_flps(std::path::Path::new(&dir), &mut flps, 0);
        println!("found {} .flp files", flps.len());
        let (mut parsed, mut with_bpm, mut sample_refs, mut existing, mut with_notes) = (0, 0, 0usize, 0, 0);
        for f in &flps {
            let Ok(raw) = std::fs::read(f) else { continue };
            let base = std::path::Path::new(f).file_name().and_then(|n| n.to_str()).unwrap_or("p.flp");
            if let Some(p) = parse_flp(&raw, base) {
                parsed += 1;
                if p.bpm > 0.0 {
                    with_bpm += 1;
                }
                if !p.notes.is_empty() {
                    with_notes += 1;
                }
                sample_refs += p.sample_paths.len();
                for sp in &p.sample_paths {
                    if std::path::Path::new(sp).is_file() {
                        existing += 1;
                    }
                }
                if parsed <= 6 {
                    println!(
                        "  {base} → bpm {:.1}  samples {}  notes {}  title {:?}",
                        p.bpm,
                        p.sample_paths.len(),
                        p.notes.len(),
                        p.title
                    );
                }
            }
        }
        println!(
            "parsed {parsed}/{}  with_bpm {with_bpm}  with_notes {with_notes}  sample_refs {sample_refs}  existing_on_disk {existing}",
            flps.len()
        );
    }

    #[test]
    fn parses_pattern_notes() {
        let mut b = Builder::default();
        b.word_ev(EV_NEW_PATTERN, 0);
        b.text_utf16(EV_PATTERN_NAME, "Verse");
        // One 24-byte note record: pos=0, rackChan=0, len=48, key=60 (C5), vel=100.
        let mut rec = vec![0u8; 24];
        rec[8..12].copy_from_slice(&48u32.to_le_bytes());
        rec[12..16].copy_from_slice(&60u32.to_le_bytes());
        rec[20] = 100;
        b.ev.push(EV_PATTERN_NOTES);
        b.varlen(rec.len());
        b.ev.extend_from_slice(&rec);

        let proj = parse_flp(&b.bytes(), "n.flp").unwrap();
        assert_eq!(proj.notes.len(), 1);
        assert_eq!(proj.notes[0].key, 60);
        assert_eq!(proj.notes[0].length, 48);
        assert_eq!(proj.notes[0].velocity, 100);
        assert_eq!(proj.notes[0].pattern_name, "Verse");
    }
}
