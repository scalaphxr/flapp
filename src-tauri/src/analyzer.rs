// Тонкая Tauri-обёртка над крейтом `flapp-dsp`: только команды вкладки «Плеер»
// и стриминг прогресса через события. Вся тяжёлая DSP-логика (зонд контейнера,
// один потоковый проход декодера, BPM, тональность, пики волны, двухуровневый
// кэш) вынесена в `flapp-dsp` — единый источник правды, который переиспользует
// нативный рерайт (native/). Здесь остаётся ровно то, что зависит от Tauri.

use std::fs;
use std::path::Path;
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::time::SystemTime;

use serde::Serialize;
use tauri::{AppHandle, Emitter, State};

use flapp_dsp::{analyze_one, encode_wav, probe_quick, AudioMeta};
// Re-export: watcher.rs зовёт `crate::analyzer::scan_dir_recursive`, а lib.rs —
// `analyzer::AnalyzerCache`.
pub use flapp_dsp::{scan_dir_recursive, AnalyzerCache};

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AnalysisProgress {
    pub path: String,
    pub done: usize,
    pub total: usize,
    pub meta: Option<AudioMeta>,
}

/// Возвращает Unix-время создания файлов без аудио-анализа — почти мгновенно.
#[tauri::command]
pub async fn player_get_dates(paths: Vec<String>) -> Vec<(String, Option<u64>)> {
    tauri::async_runtime::spawn_blocking(move || {
        paths.into_iter().map(|path| {
            let ts = fs::metadata(&path).ok()
                .and_then(|m| m.created().ok().or_else(|| m.modified().ok()))
                .and_then(|t| t.duration_since(SystemTime::UNIX_EPOCH).ok())
                .map(|d| d.as_secs());
            (path, ts)
        }).collect()
    })
    .await
    .unwrap_or_default()
}

/// Рекурсивно обходит папку, возвращает пути аудиофайлов.
#[tauri::command]
pub async fn player_scan_folder(dir: String) -> Result<Vec<String>, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let mut results = Vec::new();
        scan_dir_recursive(&dir, &mut results);
        results.sort();
        results
    })
    .await
    .map_err(|e| e.to_string())
}

/// Анализирует один файл (с кэшем).
#[tauri::command]
pub async fn player_analyze_file(
    path: String,
    cache: State<'_, AnalyzerCache>,
) -> Result<AudioMeta, String> {
    let cache = cache.inner().clone();
    tauri::async_runtime::spawn_blocking(move || {
        if let Some(cached) = cache.get(&path) {
            return cached;
        }
        let meta = analyze_one(&path);
        cache.set(&path, meta.clone());
        meta
    })
    .await
    .map_err(|e| e.to_string())
}

/// Анализирует батч файлов параллельно через rayon; события стримятся по мере
/// готовности. Две стадии: мгновенные partial-метаданные из заголовков, затем
/// полный DSP-анализ.
#[tauri::command]
pub async fn player_analyze_batch(
    paths: Vec<String>,
    app: AppHandle,
    cache: State<'_, AnalyzerCache>,
) -> Result<(), String> {
    use rayon::prelude::*;

    let total = paths.len();
    let done  = Arc::new(AtomicUsize::new(0));

    let cache_clone = cache.inner().clone();
    let app_clone   = app.clone();
    let done_clone  = done.clone();

    tauri::async_runtime::spawn_blocking(move || {
        // Закэшированные (память или диск) — отдаём сразу, без декодирования.
        let mut uncached: Vec<String> = Vec::new();
        for path in &paths {
            if let Some(meta) = cache_clone.get(path) {
                let n = done_clone.fetch_add(1, Ordering::Relaxed) + 1;
                let _ = app_clone.emit("player-analysis-progress", AnalysisProgress {
                    path: path.clone(), done: n, total, meta: Some(meta),
                });
            } else {
                uncached.push(path.clone());
            }
        }
        if uncached.is_empty() { return; }

        // Стадия 1: мгновенные partial-метаданные (заголовок + ФС) — таблица
        // заполняется форматом/длительностью сразу, не дожидаясь DSP.
        uncached.par_iter().for_each(|path| {
            let quick = probe_quick(path);
            let _ = app_clone.emit("player-analysis-progress", AnalysisProgress {
                path: path.clone(),
                done: done_clone.load(Ordering::Relaxed),
                total,
                meta: Some(quick),
            });
        });

        // Стадия 2: полный анализ (один проход декодера + STFT) параллельно.
        uncached.par_iter().for_each(|path| {
            let meta = analyze_one(path);
            cache_clone.set(path, meta.clone());
            let n = done_clone.fetch_add(1, Ordering::Relaxed) + 1;
            let _ = app_clone.emit("player-analysis-progress", AnalysisProgress {
                path: path.clone(), done: n, total, meta: Some(meta),
            });
        });
    })
    .await
    .map_err(|e| e.to_string())?;

    Ok(())
}

/// Декодирует аудиофайл в WAV-байты (для форматов, не поддерживаемых браузером,
/// например AIFF). Возвращает бинарный ответ — без JSON-сериализации массива.
#[tauri::command]
pub async fn player_decode_to_wav(path: String) -> Result<tauri::ipc::Response, String> {
    let bytes = tauri::async_runtime::spawn_blocking(move || {
        use symphonia::core::audio::SampleBuffer;
        use symphonia::core::codecs::DecoderOptions;
        use symphonia::core::formats::FormatOptions;
        use symphonia::core::io::MediaSourceStream;
        use symphonia::core::meta::MetadataOptions;
        use symphonia::core::probe::Hint;

        let file = fs::File::open(&path).map_err(|e| e.to_string())?;
        let mss = MediaSourceStream::new(Box::new(file), Default::default());
        let mut hint = Hint::new();
        if let Some(ext) = Path::new(&path).extension().and_then(|e| e.to_str()) {
            hint.with_extension(ext);
        }
        let probed = symphonia::default::get_probe()
            .format(&hint, mss, &FormatOptions::default(), &MetadataOptions::default())
            .map_err(|e| e.to_string())?;
        let mut fmt = probed.format;
        let track = fmt.default_track().ok_or("no track")?;
        let sr = track.codec_params.sample_rate.unwrap_or(44100);
        let ch = track.codec_params.channels.map(|c| c.count() as u32).unwrap_or(2);
        let track_id = track.id;
        let mut decoder = symphonia::default::get_codecs()
            .make(&track.codec_params.clone(), &DecoderOptions::default())
            .map_err(|e| e.to_string())?;

        let mut interleaved: Vec<f32> = Vec::new();
        loop {
            let packet = match fmt.next_packet() { Ok(p) => p, Err(_) => break };
            if packet.track_id() != track_id { continue; }
            let decoded = match decoder.decode(&packet) { Ok(d) => d, Err(_) => continue };
            let mut buf: SampleBuffer<f32> =
                SampleBuffer::new(decoded.capacity() as u64, *decoded.spec());
            buf.copy_interleaved_ref(decoded);
            interleaved.extend_from_slice(buf.samples());
        }
        Ok::<Vec<u8>, String>(encode_wav(&interleaved, sr, ch))
    })
    .await
    .map_err(|e| e.to_string())??;
    Ok(tauri::ipc::Response::new(bytes))
}
