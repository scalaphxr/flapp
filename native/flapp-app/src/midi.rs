// Экспорт MIDI из .flp: ноты пианоролла → стандартный SMF (.mid). Порт ядра
// midi/multitrack.go (одно-трековый вариант, format 0). Тайминг в тиках PPQ.

use std::fs;
use std::io;
use std::path::Path;

use crate::flp::{parse_flp, FlpNote};

/// Записывает SMF format 0 (один трек) для набора нот. `ppq` — division, `bpm`
/// — темп-мета. Ноты: позиция/длина в тиках PPQ, key = MIDI-нота, velocity.
pub fn write_smf(track_name: &str, notes: &[FlpNote], ppq: u16, bpm: f64) -> Vec<u8> {
    let mut track: Vec<u8> = Vec::new();

    // Имя трека (meta 0x03).
    write_varlen(&mut track, 0);
    write_meta_text(&mut track, 0x03, track_name);

    // Темп (meta 0x51): микросекунды на четверть.
    let us_per_quarter = if bpm > 0.0 { (60_000_000.0 / bpm) as u32 } else { 500_000 };
    write_varlen(&mut track, 0);
    track.extend_from_slice(&[0xFF, 0x51, 0x03]);
    track.extend_from_slice(&us_per_quarter.to_be_bytes()[1..4]); // 3 байта BE

    // События note-on/off.
    struct Ev {
        tick: u32,
        status: u8,
        note: u8,
        vel: u8,
    }
    let mut evs: Vec<Ev> = Vec::with_capacity(notes.len() * 2);
    for n in notes {
        let key = n.key.min(127);
        let vel = if n.velocity == 0 { 100 } else { n.velocity };
        let end = if n.length == 0 { n.position + 1 } else { n.position + n.length };
        evs.push(Ev { tick: n.position, status: 0x90, note: key, vel });
        evs.push(Ev { tick: end, status: 0x80, note: key, vel: 0 });
    }
    // Стабильно по тику; при равенстве note-off (0x80) раньше note-on (0x90).
    evs.sort_by(|a, b| a.tick.cmp(&b.tick).then(a.status.cmp(&b.status)));

    let mut prev = 0u32;
    for e in &evs {
        write_varlen(&mut track, e.tick - prev);
        prev = e.tick;
        track.extend_from_slice(&[e.status, e.note, e.vel]);
    }

    // End of track.
    write_varlen(&mut track, 0);
    track.extend_from_slice(&[0xFF, 0x2F, 0x00]);

    // Сборка файла: MThd + MTrk.
    let mut out: Vec<u8> = Vec::with_capacity(track.len() + 22);
    out.extend_from_slice(b"MThd");
    out.extend_from_slice(&6u32.to_be_bytes());
    out.extend_from_slice(&0u16.to_be_bytes()); // format 0
    out.extend_from_slice(&1u16.to_be_bytes()); // 1 track
    out.extend_from_slice(&ppq.max(1).to_be_bytes()); // division
    out.extend_from_slice(b"MTrk");
    out.extend_from_slice(&(track.len() as u32).to_be_bytes());
    out.extend_from_slice(&track);
    out
}

fn write_varlen(out: &mut Vec<u8>, mut v: u32) {
    let mut buf = [0u8; 5];
    let mut i = 0;
    buf[i] = (v & 0x7F) as u8;
    i += 1;
    v >>= 7;
    while v > 0 {
        buf[i] = ((v & 0x7F) as u8) | 0x80;
        i += 1;
        v >>= 7;
    }
    for j in (0..i).rev() {
        out.push(buf[j]);
    }
}

fn write_meta_text(out: &mut Vec<u8>, meta_type: u8, text: &str) {
    let b = text.as_bytes();
    out.extend_from_slice(&[0xFF, meta_type]);
    write_varlen(out, b.len() as u32);
    out.extend_from_slice(b);
}

fn sanitize(name: &str) -> String {
    name.chars()
        .map(|c| if c.is_alphanumeric() || matches!(c, ' ' | '_' | '-') { c } else { '_' })
        .collect::<String>()
        .trim()
        .to_string()
}

/// Извлекает MIDI из .flp: один .mid на паттерн (с нотами) в `dest_dir`.
/// Возвращает пути записанных файлов.
pub fn export_flp_to_midi(flp_path: &str, dest_dir: &Path) -> io::Result<Vec<String>> {
    let raw = fs::read(flp_path)?;
    let base = Path::new(flp_path).file_name().and_then(|n| n.to_str()).unwrap_or("project.flp");
    let Some(proj) = parse_flp(&raw, base) else {
        return Ok(Vec::new());
    };
    if proj.notes.is_empty() {
        return Ok(Vec::new());
    }
    fs::create_dir_all(dest_dir)?;

    // Группируем по паттерну.
    let mut by_pattern: std::collections::BTreeMap<i32, Vec<FlpNote>> = Default::default();
    for n in &proj.notes {
        by_pattern.entry(n.pattern_index).or_default().push(n.clone());
    }

    let ppq = proj.ppq.clamp(1, 65535) as u16;
    let proj_name = sanitize(&proj.name);
    let mut out = Vec::new();
    for (_, notes) in by_pattern {
        let pat_name = notes
            .first()
            .map(|n| n.pattern_name.clone())
            .unwrap_or_default();
        let track_name = if pat_name.is_empty() { proj_name.clone() } else { pat_name.clone() };
        let bytes = write_smf(&track_name, &notes, ppq, proj.bpm);
        let fname = format!("{proj_name} - {}.mid", sanitize(&track_name));
        let dest = dest_dir.join(&fname);
        fs::write(&dest, &bytes)?;
        out.push(dest.to_string_lossy().into_owned());
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn note(pos: u32, len: u32, key: u8, vel: u8) -> FlpNote {
        FlpNote { position: pos, length: len, key, velocity: vel, ..Default::default() }
    }

    #[test]
    fn write_smf_produces_valid_header_and_notes() {
        let notes = vec![note(0, 96, 60, 100), note(96, 96, 64, 90)];
        let smf = write_smf("Verse", &notes, 96, 140.0);

        // MThd header.
        assert_eq!(&smf[0..4], b"MThd");
        assert_eq!(u32::from_be_bytes([smf[4], smf[5], smf[6], smf[7]]), 6);
        assert_eq!(u16::from_be_bytes([smf[8], smf[9]]), 0); // format 0
        assert_eq!(u16::from_be_bytes([smf[10], smf[11]]), 1); // 1 track
        assert_eq!(u16::from_be_bytes([smf[12], smf[13]]), 96); // division = ppq

        // MTrk present, length matches remaining bytes.
        assert_eq!(&smf[14..18], b"MTrk");
        let trk_len = u32::from_be_bytes([smf[18], smf[19], smf[20], smf[21]]) as usize;
        assert_eq!(smf.len(), 22 + trk_len);

        // Contains note-on (0x90) and tempo meta (FF 51 03).
        assert!(smf.windows(3).any(|w| w == [0xFF, 0x51, 0x03]));
        assert!(smf.windows(3).any(|w| w[0] == 0x90 && w[1] == 60 && w[2] == 100));
        // Ends with end-of-track meta.
        assert_eq!(&smf[smf.len() - 3..], &[0xFF, 0x2F, 0x00]);
    }

    #[test]
    fn write_varlen_encodes_multibyte() {
        let mut v = Vec::new();
        write_varlen(&mut v, 0);
        assert_eq!(v, vec![0x00]);
        v.clear();
        write_varlen(&mut v, 128);
        assert_eq!(v, vec![0x81, 0x00]); // standard MIDI varint
        v.clear();
        write_varlen(&mut v, 0x3FFF);
        assert_eq!(v, vec![0xFF, 0x7F]);
    }
}
