// Модуль аудио-аналайзера для вкладки «Плеер».
//
// Два потока работы:
//   1. Быстрый зонд (probe_container): формат, длительность, SR — из заголовка
//      контейнера без декодирования сэмплов.
//   2. DSP: пики (потоковый O(4096)-памятный проход) + BPM и тональность
//      (50 сек из середины трека на 11025 Гц, один общий STFT).
//
// Параллельный батч — rayon::par_iter; каждый файл эмитирует событие по готовности.

use std::collections::HashMap;
use std::fs;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::time::SystemTime;

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Emitter, State};

// ── Типы ─────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AudioMeta {
    pub path: String,
    pub name: String,
    pub format: String,
    pub duration_s: f64,
    pub sample_rate: u32,
    pub channels: u32,
    pub bit_depth: Option<u32>,
    pub file_size_bytes: u64,
    // Зарезервированы (не вычисляются, всегда None — убраны из отображения).
    pub lufs: Option<f64>,
    pub peak_dbfs: Option<f64>,
    pub bpm: Option<f32>,
    pub key: Option<String>,
    pub key_confidence: Option<f32>,
    /// Пики waveform, 4096 точек [0..1].
    pub peaks: Vec<f32>,
    /// Unix-время создания файла (секунды с эпохи).
    pub created_at: Option<u64>,
    pub error: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AnalysisProgress {
    pub path: String,
    pub done: usize,
    pub total: usize,
    pub meta: Option<AudioMeta>,
}

// ── Кэш ──────────────────────────────────────────────────────────────────────

/// Ключ: путь + mtime + размер → инвалидируется при изменении файла.
fn cache_key(path: &str) -> String {
    let meta = fs::metadata(path).ok();
    let mtime = meta.as_ref()
        .and_then(|m| m.modified().ok())
        .and_then(|t| t.duration_since(SystemTime::UNIX_EPOCH).ok())
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let size = meta.map(|m| m.len()).unwrap_or(0);
    format!("{path}|{mtime}|{size}")
}

/// Arc<Mutex<...>> внутри позволяет Clone — нужен для передачи в rayon-замыкания.
#[derive(Clone)]
pub struct AnalyzerCache(Arc<Mutex<HashMap<String, AudioMeta>>>);

impl AnalyzerCache {
    pub fn new() -> Self {
        Self(Arc::new(Mutex::new(HashMap::new())))
    }
    fn get(&self, path: &str) -> Option<AudioMeta> {
        let key = cache_key(path);
        self.0.lock().unwrap().get(&key).cloned()
    }
    fn set(&self, path: &str, meta: AudioMeta) {
        let key = cache_key(path);
        self.0.lock().unwrap().insert(key, meta);
    }
}

// ── Быстрый зонд контейнера ───────────────────────────────────────────────────

/// Читает только заголовок контейнера: формат, длительность, SR, каналы, битность.
/// Не декодирует сэмплы — работает мгновенно.
fn probe_container(path: &str) -> Result<(String, f64, u32, u32, Option<u32>), String> {
    use symphonia::core::formats::FormatOptions;
    use symphonia::core::io::MediaSourceStream;
    use symphonia::core::meta::MetadataOptions;
    use symphonia::core::probe::Hint;

    let file = fs::File::open(path).map_err(|e| e.to_string())?;
    let mss = MediaSourceStream::new(Box::new(file), Default::default());
    let mut hint = Hint::new();
    if let Some(ext) = Path::new(path).extension().and_then(|e| e.to_str()) {
        hint.with_extension(ext);
    }
    let probed = symphonia::default::get_probe()
        .format(&hint, mss, &FormatOptions::default(), &MetadataOptions::default())
        .map_err(|e| format!("probe: {e}"))?;

    let fmt = probed.format;
    let track = fmt.default_track().ok_or("no audio track")?;
    let p = &track.codec_params;

    let sr         = p.sample_rate.unwrap_or(44100);
    let ch         = p.channels.map(|c| c.count() as u32).unwrap_or(2);
    let bit_depth  = p.bits_per_sample;
    let duration_s = p.n_frames.map(|f| f as f64 / sr as f64).unwrap_or(0.0);

    let format_name = Path::new(path)
        .extension()
        .and_then(|e| e.to_str())
        .unwrap_or("?")
        .to_ascii_uppercase();

    Ok((format_name, duration_s, sr, ch, bit_depth))
}

// ── Даунсемплирование ─────────────────────────────────────────────────────────

/// Box-фильтр (среднее каждых ratio сэмплов). Для BPM/chroma достаточно.
fn downsample_to(samples: &[f32], from_sr: u32, to_sr: u32) -> Vec<f32> {
    if from_sr <= to_sr {
        return samples.to_vec();
    }
    let ratio = from_sr as f64 / to_sr as f64;
    let out_len = (samples.len() as f64 / ratio) as usize;
    (0..out_len)
        .map(|i| {
            let s = (i as f64 * ratio) as usize;
            let e = (((i + 1) as f64 * ratio) as usize).min(samples.len());
            if s >= e { return 0.0; }
            samples[s..e].iter().sum::<f32>() / (e - s) as f32
        })
        .collect()
}

// ── Частичное декодирование из середины ───────────────────────────────────────

/// Декодирует `window_s` секунд из середины трека, сводит в моно,
/// даунсемплирует до `target_sr`. Если файл короче — берёт всё с начала.
fn decode_middle_mono(
    path: &str,
    target_sr: u32,
    window_s: f64,
    duration_s: f64,
) -> Result<Vec<f32>, String> {
    use symphonia::core::audio::SampleBuffer;
    use symphonia::core::codecs::DecoderOptions;
    use symphonia::core::formats::{FormatOptions, SeekMode, SeekTo};
    use symphonia::core::io::MediaSourceStream;
    use symphonia::core::meta::MetadataOptions;
    use symphonia::core::probe::Hint;
    use symphonia::core::units::Time;

    let file = fs::File::open(path).map_err(|e| e.to_string())?;
    let mss = MediaSourceStream::new(Box::new(file), Default::default());
    let mut hint = Hint::new();
    if let Some(ext) = Path::new(path).extension().and_then(|e| e.to_str()) {
        hint.with_extension(ext);
    }
    let probed = symphonia::default::get_probe()
        .format(&hint, mss, &FormatOptions::default(), &MetadataOptions::default())
        .map_err(|e| e.to_string())?;

    let mut fmt = probed.format;
    let track = fmt.default_track().ok_or("no track")?;
    let codec_params = track.codec_params.clone();
    let track_id = track.id;
    let sr = codec_params.sample_rate.unwrap_or(44100);
    let ch = codec_params.channels.map(|c| c.count()).unwrap_or(2).max(1);

    let mut decoder = symphonia::default::get_codecs()
        .make(&codec_params, &DecoderOptions::default())
        .map_err(|e| e.to_string())?;

    // Начало окна: середина трека минус половина окна.
    let start_s = if duration_s > window_s {
        (duration_s / 2.0 - window_s / 2.0).max(0.0)
    } else {
        0.0
    };

    if start_s > 0.5 {
        // Grob-сик; если формат не поддерживает — просто начинаем с 0.
        let _ = fmt.seek(
            SeekMode::Coarse,
            SeekTo::Time {
                time: Time::new(start_s as u64, start_s.fract()),
                track_id: None,
            },
        );
    }

    let target_frames = (window_s * sr as f64) as u64;
    let mut interleaved: Vec<f32> =
        Vec::with_capacity((window_s * sr as f64) as usize * ch);
    let mut decoded_frames = 0u64;

    loop {
        if decoded_frames >= target_frames { break; }
        let packet = match fmt.next_packet() { Ok(p) => p, Err(_) => break };
        if packet.track_id() != track_id { continue; }
        let decoded = match decoder.decode(&packet) { Ok(d) => d, Err(_) => continue };
        let mut buf: SampleBuffer<f32> =
            SampleBuffer::new(decoded.capacity() as u64, *decoded.spec());
        buf.copy_interleaved_ref(decoded);
        interleaved.extend_from_slice(buf.samples());
        decoded_frames += buf.samples().len() as u64 / ch as u64;
    }

    // Моно: среднее каналов.
    let mono: Vec<f32> = if ch <= 1 {
        interleaved
    } else {
        interleaved.chunks(ch)
            .map(|fr| fr.iter().sum::<f32>() / ch as f32)
            .collect()
    };

    Ok(if sr != target_sr { downsample_to(&mono, sr, target_sr) } else { mono })
}

// ── Потоковые пики ────────────────────────────────────────────────────────────

/// Потоковый проход по всему файлу: вычисляет `target` пиковых значений.
/// Хранит O(target) данных вместо O(n_frames).
fn compute_peaks_streaming(path: &str, n_frames_hint: u64, target: usize) -> Vec<f32> {
    use symphonia::core::audio::SampleBuffer;
    use symphonia::core::codecs::DecoderOptions;
    use symphonia::core::formats::FormatOptions;
    use symphonia::core::io::MediaSourceStream;
    use symphonia::core::meta::MetadataOptions;
    use symphonia::core::probe::Hint;

    let mut peaks = vec![0.0f32; target];

    let Ok(file) = fs::File::open(path) else { return peaks; };
    let mss = MediaSourceStream::new(Box::new(file), Default::default());
    let mut hint = Hint::new();
    if let Some(ext) = Path::new(path).extension().and_then(|e| e.to_str()) {
        hint.with_extension(ext);
    }
    let Ok(probed) = symphonia::default::get_probe()
        .format(&hint, mss, &FormatOptions::default(), &MetadataOptions::default())
    else { return peaks; };

    let mut fmt = probed.format;
    let Some(track) = fmt.default_track() else { return peaks; };
    let codec_params = track.codec_params.clone();
    let track_id = track.id;
    let ch = codec_params.channels.map(|c| c.count()).unwrap_or(2).max(1);

    let Ok(mut decoder) = symphonia::default::get_codecs()
        .make(&codec_params, &DecoderOptions::default())
    else { return peaks; };

    if n_frames_hint > 0 {
        // Длина известна — фиксированные ковши.
        let bucket_size = (n_frames_hint as usize / target).max(1);
        let mut frame_pos = 0usize;
        loop {
            let packet = match fmt.next_packet() { Ok(p) => p, Err(_) => break };
            if packet.track_id() != track_id { continue; }
            let decoded = match decoder.decode(&packet) { Ok(d) => d, Err(_) => continue };
            let mut buf: SampleBuffer<f32> =
                SampleBuffer::new(decoded.capacity() as u64, *decoded.spec());
            buf.copy_interleaved_ref(decoded);
            for frame in buf.samples().chunks(ch) {
                let amp = frame.iter().fold(0.0f32, |m, &x| m.max(x.abs()));
                let bucket = (frame_pos / bucket_size).min(target - 1);
                if amp > peaks[bucket] { peaks[bucket] = amp; }
                frame_pos += 1;
            }
        }
    } else {
        // Длина неизвестна — собираем в вектор, потом даунсемплируем.
        let mut all: Vec<f32> = Vec::new();
        loop {
            let packet = match fmt.next_packet() { Ok(p) => p, Err(_) => break };
            if packet.track_id() != track_id { continue; }
            let decoded = match decoder.decode(&packet) { Ok(d) => d, Err(_) => continue };
            let mut buf: SampleBuffer<f32> =
                SampleBuffer::new(decoded.capacity() as u64, *decoded.spec());
            buf.copy_interleaved_ref(decoded);
            for frame in buf.samples().chunks(ch) {
                all.push(frame.iter().fold(0.0f32, |m, &x| m.max(x.abs())));
            }
        }
        if !all.is_empty() {
            let win = (all.len() / target).max(1);
            peaks = all.chunks(win)
                .map(|chunk| chunk.iter().cloned().fold(0.0f32, f32::max))
                .take(target)
                .collect();
            peaks.resize(target, 0.0);
        }
    }

    peaks
}

// ── BPM + Тональность (общий STFT) ────────────────────────────────────────────

// Профили Krumhansl-Schmuckler (12 полутонов с C).
const KS_MAJOR: [f64; 12] = [6.35,2.23,3.48,2.33,4.38,4.09,2.52,5.19,2.39,3.66,2.29,2.88];
const KS_MINOR: [f64; 12] = [6.33,2.68,3.52,5.38,2.60,3.53,2.54,4.75,3.98,2.69,3.34,3.17];
const NOTE_NAMES: [&str; 12] = ["C","C#","D","D#","E","F","F#","G","G#","A","A#","B"];

/// Один STFT-проход даёт и onset-огибающую (→ BPM), и chroma (→ тональность).
/// `mono` уже даунсемплирован до `sr` (обычно 11025 Гц).
fn compute_bpm_and_key(mono: &[f32], sr: u32) -> (Option<f32>, Option<String>, Option<f32>) {
    use rustfft::{FftPlanner, num_complex::Complex};

    if (mono.len() as u32) < sr { return (None, None, None); }

    let win = 2048usize;
    let hop = 512usize;
    let half = win / 2 + 1;

    let hann: Vec<f32> = (0..win)
        .map(|i| 0.5 * (1.0 - (2.0 * std::f32::consts::PI * i as f32 / (win - 1) as f32).cos()))
        .collect();

    let mut planner = FftPlanner::<f32>::new();
    let fft = planner.plan_fft_forward(win);

    let freq_per_bin = sr as f64 / win as f64;
    let mut onset_env: Vec<f32> = Vec::new();
    let mut chroma_acc = [0.0f64; 12];
    let mut prev_mag = vec![0.0f32; half];

    for start in (0..mono.len().saturating_sub(win)).step_by(hop) {
        let mut buf: Vec<Complex<f32>> = mono[start..start + win]
            .iter().zip(&hann)
            .map(|(&s, &h)| Complex::new(s * h, 0.0))
            .collect();
        fft.process(&mut buf);

        let mag: Vec<f32> = buf[..half].iter().map(|c| c.norm()).collect();

        // Спектральный поток → onset strength.
        onset_env.push(
            mag.iter().zip(&prev_mag).map(|(&m, &p)| (m - p).max(0.0)).sum()
        );

        // Chroma: мощность по питч-классам.
        for (k, &m) in mag[1..].iter().enumerate() {
            let freq = (k + 1) as f64 * freq_per_bin;
            if !(27.5..=4200.0).contains(&freq) { continue; }
            let midi = 69.0 + 12.0 * (freq / 440.0).log2();
            let pc = ((midi.round() as i32).rem_euclid(12)) as usize;
            chroma_acc[pc] += (m as f64) * (m as f64);
        }

        prev_mag = mag;
    }

    let bpm = bpm_from_onset(&onset_env, sr as f32 / hop as f32);
    let (key, conf) = key_from_chroma(&chroma_acc);
    (bpm, key, conf)
}

fn bpm_from_onset(onset: &[f32], fps: f32) -> Option<f32> {
    use rustfft::{FftPlanner, num_complex::Complex};

    let n = onset.len();
    if n < 16 { return None; }
    let max_o = onset.iter().cloned().fold(0.0f32, f32::max);
    if max_o < 1e-9 { return None; }
    let norm: Vec<f32> = onset.iter().map(|&x| x / max_o).collect();

    let fft_size = (2 * n).next_power_of_two();
    let mut planner = FftPlanner::<f32>::new();
    let fwd = planner.plan_fft_forward(fft_size);
    let inv = planner.plan_fft_inverse(fft_size);

    let mut buf: Vec<Complex<f32>> = (0..fft_size)
        .map(|i| Complex::new(if i < n { norm[i] } else { 0.0 }, 0.0))
        .collect();
    fwd.process(&mut buf);
    for c in &mut buf { *c = Complex::new(c.norm_sqr(), 0.0); }
    inv.process(&mut buf);

    let lag_min = (fps * 60.0 / 180.0).ceil() as usize;
    let lag_max = (fps * 60.0 / 70.0).floor() as usize;

    let best = (lag_min..=lag_max.min(buf.len() - 1))
        .max_by(|&a, &b| buf[a].re.partial_cmp(&buf[b].re).unwrap_or(std::cmp::Ordering::Equal))?;

    Some(normalize_bpm(fps * 60.0 / best as f32))
}

fn key_from_chroma(chroma_acc: &[f64; 12]) -> (Option<String>, Option<f32>) {
    let sum: f64 = chroma_acc.iter().sum();
    if sum < 1e-12 { return (None, None); }
    let chroma: Vec<f64> = chroma_acc.iter().map(|&x| x / sum).collect();

    let mut best_r = f64::NEG_INFINITY;
    let mut best_label = String::new();
    for root in 0..12usize {
        let r = pearson_correlation(&chroma, &rotate(&KS_MAJOR, root));
        if r > best_r { best_r = r; best_label = NOTE_NAMES[root].to_string(); }
        let r = pearson_correlation(&chroma, &rotate(&KS_MINOR, root));
        if r > best_r { best_r = r; best_label = format!("{}m", NOTE_NAMES[root]); }
    }
    let conf = best_r as f32;
    if conf < 0.55 { return (None, Some(conf)); }
    (Some(best_label), Some(conf))
}

fn rotate(arr: &[f64; 12], n: usize) -> Vec<f64> {
    let n = n % 12;
    arr[n..].iter().chain(arr[..n].iter()).cloned().collect()
}

fn pearson_correlation(a: &[f64], b: &[f64]) -> f64 {
    let n = a.len() as f64;
    let ma = a.iter().sum::<f64>() / n;
    let mb = b.iter().sum::<f64>() / n;
    let num: f64 = a.iter().zip(b).map(|(&x, &y)| (x - ma) * (y - mb)).sum();
    let da = a.iter().map(|&x| (x - ma).powi(2)).sum::<f64>().sqrt();
    let db = b.iter().map(|&x| (x - mb).powi(2)).sum::<f64>().sqrt();
    if da < 1e-12 || db < 1e-12 { return 0.0; }
    num / (da * db)
}

fn normalize_bpm(mut bpm: f32) -> f32 {
    while bpm < 70.0  { bpm *= 2.0; }
    while bpm > 180.0 { bpm /= 2.0; }
    bpm
}

// ── Главная функция анализа ───────────────────────────────────────────────────

pub fn analyze_one(path: &str) -> AudioMeta {
    let name = Path::new(path)
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or(path)
        .to_string();

    // FS-метаданные (мгновенно; fallback modified() если created() недоступно).
    let fs_meta = fs::metadata(path).ok();
    let file_size_bytes = fs_meta.as_ref().map(|m| m.len()).unwrap_or(0);
    let created_at = fs_meta
        .and_then(|m| m.created().ok().or_else(|| m.modified().ok()))
        .and_then(|t| t.duration_since(SystemTime::UNIX_EPOCH).ok())
        .map(|d| d.as_secs());

    // 1. Зонд контейнера — только заголовок, без декодирования.
    let (format, duration_s, sample_rate, channels, bit_depth) = match probe_container(path) {
        Ok(v) => v,
        Err(e) => return AudioMeta {
            path: path.to_string(), name,
            format: String::new(), duration_s: 0.0, sample_rate: 0, channels: 0,
            bit_depth: None, file_size_bytes, lufs: None, peak_dbfs: None,
            bpm: None, key: None, key_confidence: None, peaks: vec![],
            created_at, error: Some(e),
        },
    };

    let n_frames = if duration_s > 0.0 { (duration_s * sample_rate as f64) as u64 } else { 0 };

    // 2. Пики — потоковый проход по всему файлу, O(4096) памяти.
    let peaks = compute_peaks_streaming(path, n_frames, 4096);

    // 3. BPM + тональность — 50 сек из середины на 11025 Гц, один STFT.
    const TARGET_SR: u32 = 11025;
    const WINDOW_S:  f64 = 50.0;

    let (bpm, key, key_confidence) =
        match decode_middle_mono(path, TARGET_SR, WINDOW_S, duration_s) {
            Ok(mono) => compute_bpm_and_key(&mono, TARGET_SR),
            Err(_)   => (None, None, None),
        };

    AudioMeta {
        path: path.to_string(),
        name,
        format,
        duration_s,
        sample_rate,
        channels,
        bit_depth,
        file_size_bytes,
        lufs: None,
        peak_dbfs: None,
        bpm,
        key,
        key_confidence,
        peaks,
        created_at,
        error: None,
    }
}

// ── Кодирование WAV (для player_decode_to_wav) ────────────────────────────────

fn encode_wav(samples: &[f32], sr: u32, channels: u32) -> Vec<u8> {
    let n = samples.len();
    let bps: u16 = 16;
    let byte_rate = sr * channels * bps as u32 / 8;
    let block_align = channels as u16 * bps / 8;
    let data_size = (n * 2) as u32;
    let file_size = 36 + data_size;

    let mut out = Vec::with_capacity(44 + data_size as usize);
    out.extend_from_slice(b"RIFF");
    out.extend_from_slice(&file_size.to_le_bytes());
    out.extend_from_slice(b"WAVE");
    out.extend_from_slice(b"fmt ");
    out.extend_from_slice(&16u32.to_le_bytes());
    out.extend_from_slice(&1u16.to_le_bytes());
    out.extend_from_slice(&(channels as u16).to_le_bytes());
    out.extend_from_slice(&sr.to_le_bytes());
    out.extend_from_slice(&byte_rate.to_le_bytes());
    out.extend_from_slice(&block_align.to_le_bytes());
    out.extend_from_slice(&bps.to_le_bytes());
    out.extend_from_slice(b"data");
    out.extend_from_slice(&data_size.to_le_bytes());
    for &s in samples {
        out.extend_from_slice(&((s.clamp(-1.0, 1.0) * 32767.0) as i16).to_le_bytes());
    }
    out
}

// ── Сканирование папки ────────────────────────────────────────────────────────

fn is_audio_ext(path: &Path) -> bool {
    matches!(
        path.extension()
            .and_then(|e| e.to_str())
            .map(|e| e.to_ascii_lowercase())
            .as_deref(),
        Some("wav"|"mp3"|"flac"|"ogg"|"aiff"|"aif"|"m4a"|"aac")
    )
}

fn scan_dir_recursive(dir: &str, results: &mut Vec<String>) {
    let Ok(entries) = fs::read_dir(dir) else { return };
    for entry in entries.flatten() {
        let path = entry.path();
        if path.is_dir() {
            scan_dir_recursive(&path.to_string_lossy(), results);
        } else if is_audio_ext(&path) {
            if let Some(s) = path.to_str() { results.push(s.to_string()); }
        }
    }
}

// ── Tauri-команды ─────────────────────────────────────────────────────────────

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
    if let Some(cached) = cache.get(&path) {
        return Ok(cached);
    }
    let p = path.clone();
    let meta = tauri::async_runtime::spawn_blocking(move || analyze_one(&p))
        .await
        .map_err(|e| e.to_string())?;
    cache.set(&path, meta.clone());
    Ok(meta)
}

/// Анализирует батч файлов параллельно через rayon; события стримятся по мере готовности.
#[tauri::command]
pub async fn player_analyze_batch(
    paths: Vec<String>,
    app: AppHandle,
    cache: State<'_, AnalyzerCache>,
) -> Result<(), String> {
    use rayon::prelude::*;

    let total = paths.len();
    let done  = Arc::new(AtomicUsize::new(0));

    // Закэшированные файлы — отдаём сразу.
    let mut uncached: Vec<String> = Vec::new();
    for path in &paths {
        if let Some(meta) = cache.get(path) {
            let n = done.fetch_add(1, Ordering::Relaxed) + 1;
            let _ = app.emit("player-analysis-progress", AnalysisProgress {
                path: path.clone(), done: n, total, meta: Some(meta),
            });
        } else {
            uncached.push(path.clone());
        }
    }

    if uncached.is_empty() { return Ok(()); }

    // Клонируем AppHandle и AnalyzerCache (оба Send+Sync через Arc) для rayon.
    let cache_clone = cache.inner().clone();
    let app_clone   = app.clone();
    let done_clone  = done.clone();

    tauri::async_runtime::spawn_blocking(move || {
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

/// Декодирует аудиофайл в WAV-байты (для форматов, не поддерживаемых браузером).
#[tauri::command]
pub async fn player_decode_to_wav(path: String) -> Result<Vec<u8>, String> {
    tauri::async_runtime::spawn_blocking(move || {
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
        Ok(encode_wav(&interleaved, sr, ch))
    })
    .await
    .map_err(|e| e.to_string())?
}
