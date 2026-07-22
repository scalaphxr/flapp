// Экспорт пака: выбранные сэмплы → ZIP, разложенные по папкам категорий
// («808/», «Drum Loop/» …). Порт идеи usecase/packbuilder.go. Движок; выбор
// файлов — UI-слой.

use std::collections::HashSet;
use std::fs::File;
use std::io;
use std::path::Path;

use zip::write::SimpleFileOptions;

use crate::classify::Category;

/// Пишет ZIP: каждый (путь, категория) → `<Категория>/<имя файла>`. Коллизии
/// имён внутри категории разводятся суффиксом « (n)». Возвращает число файлов.
pub fn export_pack_zip(items: &[(String, Category)], dest: &Path) -> io::Result<usize> {
    let f = File::create(dest)?;
    let mut zw = zip::ZipWriter::new(f);
    let opt = SimpleFileOptions::default();
    let mut used: HashSet<String> = HashSet::new();
    let mut count = 0;

    for (path, cat) in items {
        let base = Path::new(path)
            .file_name()
            .and_then(|n| n.to_str())
            .unwrap_or("sample");
        let entry = unique_entry(cat.label(), base, &mut used);

        let mut src = match File::open(path) {
            Ok(f) => f,
            Err(_) => continue, // пропускаем недоступные, не роняем экспорт
        };
        zw.start_file(&entry, opt)?;
        io::copy(&mut src, &mut zw)?;
        count += 1;
    }
    zw.finish()?;
    Ok(count)
}

fn unique_entry(folder: &str, base: &str, used: &mut HashSet<String>) -> String {
    let mut candidate = format!("{folder}/{base}");
    if !used.contains(&candidate) {
        used.insert(candidate.clone());
        return candidate;
    }
    let (stem, ext) = match base.rsplit_once('.') {
        Some((s, e)) if !s.is_empty() => (s, format!(".{e}")),
        _ => (base, String::new()),
    };
    let mut n = 1;
    loop {
        candidate = format!("{folder}/{stem} ({n}){ext}");
        if !used.contains(&candidate) {
            used.insert(candidate.clone());
            return candidate;
        }
        n += 1;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Read;

    #[test]
    fn exports_zip_grouped_by_category() {
        let tmp = std::env::temp_dir().join(format!("flapp-exp-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();
        let a = tmp.join("a.wav");
        let b = tmp.join("b.wav");
        std::fs::write(&a, b"aaa").unwrap();
        std::fs::write(&b, b"bbb").unwrap();

        let items = vec![
            (a.to_string_lossy().into_owned(), Category::Kick),
            (b.to_string_lossy().into_owned(), Category::C808),
            (a.to_string_lossy().into_owned(), Category::Kick), // collision → " (1)"
        ];
        let dest = tmp.join("pack.zip");
        let n = export_pack_zip(&items, &dest).unwrap();
        assert_eq!(n, 3);

        // Read back entry names.
        let f = File::open(&dest).unwrap();
        let mut zip = zip::ZipArchive::new(f).unwrap();
        let mut names: Vec<String> = (0..zip.len())
            .map(|i| zip.by_index(i).unwrap().name().to_string())
            .collect();
        names.sort();
        assert_eq!(names, vec!["808/b.wav", "Kick/a (1).wav", "Kick/a.wav"]);

        // Content intact.
        let mut e = zip.by_name("808/b.wav").unwrap();
        let mut s = String::new();
        e.read_to_string(&mut s).unwrap();
        assert_eq!(s, "bbb");
        drop(e);

        let _ = std::fs::remove_dir_all(&tmp);
    }
}
