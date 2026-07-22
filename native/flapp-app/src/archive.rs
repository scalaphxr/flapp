// Харвест из архивов: извлечение аудио из .zip во время скана. Внутренняя
// структура папок сохраняется (её использует классификатор по пути). Защита от
// zip-бомб (лимит на запись) и path-traversal (enclosed_name). RAR/7z — позже.

use std::fs;
use std::hash::{Hash, Hasher};
use std::io;
use std::path::Path;
use std::time::UNIX_EPOCH;

const MAX_ENTRY_BYTES: u64 = 512 * 1024 * 1024; // защита от бомбы: 512 МБ/запись
const AUDIO_EXTS: &[&str] = &["wav", "mp3", "flac", "ogg", "oga", "aiff", "aif", "m4a", "aac"];

fn is_audio(name: &str) -> bool {
    Path::new(name)
        .extension()
        .and_then(|e| e.to_str())
        .map(|e| AUDIO_EXTS.contains(&e.to_ascii_lowercase().as_str()))
        .unwrap_or(false)
}

const ARCHIVE_EXTS: &[&str] = &["zip", "7z"];

/// Рекурсивно найти архивы (.zip/.7z) в папке (ограниченная глубина, без symlink).
pub fn find_archives(dir: &str) -> Vec<String> {
    let mut out = Vec::new();
    find_archives_rec(Path::new(dir), &mut out, 0);
    out
}

fn find_archives_rec(dir: &Path, out: &mut Vec<String>, depth: usize) {
    if depth > 32 {
        return;
    }
    let Ok(rd) = fs::read_dir(dir) else { return };
    for e in rd.flatten() {
        let Ok(ft) = e.file_type() else { continue };
        let p = e.path();
        if ft.is_dir() {
            find_archives_rec(&p, out, depth + 1);
        } else if p
            .extension()
            .and_then(|x| x.to_str())
            .is_some_and(|x| ARCHIVE_EXTS.iter().any(|a| x.eq_ignore_ascii_case(a)))
        {
            out.push(p.to_string_lossy().into_owned());
        }
    }
}

/// Извлечь аудио из архива (диспетчер по расширению: zip/7z). RAR — позже
/// (нужна C-библиотека unrar).
pub fn extract_archive_audio(path: &str, dest_root: &Path) -> Vec<String> {
    match Path::new(path).extension().and_then(|e| e.to_str()) {
        Some(e) if e.eq_ignore_ascii_case("7z") => extract_7z_audio(path, dest_root),
        _ => extract_zip_audio(path, dest_root),
    }
}

/// Безопасное соединение base + относительный путь из архива: отбрасывает
/// абсолютные пути и компоненты "..".
fn safe_join(base: &Path, name: &str) -> Option<std::path::PathBuf> {
    let rel = Path::new(name);
    let mut out = base.to_path_buf();
    for comp in rel.components() {
        use std::path::Component::*;
        match comp {
            Normal(c) => out.push(c),
            CurDir => {}
            _ => return None, // RootDir/ParentDir/Prefix — небезопасно
        }
    }
    Some(out)
}

/// Извлечь аудио из .7z (pure-Rust sevenz-rust2).
fn extract_7z_audio(archive_path: &str, dest_root: &Path) -> Vec<String> {
    use sevenz_rust2::{ArchiveReader, Password};
    let dir = dest_root.join(extract_key(archive_path));
    let mut out = Vec::new();
    if dir.is_dir() {
        list_audio(&dir, &mut out);
        if !out.is_empty() {
            return out;
        }
    }
    let Ok(mut reader) = ArchiveReader::open(archive_path, Password::empty()) else { return out };
    let _ = fs::create_dir_all(&dir);

    let collected = std::cell::RefCell::new(Vec::new());
    let _ = reader.for_each_entries(|entry, rd| {
        if entry.is_directory() || !entry.has_stream() || entry.size() > MAX_ENTRY_BYTES || !is_audio(entry.name()) {
            return Ok(true);
        }
        if let Some(dest) = safe_join(&dir, entry.name()) {
            if let Some(parent) = dest.parent() {
                let _ = fs::create_dir_all(parent);
            }
            if let Ok(mut f) = fs::File::create(&dest) {
                if io::copy(rd, &mut f).is_ok() {
                    collected.borrow_mut().push(dest.to_string_lossy().into_owned());
                }
            }
        }
        Ok(true)
    });
    collected.into_inner()
}

/// Ключ извлечения = путь+mtime+размер, чтобы не переизвлекать неизменный zip.
fn extract_key(zip_path: &str) -> String {
    let mut h = std::collections::hash_map::DefaultHasher::new();
    zip_path.hash(&mut h);
    if let Ok(m) = fs::metadata(zip_path) {
        m.len().hash(&mut h);
        if let Ok(mt) = m.modified() {
            if let Ok(d) = mt.duration_since(UNIX_EPOCH) {
                d.as_secs().hash(&mut h);
            }
        }
    }
    format!("{:016x}", h.finish())
}

fn list_audio(dir: &Path, out: &mut Vec<String>) {
    let Ok(rd) = fs::read_dir(dir) else { return };
    for e in rd.flatten() {
        let p = e.path();
        if p.is_dir() {
            list_audio(&p, out);
        } else if p.file_name().and_then(|n| n.to_str()).is_some_and(is_audio) {
            out.push(p.to_string_lossy().into_owned());
        }
    }
}

/// Извлечь аудио-записи из zip в `dest_root/<key>/`, сохранив структуру папок.
/// Возвращает пути извлечённых файлов. Уже извлечённый (по ключу) — переиспользует.
pub fn extract_zip_audio(zip_path: &str, dest_root: &Path) -> Vec<String> {
    let dir = dest_root.join(extract_key(zip_path));
    let mut out = Vec::new();

    // Уже извлечено — переиспользуем.
    if dir.is_dir() {
        list_audio(&dir, &mut out);
        if !out.is_empty() {
            return out;
        }
    }

    let Ok(file) = fs::File::open(zip_path) else { return out };
    let Ok(mut zip) = zip::ZipArchive::new(file) else { return out };
    let _ = fs::create_dir_all(&dir);

    for i in 0..zip.len() {
        let Ok(mut entry) = zip.by_index(i) else { continue };
        if !entry.is_file() || entry.size() > MAX_ENTRY_BYTES || !is_audio(entry.name()) {
            continue;
        }
        // enclosed_name отбрасывает записи с path-traversal ("../…").
        let Some(rel) = entry.enclosed_name() else { continue };
        let dest = dir.join(&rel);
        if let Some(parent) = dest.parent() {
            let _ = fs::create_dir_all(parent);
        }
        if let Ok(mut out_f) = fs::File::create(&dest) {
            if io::copy(&mut entry, &mut out_f).is_ok() {
                out.push(dest.to_string_lossy().into_owned());
            }
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use zip::write::SimpleFileOptions;

    #[test]
    fn safe_join_blocks_traversal() {
        let base = Path::new("C:/tmp/extract");
        assert!(safe_join(base, "808/deep.wav").is_some());
        assert!(safe_join(base, "../../etc/passwd").is_none());
        assert!(safe_join(base, "/abs/evil").is_none());
        let ok = safe_join(base, "a/b/c.wav").unwrap();
        assert!(ok.ends_with("c.wav"));
    }

    #[test]
    fn extracts_only_audio_preserving_folders() {
        let tmp = std::env::temp_dir().join(format!("flapp-arc-{}", std::process::id()));
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();
        let zip_path = tmp.join("pack.zip");

        // Build a zip: one audio in a subfolder, one non-audio.
        {
            let f = fs::File::create(&zip_path).unwrap();
            let mut zw = zip::ZipWriter::new(f);
            let opt = SimpleFileOptions::default();
            zw.start_file("808 Kicks/deep.wav", opt).unwrap();
            zw.write_all(b"RIFFfake").unwrap();
            zw.start_file("readme.txt", opt).unwrap();
            zw.write_all(b"hello").unwrap();
            zw.finish().unwrap();
        }

        let dest = tmp.join("extracted");
        let got = extract_zip_audio(&zip_path.to_string_lossy(), &dest);
        assert_eq!(got.len(), 1, "only the .wav should extract");
        let p = &got[0];
        assert!(p.ends_with("deep.wav"));
        assert!(p.replace('\\', "/").contains("808 Kicks/"), "folder preserved: {p}");
        assert!(Path::new(p).exists());

        // Second call reuses (same key) and returns the same file.
        let again = extract_zip_audio(&zip_path.to_string_lossy(), &dest);
        assert_eq!(again.len(), 1);

        let _ = fs::remove_dir_all(&tmp);
    }
}
