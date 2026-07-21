// Модуль аудио-аналайзера для вкладки «Плеер».
//
// Конвейер построен вокруг скорости:
//   1. Быстрый зонд (probe_container): формат, длительность, SR — только из
//      заголовка контейнера. Уходит в UI мгновенно как partial-метаданные.
//   2. ОДИН потоковый проход декодера на файл: одновременно пики волны
//      (O(4096) памяти) и моно-окно из середины для BPM/тональности.
//      Раньше файл декодировался дважды — это была главная статья расходов.
//   3. DSP по одному моно-буферу: onset-STFT (→ BPM) на 50-секундном окне из
//      середины и HPCP-хрома (→ тональность) по всему треку (кап 240 с) —
//      пики спектра с поправкой строя, гармоническое взвешивание, басовая
//      хрома, профили Sha'ath. См. раздел «Тональность (HPCP)».
//   4. Двухуровневый кэш: память (сессия) + диск (между запусками) — повторное
//      открытие тех же папок не декодирует ничего.
//
// Параллельный батч — rayon::par_iter; каждый файл эмитирует событие по готовности.

use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::SystemTime;

use serde::{Deserialize, Serialize};

// ── Типы ─────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
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
    /// true — быстрые метаданные из заголовка; DSP (пики/BPM/key) ещё идёт.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub partial: Option<bool>,
    /// Перцептивный отпечаток (496-бит hex) для акустического дедупа; пусто у
    /// partial-строк и при ошибке анализа. `#[serde(default)]` — совместимость
    /// со старым дисковым кэшем.
    #[serde(default)]
    pub fingerprint: String,
}


// ── Кэш ──────────────────────────────────────────────────────────────────────

/// Версия формата анализа. Бамп инвалидирует дисковый кэш целиком — нужен,
/// когда меняется алгоритм или формат меток (например, Am → Amin).
const ANALYSIS_VERSION: u32 = 4;

/// Ключ: версия + путь + mtime + размер → инвалидируется при изменении файла
/// или алгоритма анализа.
fn cache_key(path: &str) -> String {
    let meta = fs::metadata(path).ok();
    let mtime = meta.as_ref()
        .and_then(|m| m.modified().ok())
        .and_then(|t| t.duration_since(SystemTime::UNIX_EPOCH).ok())
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let size = meta.map(|m| m.len()).unwrap_or(0);
    format!("v{ANALYSIS_VERSION}|{path}|{mtime}|{size}")
}

/// FNV-1a: имя файла дискового кэша из ключа, без внешних зависимостей.
fn fnv1a(s: &str) -> u64 {
    let mut h: u64 = 0xcbf29ce484222325;
    for b in s.as_bytes() {
        h ^= *b as u64;
        h = h.wrapping_mul(0x100000001b3);
    }
    h
}

/// Дисковая запись: ключ хранится внутри и сверяется при чтении, чтобы
/// коллизия имени файла не подсунула чужие метаданные.
#[derive(Serialize, Deserialize)]
struct CacheEntry {
    key: String,
    meta: AudioMeta,
}

/// Двухуровневый кэш анализа: память (на время сессии) + диск (между
/// запусками приложения). Arc внутри позволяет Clone для rayon-замыканий.
#[derive(Clone)]
pub struct AnalyzerCache {
    mem: Arc<Mutex<HashMap<String, AudioMeta>>>,
    dir: Option<Arc<PathBuf>>,
}

impl AnalyzerCache {
    pub fn new(dir: Option<PathBuf>) -> Self {
        let dir = dir.and_then(|d| fs::create_dir_all(&d).ok().map(|_| Arc::new(d)));
        Self { mem: Arc::new(Mutex::new(HashMap::new())), dir }
    }

    fn disk_file(&self, key: &str) -> Option<PathBuf> {
        self.dir.as_ref().map(|d| d.join(format!("{:016x}.json", fnv1a(key))))
    }

    pub fn get(&self, path: &str) -> Option<AudioMeta> {
        let key = cache_key(path);
        if let Some(m) = self.mem.lock().unwrap().get(&key) {
            return Some(m.clone());
        }
        let file = self.disk_file(&key)?;
        let bytes = fs::read(file).ok()?;
        let entry: CacheEntry = serde_json::from_slice(&bytes).ok()?;
        if entry.key != key { return None; }
        self.mem.lock().unwrap().insert(key, entry.meta.clone());
        Some(entry.meta)
    }

    pub fn set(&self, path: &str, meta: AudioMeta) {
        let key = cache_key(path);
        // Ошибки анализа на диск не пишем: пусть следующая сессия попробует
        // ещё раз (файл мог быть временно занят/недочитан).
        if meta.error.is_none() {
            if let Some(file) = self.disk_file(&key) {
                if let Ok(bytes) = serde_json::to_vec(&CacheEntry { key: key.clone(), meta: meta.clone() }) {
                    let _ = fs::write(file, bytes);
                }
            }
        }
        self.mem.lock().unwrap().insert(key, meta);
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

// ── Единый потоковый проход: пики + моно-окно для DSP ────────────────────────

/// Результат одного прохода декодера по файлу.
struct StreamedAudio {
    /// `target_peaks` пиков волны [0..1].
    peaks: Vec<f32>,
    /// Моно-окно `window_s` секунд из середины (для BPM/тональности).
    dsp_mono: Vec<f32>,
    /// Фактический SR `dsp_mono`: целевой, либо нативный, если файл ниже целевого.
    dsp_sr: u32,
}

/// Декодирует файл ОДИН раз, попутно собирая и пики для отрисовки волны,
/// и моно-окно из середины для BPM/тональности. Раньше это были два
/// независимых прохода (полный + окно с seek) — почти вдвое больше работы
/// декодера на каждый файл. Даунсемплирование — box-фильтром на лету
/// (аккумуляция сумма/счётчик по выходным ковшам): окно в 240 с в нативном
/// SR раздувало бы память параллельного батча на десятки МБ на файл.
fn stream_peaks_and_dsp(
    path: &str,
    n_frames_hint: u64,
    target_peaks: usize,
    dsp_sr: u32,
    window_s: f64,
    duration_s: f64,
) -> StreamedAudio {
    use symphonia::core::audio::SampleBuffer;
    use symphonia::core::codecs::DecoderOptions;
    use symphonia::core::formats::FormatOptions;
    use symphonia::core::io::MediaSourceStream;
    use symphonia::core::meta::MetadataOptions;
    use symphonia::core::probe::Hint;

    let mut out = StreamedAudio { peaks: vec![0.0; target_peaks], dsp_mono: Vec::new(), dsp_sr };

    let Ok(file) = fs::File::open(path) else { return out; };
    let mss = MediaSourceStream::new(Box::new(file), Default::default());
    let mut hint = Hint::new();
    if let Some(ext) = Path::new(path).extension().and_then(|e| e.to_str()) {
        hint.with_extension(ext);
    }
    let Ok(probed) = symphonia::default::get_probe()
        .format(&hint, mss, &FormatOptions::default(), &MetadataOptions::default())
    else { return out; };

    let mut fmt = probed.format;
    let Some(track) = fmt.default_track() else { return out; };
    let codec_params = track.codec_params.clone();
    let track_id = track.id;
    let sr = codec_params.sample_rate.unwrap_or(44100).max(1);
    let ch = codec_params.channels.map(|c| c.count()).unwrap_or(2).max(1);

    let Ok(mut decoder) = symphonia::default::get_codecs()
        .make(&codec_params, &DecoderOptions::default())
    else { return out; };

    // Окно DSP в кадрах: из середины трека; если длина неизвестна (VBR без
    // заголовка) или трек короче окна — с начала.
    let start_s = if duration_s > window_s { duration_s / 2.0 - window_s / 2.0 } else { 0.0 };
    let dsp_start = (start_s * sr as f64) as u64;
    let dsp_end   = dsp_start + (window_s * sr as f64) as u64;

    // Аккумуляторы даунсемплирования. Не апсемплируем: файлам ниже целевого
    // SR отдаём нативный (dsp_sr в out это отражает).
    let out_sr = dsp_sr.min(sr);
    out.dsp_sr = out_sr;
    let eff_window_s = if duration_s > 0.0 { duration_s.min(window_s) + 1.0 } else { window_s };
    let want_out = (eff_window_s * out_sr as f64).ceil() as usize;
    let mut dsp_sum: Vec<f32> = vec![0.0; want_out];
    let mut dsp_cnt: Vec<u16> = vec![0; want_out];
    let mut dsp_len: usize = 0;

    // Пики: при известной длине — фиксированные ковши, иначе собираем всё
    // и даунсемплируем в конце.
    let bucket_size = if n_frames_hint > 0 {
        (n_frames_hint as usize / target_peaks).max(1)
    } else {
        0
    };
    let mut all_amps: Vec<f32> = Vec::new();

    // SampleBuffer переиспользуется между пакетами: аллокация на каждый из
    // тысяч пакетов была заметной статьёй расходов.
    let mut sample_buf: Option<SampleBuffer<f32>> = None;
    let mut frame_pos: u64 = 0;

    loop {
        let packet = match fmt.next_packet() { Ok(p) => p, Err(_) => break };
        if packet.track_id() != track_id { continue; }
        let decoded = match decoder.decode(&packet) { Ok(d) => d, Err(_) => continue };

        let spec = *decoded.spec();
        let needed = decoded.capacity() * spec.channels.count();
        let recreate = match &sample_buf {
            Some(b) => b.capacity() < needed,
            None => true,
        };
        if recreate {
            sample_buf = Some(SampleBuffer::new(decoded.capacity() as u64, spec));
        }
        let buf = sample_buf.as_mut().unwrap();
        buf.copy_interleaved_ref(decoded);

        for frame in buf.samples().chunks(ch) {
            let mut amp = 0.0f32;
            let mut sum = 0.0f32;
            for &s in frame {
                let a = s.abs();
                if a > amp { amp = a; }
                sum += s;
            }

            if bucket_size > 0 {
                let b = ((frame_pos as usize) / bucket_size).min(target_peaks - 1);
                if amp > out.peaks[b] { out.peaks[b] = amp; }
            } else {
                all_amps.push(amp);
            }

            if frame_pos >= dsp_start && frame_pos < dsp_end {
                let rel = frame_pos - dsp_start;
                let idx = ((rel as u128 * out_sr as u128) / sr as u128) as usize;
                if idx < want_out {
                    dsp_sum[idx] += sum / ch as f32;
                    dsp_cnt[idx] = dsp_cnt[idx].saturating_add(1);
                    if idx >= dsp_len { dsp_len = idx + 1; }
                }
            }
            frame_pos += 1;
        }
    }

    if bucket_size == 0 && !all_amps.is_empty() {
        let win = (all_amps.len() / target_peaks).max(1);
        out.peaks = all_amps.chunks(win)
            .map(|c| c.iter().cloned().fold(0.0f32, f32::max))
            .take(target_peaks)
            .collect();
        out.peaks.resize(target_peaks, 0.0);
    }

    out.dsp_mono = dsp_sum[..dsp_len]
        .iter()
        .zip(&dsp_cnt[..dsp_len])
        .map(|(&s, &c)| if c > 0 { s / c as f32 } else { 0.0 })
        .collect();
    out
}

// ── BPM (onset-STFT) ──────────────────────────────────────────────────────────

const NOTE_NAMES: [&str; 12] = ["C","C#","D","D#","E","F","F#","G","G#","A","A#","B"];

// Границы поиска темпа и лог-нормальный приор. Центр 140 BPM отражает
// основной жанр библиотеки (хип-хоп/трэп) и решает октавную неоднозначность
// («70 или 140?») в пользу привычной нотации.
const BPM_MIN: f32 = 60.0;
const BPM_MAX: f32 = 200.0;
const BPM_PRIOR_CENTER: f32 = 140.0;
const BPM_PRIOR_SIGMA: f32 = 0.8; // в октавах (log2)

/// Onset-огибающая по STFT (положительный спектральный поток) → BPM.
/// `mono` уже даунсемплирован до `sr` (обычно 11025 Гц).
///
/// Тональность считается отдельным проходом (см. detect_key): ей нужно окно
/// 8192 для разрешения баса и редкий хоп, а onset-огибающей — наоборот,
/// плотный хоп с коротким окном. Общий STFT был компромиссом, который портил
/// хрому. FFT-буфер, scratch и вектора магнитуд переиспользуются между кадрами.
fn compute_bpm(mono: &[f32], sr: u32) -> Option<f32> {
    use rustfft::{FftPlanner, num_complex::Complex};

    if (mono.len() as u32) < sr { return None; }

    let win = 2048usize;
    // hop 256 (fps ≈ 43 при 11025 Гц) — вдвое плотнее сетка лагов, чем при 512:
    // на старой сетке между 70 и 180 BPM было всего ~11 дискретных значений,
    // и точный темп (например, ровно 140) был физически недостижим.
    let hop = 256usize;
    let half = win / 2 + 1;

    let hann: Vec<f32> = (0..win)
        .map(|i| 0.5 * (1.0 - (2.0 * std::f32::consts::PI * i as f32 / (win - 1) as f32).cos()))
        .collect();

    let mut planner = FftPlanner::<f32>::new();
    let fft = planner.plan_fft_forward(win);
    let mut scratch = vec![Complex::new(0.0f32, 0.0); fft.get_inplace_scratch_len()];

    let mut onset_env: Vec<f32> = Vec::with_capacity(mono.len() / hop + 1);
    let mut prev_mag = vec![0.0f32; half];
    let mut mag = vec![0.0f32; half];
    let mut buf: Vec<Complex<f32>> = vec![Complex::new(0.0f32, 0.0); win];
    let mut first_frame = true;

    for start in (0..mono.len().saturating_sub(win)).step_by(hop) {
        for i in 0..win {
            buf[i] = Complex::new(mono[start + i] * hann[i], 0.0);
        }
        fft.process_with_scratch(&mut buf, &mut scratch);

        for k in 0..half {
            mag[k] = buf[k].norm();
        }

        // Спектральный поток по лог-сжатым магнитудам → onset strength.
        // Лог-компрессия уравнивает вклад тихих и громких полос (иначе бас
        // доминирует), а первый кадр пропускаем: diff с нулевым prev_mag дал
        // бы огромный ложный пик в начале огибающей.
        if first_frame {
            first_frame = false;
            onset_env.push(0.0);
        } else {
            let mut flux = 0.0f32;
            for k in 0..half {
                let d = (1.0 + mag[k]).ln() - (1.0 + prev_mag[k]).ln();
                if d > 0.0 { flux += d; }
            }
            onset_env.push(flux);
        }

        std::mem::swap(&mut prev_mag, &mut mag);
    }

    bpm_from_onset(&onset_env, sr as f32 / hop as f32)
}

/// Оценка темпа по onset-огибающей: автокорреляция + гармоническая поддержка.
///
/// Отличия от «наивного argmax по автокорреляции», который давал системно
/// неверный BPM:
///   - вычитание среднего: автокорреляция положительного сигнала монотонно
///     убывает с лагом, поэтому argmax почти всегда попадал в минимальный лаг —
///     практически все треки получали BPM у верхней границы диапазона;
///   - unbiased-нормировка ac[k]/(n−k) и приведение к r[k] = ac[k]/ac[0];
///   - счёт кандидата по сумме гармоник r[L] + r[2L]/2 + r[3L]/4: истинный
///     период доли поддержан своими кратными, случайный пик — нет;
///   - мягкий лог-приор вокруг 140 BPM против октавных ошибок;
///   - параболическое уточнение вершины: дробный лаг даёт точность лучше
///     0.5 BPM вместо шага сетки в несколько BPM.
fn bpm_from_onset(onset: &[f32], fps: f32) -> Option<f32> {
    use rustfft::{FftPlanner, num_complex::Complex};

    let n = onset.len();
    if n < 48 { return None; } // меньше ~1 сек огибающей — не на чем считать

    let mean = onset.iter().sum::<f32>() / n as f32;
    let mut centered: Vec<f32> = onset.iter().map(|&x| x - mean).collect();
    let max_abs = centered.iter().fold(0.0f32, |m, &x| m.max(x.abs()));
    if max_abs < 1e-9 { return None; }
    for x in &mut centered { *x /= max_abs; }

    let fft_size = (2 * n).next_power_of_two();
    let mut planner = FftPlanner::<f32>::new();
    let fwd = planner.plan_fft_forward(fft_size);
    let inv = planner.plan_fft_inverse(fft_size);

    let mut buf: Vec<Complex<f32>> = (0..fft_size)
        .map(|i| Complex::new(if i < n { centered[i] } else { 0.0 }, 0.0))
        .collect();
    fwd.process(&mut buf);
    for c in &mut buf { *c = Complex::new(c.norm_sqr(), 0.0); }
    inv.process(&mut buf);

    let var = buf[0].re / n as f32;
    if var < 1e-12 { return None; }
    // Нормированная unbiased-автокорреляция; за пределами надёжной зоны — 0.
    // Ограничение k < 3n/4 не даёт unbiased-делителю (n−k) раздуть шум хвоста.
    let r = |k: usize| -> f32 {
        if k == 0 || k >= n * 3 / 4 { return 0.0; }
        (buf[k].re / (n - k) as f32) / var
    };

    let lag_min = ((fps * 60.0 / BPM_MAX).ceil() as usize).max(2);
    let lag_max = ((fps * 60.0 / BPM_MIN).floor() as usize).min(n / 2);
    if lag_min >= lag_max { return None; }

    let mut best_lag = 0usize;
    let mut best_score = f32::NEG_INFINITY;
    for lag in lag_min..=lag_max {
        let bpm = fps * 60.0 / lag as f32;
        let z = (bpm / BPM_PRIOR_CENTER).log2() / BPM_PRIOR_SIGMA;
        let w = (-0.5 * z * z).exp();
        let score = w * (r(lag) + 0.5 * r(2 * lag) + 0.25 * r(3 * lag));
        if score > best_score {
            best_score = score;
            best_lag = lag;
        }
    }
    // Слабая периодичность (эмбиент, шум, одиночные хиты) — честнее «—»,
    // чем случайное число.
    if best_lag == 0 || r(best_lag) < 0.08 { return None; }

    // Параболическое уточнение вершины по соседним лагам.
    let (y0, y1, y2) = (r(best_lag - 1), r(best_lag), r(best_lag + 1));
    let denom = y0 - 2.0 * y1 + y2;
    let delta = if denom.abs() > 1e-9 {
        (0.5 * (y0 - y2) / denom).clamp(-0.5, 0.5)
    } else {
        0.0
    };
    let lag_f = best_lag as f32 + delta;

    Some(normalize_bpm(fps * 60.0 / lag_f))
}

// ── Тональность (HPCP) ────────────────────────────────────────────────────────
//
// Прежний детектор ссыпал в хрому ВСЮ энергию каждого кадра общего с BPM STFT
// (окно 2048): на частотах 808-баса один бин накрывал до трёх полутонов, детюн
// смещал энергию в чужие питч-классы, перкуссия и обертоны загрязняли профиль,
// а KS-профили (выведенные на классике) путали относительные тональности.
// Новый конвейер — HPCP (Harmonic Pitch Class Profile), как в KeyFinder /
// Mixed In Key / Essentia:
//   пики спектра (окно 8192, параболическое уточнение частоты)
//   → глобальная поправка строя (детюн, не-A440)
//   → раскладка по питч-классам с гармоническим взвешиванием (h=1..4)
//   → отдельная басовая хрома (тоника у бит-музыки живёт в басу)
//   → профили Sha'ath + бонус за совпадение тоники с басом
//   → уверенность: отрыв от конкурента + согласие 10-секундных блоков.

/// Окно 8192 @ 11025 Гц (0.74 с, 1.35 Гц/бин): соседние полутоны разводятся
/// по бинам даже в басовом регистре. Хоп редкий: для глобальной тональности
/// плотная сетка кадров не нужна, зато считается быстро.
const CHROMA_WIN: usize = 8192;
const CHROMA_HOP: usize = 2048;
/// Диапазон поиска пиков (Гц): ниже A1 полутоны не разводятся даже этим
/// окном, выше A7 — тарелки и артефакты кодеков.
const PEAK_FMIN: f64 = 55.0;
const PEAK_FMAX: f64 = 3520.0;
/// Верх «басовой» зоны: доминирующий питч-класс здесь — почти всегда тоника.
const BASS_FMAX: f64 = 220.0;
/// Веса гармоник 1..4 (затухание 0.6): пик кредитует и возможные фундаменталы
/// ниже себя — обертоны перестают раздувать терцию и квинту.
const HARMONIC_W: [f64; 4] = [1.0, 0.6, 0.36, 0.216];
/// Бонус кандидату, чья тоника совпадает с доминирующим басом (доля баса на
/// тонике 0..1 → до +0.1 к корреляции). Главный разводящий для относительных
/// тональностей (Amin и Cmaj — один набор нот, различие только в тонике).
const BASS_BONUS: f64 = 0.10;
/// Верх зоны «якорей» для подавления обертонов: гармоники, о которые
/// спотыкался режим, порождает бас (дисторшн-808), а не мелодия.
const BASS_ANCHOR_FMAX: f64 = 260.0;
/// Отношения обертон/якорь: 3, 6 — квинтовые гармоники, 5 — большая терция,
/// 7 — септима; 2.5 и 3.5 — те же 5-я и 7-я, когда якорь — 2-я гармоника
/// суб-баса (фундаментал ниже PEAK_FMIN и в списке пиков отсутствует).
const OVERTONE_RATIOS: [f64; 6] = [2.5, 3.0, 3.5, 5.0, 6.0, 7.0];
/// Остаточный вес обертона (не ноль: изредка реальная нота совпадает с сеткой).
const OVERTONE_DAMP: f64 = 0.25;
/// Вес явного сравнения терций при выборе режима: перевес большой терции над
/// малой у тоники кандидата (−1..1) смещает мажор/минор на ±MODE_BONUS.
/// Применяется с весом доказательности — см. key_scores. Калибровка на эвале:
/// 0.10 и дамп 0.15 давали меньше параллельных ошибок, но ломали корни
/// («мимо» росло быстрее) — 0.06/0.25 оптимум по weighted.
const MODE_BONUS: f64 = 0.06;

// Профили Крумхансл–Шмуклер. На «грязной» хроме их обгоняли профили Sha'ath
// (подогнанные под артефакты пайплайна KeyFinder), но на HPCP с подавлением
// обертонов и явным сравнением терций классические KS выиграли эвал на
// реальных битах: 67.3% weighted против 61.6% у Sha'ath (см. key_eval_folder).
const KEY_MAJOR: [f64; 12] = [6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88];
const KEY_MINOR: [f64; 12] = [6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17];

/// Хрома-признаки трека, извлечённые одним проходом по моно-буферу.
struct ChromaFeatures {
    /// Глобальная HPCP-хрома (сумма по тональным кадрам).
    hpcp: [f64; 12],
    /// Басовая хрома (пики ≤ BASS_FMAX, только фундаменталы).
    bass: [f64; 12],
    /// HPCP по ~10-секундным блокам — для голосования при оценке уверенности.
    blocks: Vec<[f64; 12]>,
    /// Кадры с внятными тональными пиками (прошли гейты тишины/плоскостности).
    tonal_frames: usize,
}

/// Извлекает HPCP-хрому: пики спектра → поправка строя → питч-классы.
fn chroma_features(mono: &[f32], sr: u32) -> Option<ChromaFeatures> {
    use rustfft::{FftPlanner, num_complex::Complex};

    let win = CHROMA_WIN;
    let hop = CHROMA_HOP;
    if mono.len() < win + hop { return None; }
    let half = win / 2 + 1;
    let freq_per_bin = sr as f64 / win as f64;
    let kmin = (PEAK_FMIN / freq_per_bin).ceil() as usize;
    let kmax = ((PEAK_FMAX / freq_per_bin).floor() as usize).min(half.saturating_sub(2));
    if kmin < 1 || kmin + 8 >= kmax { return None; } // SR слишком низкий

    let hann: Vec<f32> = (0..win)
        .map(|i| 0.5 * (1.0 - (2.0 * std::f32::consts::PI * i as f32 / (win - 1) as f32).cos()))
        .collect();
    let mut planner = FftPlanner::<f32>::new();
    let fft = planner.plan_fft_forward(win);
    let mut scratch = vec![Complex::new(0.0f32, 0.0); fft.get_inplace_scratch_len()];
    let mut buf = vec![Complex::new(0.0f32, 0.0); win];
    let mut mag = vec![0.0f32; half];

    /// Спектральный пик: частота уточнена параболой по ln-магнитудам соседей.
    struct Peak { freq: f64, mag: f64, frame: usize }
    let mut peaks: Vec<Peak> = Vec::new();
    let mut tonal_frames = 0usize;

    // На кадр — не больше 40 сильнейших пиков: хватает на все голоса микса,
    // а хвост шумовых микро-максимумов отсекается.
    const MAX_PEAKS: usize = 40;
    let mut frame_peaks: Vec<(f64, f64)> = Vec::with_capacity(128);

    for start in (0..mono.len() - win).step_by(hop) {
        for i in 0..win {
            buf[i] = Complex::new(mono[start + i] * hann[i], 0.0);
        }
        fft.process_with_scratch(&mut buf, &mut scratch);
        for k in 0..half {
            mag[k] = buf[k].norm();
        }

        // Гейты кадра: тишина и «плоские» кадры (перкуссия/шум) не голосуют.
        // Плоскостность = geo/arith средних магнитуд: ~1 у шума, ≪1 у нот.
        let mut fmax = 0.0f32;
        let mut sum_m = 0.0f64;
        let mut sum_ln = 0.0f64;
        for k in kmin..=kmax {
            let m = mag[k];
            if m > fmax { fmax = m; }
            sum_m += m as f64;
            sum_ln += (m as f64 + 1e-12).ln();
        }
        if fmax < 1e-4 { continue; }
        let nb = (kmax - kmin + 1) as f64;
        let flatness = (sum_ln / nb).exp() / (sum_m / nb + 1e-12);
        if flatness > 0.5 { continue; }

        // Локальные максимумы выше −50 дБ от пика кадра.
        frame_peaks.clear();
        let thr = fmax * 3.2e-3;
        for k in kmin..=kmax {
            let m = mag[k];
            if m <= thr || m <= mag[k - 1] || m < mag[k + 1] { continue; }
            let l = (mag[k - 1] as f64 + 1e-12).ln();
            let c = (m as f64).ln();
            let r = (mag[k + 1] as f64 + 1e-12).ln();
            let den = l - 2.0 * c + r;
            let delta = if den.abs() > 1e-12 { (0.5 * (l - r) / den).clamp(-0.5, 0.5) } else { 0.0 };
            frame_peaks.push(((k as f64 + delta) * freq_per_bin, m as f64));
        }
        if frame_peaks.len() < 2 { continue; }
        tonal_frames += 1;
        if frame_peaks.len() > MAX_PEAKS {
            frame_peaks.sort_unstable_by(|a, b| b.1.total_cmp(&a.1));
            frame_peaks.truncate(MAX_PEAKS);
        }

        // Подавление обертонов баса: пик в целочисленном (или полуцелом, когда
        // фундаментал ниже диапазона и якорем служит 2-я гармоника) отношении
        // к более сильному НИЗКОМУ пику кадра — гармоника тембра, а не нота.
        // Главный виновник ложного мажора: 5-я гармоника дисторшн-808 ложится
        // ровно на большую терцию тоники (в минорных битах это перевешивало
        // малую терцию мелодии — параллельные ошибки «minor → major»).
        // Октавные отношения (2, 4, 8) не трогаем: питч-класс тот же.
        let frame = start / hop;
        for i in 0..frame_peaks.len() {
            let (freq, m) = frame_peaks[i];
            let mut damp = 1.0f64;
            'anchors: for &(fj, mj) in &frame_peaks {
                if fj > BASS_ANCHOR_FMAX || fj >= freq || mj < m * 1.2 { continue; }
                let r = freq / fj;
                for &n in &OVERTONE_RATIOS {
                    if (r / n - 1.0).abs() < 0.03 {
                        damp = OVERTONE_DAMP;
                        break 'anchors;
                    }
                }
            }
            peaks.push(Peak { freq, mag: m * damp, frame });
        }
    }
    if tonal_frames == 0 { return None; }

    // Глобальная поправка строя: циркулярное среднее отклонений пиков от
    // полутоновой сетки. Вес √mag — чтобы громкий бас не решал единолично.
    let (mut sc, mut ss) = (0.0f64, 0.0f64);
    for p in &peaks {
        let midi = 69.0 + 12.0 * (p.freq / 440.0).log2();
        let dev = midi - midi.round(); // −0.5..0.5 полутона
        let w = p.mag.sqrt();
        let a = 2.0 * std::f64::consts::PI * dev;
        sc += w * a.cos();
        ss += w * a.sin();
    }
    let tuning = ss.atan2(sc) / (2.0 * std::f64::consts::PI);

    // Аккумуляция: пик кредитует питч-классы возможных фундаменталов
    // (гармоники 1..4); вклад взвешен близостью к центру полутона (cos²),
    // чтобы «межнотные» частоты не голосовали в полную силу.
    let frames_per_block = ((10.0 * sr as f64 / hop as f64).ceil() as usize).max(1);
    let total_frames = (mono.len() - win) / hop + 1;
    let n_blocks = (total_frames + frames_per_block - 1) / frames_per_block;
    let mut hpcp = [0.0f64; 12];
    let mut bass = [0.0f64; 12];
    let mut blocks = vec![[0.0f64; 12]; n_blocks];
    for p in &peaks {
        let bi = (p.frame / frames_per_block).min(n_blocks - 1);
        for (h, &hw) in HARMONIC_W.iter().enumerate() {
            let f0 = p.freq / (h + 1) as f64;
            if f0 < 27.5 { break; }
            let midi = 69.0 + 12.0 * (f0 / 440.0).log2() - tuning;
            let frac = midi - midi.round();
            let closeness = (std::f64::consts::PI * frac).cos();
            let contrib = p.mag * hw * closeness * closeness;
            let pc = (midi.round() as i64).rem_euclid(12) as usize;
            hpcp[pc] += contrib;
            blocks[bi][pc] += contrib;
        }
        if p.freq <= BASS_FMAX {
            let midi = 69.0 + 12.0 * (p.freq / 440.0).log2() - tuning;
            let pc = (midi.round() as i64).rem_euclid(12) as usize;
            bass[pc] += p.mag;
        }
    }
    Some(ChromaFeatures { hpcp, bass, blocks, tonal_frames })
}

/// Тональность трека: HPCP-хрома + профили Sha'ath.
/// Формат меток: «Cmaj» / «Amin» — привычная битмейкерам нотация.
fn detect_key(mono: &[f32], sr: u32) -> (Option<String>, Option<f32>) {
    match chroma_features(mono, sr) {
        Some(f) => match_key(&f, &KEY_MAJOR, &KEY_MINOR),
        None => (None, None),
    }
}

/// Сопоставление хромы с 24 кандидатами (12 мажоров + 12 миноров).
/// Профили передаются параметрами — эвал сравнивает несколько наборов.
fn match_key(f: &ChromaFeatures, major: &[f64; 12], minor: &[f64; 12]) -> (Option<String>, Option<f32>) {
    let hsum: f64 = f.hpcp.iter().sum();
    // Единичные тональные кадры — скорее свип/эффект, чем тональность.
    if hsum < 1e-9 || f.tonal_frames < 6 { return (None, None); }

    let scores = key_scores(&f.hpcp, Some(&f.bass), major, minor);
    let (best, s1, s2) = top_two(&scores);
    // Слабая корреляция с любым профилем — атонально, честнее «—».
    if s1 < 0.40 { return (None, None); }

    // Согласие блоков: в скольких 10-секундных блоках побеждает та же (или
    // гармонически родственная) тональность. Почти пустые блоки не голосуют.
    let bsums: Vec<f64> = f.blocks.iter().map(|b| b.iter().sum()).collect();
    let mean_b = bsums.iter().sum::<f64>() / bsums.len().max(1) as f64;
    let mut agree = 0.0f64;
    let mut voted = 0usize;
    for (b, &bsum) in f.blocks.iter().zip(&bsums) {
        if bsum <= 0.0 || bsum < mean_b * 0.1 { continue; }
        let (bb, _, _) = top_two(&key_scores(b, None, major, minor));
        voted += 1;
        agree += related_weight(best, bb);
    }
    let agreement = if voted > 0 { agree / voted as f64 } else { 0.5 };

    // Отрыв 0.20 корреляции от ближайшего конкурента ≈ уверенный случай.
    let margin = ((s1 - s2) / 0.20).clamp(0.0, 1.0);
    let conf = (0.6 * margin + 0.4 * agreement).clamp(0.0, 1.0) as f32;

    let root = best % 12;
    let label = format!("{}{}", NOTE_NAMES[root], if best >= 12 { "min" } else { "maj" });
    (Some(label), Some(conf))
}

/// Корреляции хромы с 24 профилями; индекс: 0..12 мажоры, 12..24 миноры.
/// `bass` (если задана) добавляет кандидатам бонус за тонику в басу.
fn key_scores(
    chroma: &[f64; 12],
    bass: Option<&[f64; 12]>,
    major: &[f64; 12],
    minor: &[f64; 12],
) -> [f64; 24] {
    let bass_sum = bass.map(|b| b.iter().sum::<f64>()).unwrap_or(0.0);
    let ch: Vec<f64> = chroma.to_vec();
    let mut out = [0.0f64; 24];
    for root in 0..12usize {
        let prof_maj: Vec<f64> = (0..12).map(|p| major[(p + 12 - root) % 12]).collect();
        let prof_min: Vec<f64> = (0..12).map(|p| minor[(p + 12 - root) % 12]).collect();
        let bonus = match bass {
            Some(b) if bass_sum > 1e-9 => BASS_BONUS * b[root] / bass_sum,
            _ => 0.0,
        };
        // Режим решает терция: профильная корреляция различает мажор/минор
        // слабо (наборы нот почти совпадают), сравниваем терции напрямую.
        let m3 = chroma[(root + 4) % 12];
        let mi3 = chroma[(root + 3) % 12];
        let third = MODE_BONUS * (m3 - mi3) / (m3 + mi3 + 1e-12);
        out[root] = pearson_correlation(&ch, &prof_maj) + bonus + third;
        out[12 + root] = pearson_correlation(&ch, &prof_min) + bonus - third;
    }
    out
}

/// Индексы и значения двух лучших оценок: (best_idx, best, второе место).
fn top_two(scores: &[f64; 24]) -> (usize, f64, f64) {
    let mut bi = 0usize;
    for i in 1..24 {
        if scores[i] > scores[bi] { bi = i; }
    }
    let mut s2 = f64::NEG_INFINITY;
    for (i, &s) in scores.iter().enumerate() {
        if i != bi && s > s2 { s2 = s; }
    }
    (bi, scores[bi], s2)
}

/// Вес родства тональностей для голосования блоков: 1 — та же, 0.5 — квинтовый
/// сосед или относительная, 0 — прочее. Индексы в формате key_scores.
fn related_weight(a: usize, b: usize) -> f64 {
    if a == b { return 1.0; }
    let (ra, ma) = (a % 12, a >= 12);
    let (rb, mb) = (b % 12, b >= 12);
    if ma == mb && (rb == (ra + 7) % 12 || rb == (ra + 5) % 12) { return 0.5; }
    let rel = if ma { (ra + 3) % 12 } else { (ra + 9) % 12 };
    if ma != mb && rb == rel { return 0.5; }
    0.0
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

/// Извлекает BPM из имени файла. Число рядом со словом «bpm» (40–300) —
/// приоритетный источник; иначе первое отдельное 2–3-значное число 60–260.
/// Зеркалит семантику extractBPMFromName в Go-бэкенде (midi_extract.go).
fn bpm_from_filename(name: &str) -> Option<f32> {
    let lower = name.to_ascii_lowercase();
    let b = lower.as_bytes();

    // Числовые токены (start, end, value); ряды длиннее 3 цифр (44100, 2024…)
    // и короче 2 — не BPM.
    let mut tokens: Vec<(usize, usize, u32)> = Vec::new();
    let mut i = 0usize;
    while i < b.len() {
        if b[i].is_ascii_digit() {
            let s = i;
            while i < b.len() && b[i].is_ascii_digit() { i += 1; }
            if (2..=3).contains(&(i - s)) {
                if let Ok(v) = lower[s..i].parse::<u32>() {
                    tokens.push((s, i, v));
                }
            }
        } else {
            i += 1;
        }
    }

    let is_sep = |c: u8| matches!(c, b' ' | b'-' | b'_' | b'.');

    // 1. Число, примыкающее к «bpm»: "140bpm", "bpm 140", "140_bpm"…
    for &(s, e, v) in &tokens {
        if !(40..=300).contains(&v) { continue; }
        let mut after = e;
        while after < b.len() && is_sep(b[after]) { after += 1; }
        if lower[after..].starts_with("bpm") { return Some(v as f32); }
        let mut before = s;
        while before > 0 && is_sep(b[before - 1]) { before -= 1; }
        if lower[..before].ends_with("bpm") { return Some(v as f32); }
    }

    // 2. Первое отдельное число в правдоподобном диапазоне.
    for &(_, _, v) in &tokens {
        if (60..=260).contains(&v) { return Some(v as f32); }
    }
    None
}

// ── Главная функция анализа ───────────────────────────────────────────────────

/// Быстрые метаданные из заголовка + ФС — без декодирования сэмплов.
/// UI показывает их мгновенно (partial=true), пока DSP считает пики/BPM/key.
pub fn probe_quick(path: &str) -> AudioMeta {
    let name = Path::new(path)
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or(path)
        .to_string();

    let fs_meta = fs::metadata(path).ok();
    let file_size_bytes = fs_meta.as_ref().map(|m| m.len()).unwrap_or(0);
    let created_at = fs_meta
        .and_then(|m| m.created().ok().or_else(|| m.modified().ok()))
        .and_then(|t| t.duration_since(SystemTime::UNIX_EPOCH).ok())
        .map(|d| d.as_secs());

    let (format, duration_s, sample_rate, channels, bit_depth, error) =
        match probe_container(path) {
            Ok((f, d, sr, ch, bd)) => (f, d, sr, ch, bd, None),
            Err(e) => (String::new(), 0.0, 0, 0, None, Some(e)),
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
        bpm: None,
        key: None,
        key_confidence: None,
        peaks: vec![],
        created_at,
        error,
        partial: Some(true),
        fingerprint: String::new(),
    }
}

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
            created_at, error: Some(e), partial: None, fingerprint: String::new(),
        },
    };

    let n_frames = if duration_s > 0.0 { (duration_s * sample_rate as f64) as u64 } else { 0 };

    // 2. Один проход декодера: пики волны + моно-окно из середины для DSP.
    //    Окно 240 с: тональности нужен весь трек (стабильнее), BPM по-прежнему
    //    считается на центральных 50 секундах этого же буфера.
    const TARGET_SR: u32 = 11025;
    const BPM_WINDOW_S: f64 = 50.0;
    const KEY_WINDOW_S: f64 = 240.0;
    let streamed = stream_peaks_and_dsp(path, n_frames, 4096, TARGET_SR, KEY_WINDOW_S, duration_s);
    let dsp = &streamed.dsp_mono;
    let dsp_sr = streamed.dsp_sr;

    // 3a. BPM — центральный 50-секундный срез (прежняя семантика окна).
    let bpm_n = (BPM_WINDOW_S * dsp_sr as f64) as usize;
    let bpm_slice = if dsp.len() > bpm_n {
        let s = (dsp.len() - bpm_n) / 2;
        &dsp[s..s + bpm_n]
    } else {
        &dsp[..]
    };
    let bpm = compute_bpm(bpm_slice, dsp_sr);

    // 3b. Тональность — HPCP по всему буферу.
    let (key, key_confidence) = detect_key(dsp, dsp_sr);

    // 3c. Перцептивный отпечаток для акустического дедупа (тот же моно-буфер).
    let fp = fingerprint(dsp, dsp_sr);

    // Явный BPM в имени файла — самый надёжный источник: продюсеры подписывают
    // темп («Beat 140bpm», «trap_155_dark»), а DSP-оценка на халфтайм-битах
    // склонна к октавным ошибкам. Имя переопределяет анализ.
    let bpm = bpm_from_filename(&name).or(bpm);

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
        peaks: streamed.peaks,
        created_at,
        error: None,
        partial: None,
        fingerprint: fp,
    }
}

/// Per-phase timings in milliseconds for one file (analysis benchmark only).
#[derive(Default, Clone, Copy)]
pub struct PhaseMs {
    pub probe: f64,
    /// decode + waveform peaks + DSP mono buffer (single streaming pass).
    pub decode: f64,
    pub bpm: f64,
    pub key: f64,
}

/// Mirror of `analyze_one` that times each phase. Used by the bench to find the
/// dominant cost before optimizing.
pub fn profile_one(path: &str) -> PhaseMs {
    use std::time::Instant;
    let mut t = PhaseMs::default();

    let s = Instant::now();
    let probed = probe_container(path);
    t.probe = s.elapsed().as_secs_f64() * 1000.0;
    let (_, duration_s, sample_rate, _, _) = match probed {
        Ok(v) => v,
        Err(_) => return t,
    };
    let n_frames = if duration_s > 0.0 { (duration_s * sample_rate as f64) as u64 } else { 0 };

    const TARGET_SR: u32 = 11025;
    const BPM_WINDOW_S: f64 = 50.0;
    const KEY_WINDOW_S: f64 = 240.0;

    let s = Instant::now();
    let streamed = stream_peaks_and_dsp(path, n_frames, 4096, TARGET_SR, KEY_WINDOW_S, duration_s);
    t.decode = s.elapsed().as_secs_f64() * 1000.0;
    let dsp = &streamed.dsp_mono;
    let dsp_sr = streamed.dsp_sr;

    let bpm_n = (BPM_WINDOW_S * dsp_sr as f64) as usize;
    let bpm_slice = if dsp.len() > bpm_n {
        let a = (dsp.len() - bpm_n) / 2;
        &dsp[a..a + bpm_n]
    } else {
        &dsp[..]
    };
    let s = Instant::now();
    let _ = compute_bpm(bpm_slice, dsp_sr);
    t.bpm = s.elapsed().as_secs_f64() * 1000.0;

    let s = Instant::now();
    let _ = detect_key(dsp, dsp_sr);
    t.key = s.elapsed().as_secs_f64() * 1000.0;

    t
}

// ── Перцептивный отпечаток (акустический дедуп) ───────────────────────────────

const FP_TIME_BINS: usize = 16;
const FP_BANDS: usize = 32;

/// 496-битный перцептивный отпечаток моно-буфера: 16 временных сегментов ×
/// сравнение 32 лог-полос спектра (band[b] > band[b+1] → бит). Инвариантен к
/// усилению (сравнение соседних полос + монотонность log1p), устойчив к
/// ре-энкоду; близость измеряется расстоянием Хэмминга. Порт audio.perceptualHash.
pub fn fingerprint(mono: &[f32], sr: u32) -> String {
    use rustfft::{num_complex::Complex, FftPlanner};
    if mono.is_empty() || sr == 0 {
        return String::new();
    }
    let mut seg_len = mono.len() / FP_TIME_BINS;
    if seg_len < 64 {
        seg_len = mono.len();
    }
    let win = next_pow2(seg_len).min(8192);
    if win == 0 {
        return String::new();
    }
    let w = hann(win);
    let mut planner = FftPlanner::<f32>::new();
    let fft = planner.plan_fft_forward(win);
    let mut buf = vec![Complex::new(0.0f32, 0.0); win];
    let mut grid = vec![[0f64; FP_BANDS]; FP_TIME_BINS];

    for (t, row) in grid.iter_mut().enumerate() {
        let mut start = t * seg_len;
        if start + win > mono.len() {
            start = mono.len().saturating_sub(win);
        }
        for c in buf.iter_mut() {
            *c = Complex::new(0.0, 0.0);
        }
        let end = (start + win).min(mono.len());
        for i in 0..end - start {
            buf[i].re = mono[start + i] * w[i] as f32;
        }
        fft.process(&mut buf);
        log_bands_into(&buf, win, sr, row);
    }

    let total_bits = FP_TIME_BINS * (FP_BANDS - 1);
    let mut bits = vec![0u8; total_bits.div_ceil(8)];
    let mut bit_idx = 0;
    for row in &grid {
        for b in 0..FP_BANDS - 1 {
            if row[b] > row[b + 1] {
                bits[bit_idx / 8] |= 1 << (bit_idx % 8);
            }
            bit_idx += 1;
        }
    }
    to_hex(&bits)
}

fn log_bands_into(spec: &[rustfft::num_complex::Complex<f32>], win: usize, sr: u32, dst: &mut [f64; FP_BANDS]) {
    let n = dst.len();
    let half = win / 2;
    let min_f = 80.0f64;
    let mut max_f = sr as f64 / 2.0;
    if max_f <= min_f {
        max_f = min_f * 2.0;
    }
    let log_min = min_f.ln();
    let log_max = max_f.ln();
    let bin_hz = sr as f64 / win as f64;
    for v in dst.iter_mut() {
        *v = 0.0;
    }
    for k in 1..half {
        let freq = k as f64 * bin_hz;
        if freq < min_f || freq > max_f {
            continue;
        }
        let mag = (spec[k].re as f64).hypot(spec[k].im as f64);
        let mut idx = (n as f64 * (freq.ln() - log_min) / (log_max - log_min)) as isize;
        idx = idx.clamp(0, n as isize - 1);
        dst[idx as usize] += mag * mag;
    }
    for v in dst.iter_mut() {
        *v = (1.0 + *v).ln();
    }
}

fn next_pow2(n: usize) -> usize {
    let mut p = 1usize;
    while p < n {
        p <<= 1;
    }
    p
}

fn hann(n: usize) -> Vec<f64> {
    if n <= 1 {
        return vec![1.0; n.max(1)];
    }
    (0..n)
        .map(|i| 0.5 - 0.5 * (2.0 * std::f64::consts::PI * i as f64 / (n as f64 - 1.0)).cos())
        .collect()
}

/// Расстояние Хэмминга между двумя hex-отпечатками равной длины; -1 если
/// несопоставимы (пустые или разной длины). Порт audio.HammingHex.
pub fn hamming_hex(a: &str, b: &str) -> i32 {
    if a.is_empty() || b.is_empty() || a.len() != b.len() {
        return -1;
    }
    let (Some(ab), Some(bb)) = (from_hex(a), from_hex(b)) else {
        return -1;
    };
    ab.iter().zip(&bb).map(|(x, y)| (x ^ y).count_ones() as i32).sum()
}

fn to_hex(bytes: &[u8]) -> String {
    const H: &[u8; 16] = b"0123456789abcdef";
    let mut s = String::with_capacity(bytes.len() * 2);
    for &b in bytes {
        s.push(H[(b >> 4) as usize] as char);
        s.push(H[(b & 0xf) as usize] as char);
    }
    s
}

fn from_hex(s: &str) -> Option<Vec<u8>> {
    if s.len() % 2 != 0 {
        return None;
    }
    let b = s.as_bytes();
    let val = |c: u8| -> Option<u8> {
        match c {
            b'0'..=b'9' => Some(c - b'0'),
            b'a'..=b'f' => Some(c - b'a' + 10),
            b'A'..=b'F' => Some(c - b'A' + 10),
            _ => None,
        }
    };
    let mut out = Vec::with_capacity(s.len() / 2);
    let mut i = 0;
    while i < b.len() {
        out.push((val(b[i])? << 4) | val(b[i + 1])?);
        i += 2;
    }
    Some(out)
}

#[cfg(test)]
mod fp_tests {
    use super::*;

    #[test]
    fn hamming_hex_basics() {
        assert_eq!(hamming_hex("ff", "ff"), 0);
        assert_eq!(hamming_hex("ff", "fe"), 1);
        assert_eq!(hamming_hex("00", "ff"), 8);
        assert_eq!(hamming_hex("", "ff"), -1);
        assert_eq!(hamming_hex("ff", "ffff"), -1); // length mismatch
    }

    fn sine(f: f32, sr: u32, n: usize) -> Vec<f32> {
        (0..n)
            .map(|i| (2.0 * std::f32::consts::PI * f * i as f32 / sr as f32).sin())
            .collect()
    }

    #[test]
    fn fingerprint_deterministic_gain_invariant_and_discriminative() {
        let sr = 11025u32;
        let a = sine(440.0, sr, sr as usize * 3);
        let fp_a = fingerprint(&a, sr);
        assert_eq!(fp_a.len(), 124); // 496 bits = 62 bytes = 124 hex chars

        // Identical input → distance 0.
        assert_eq!(hamming_hex(&fp_a, &fingerprint(&a, sr)), 0);

        // Gain change → comparative bands + monotone log1p → still 0.
        let quiet: Vec<f32> = a.iter().map(|s| s * 0.3).collect();
        assert_eq!(hamming_hex(&fp_a, &fingerprint(&quiet, sr)), 0);

        // Clearly different tone → large distance.
        let b = sine(1500.0, sr, sr as usize * 3);
        let d = hamming_hex(&fp_a, &fingerprint(&b, sr));
        assert!(d > 20, "distinct tones should differ, got {d}");
    }
}

// ── Кодирование WAV (для player_decode_to_wav) ────────────────────────────────

pub fn encode_wav(samples: &[f32], sr: u32, channels: u32) -> Vec<u8> {
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

pub fn scan_dir_recursive(dir: &str, results: &mut Vec<String>) {
    scan_dir_bounded(dir, results, 0);
}

/// Обход с двумя предохранителями: symlink/junction не раскрываются (цикл из
/// джанкшенов = бесконечная рекурсия = stack overflow и мгновенная смерть
/// процесса), глубина ограничена — глубже реальные библиотеки сэмплов не бывают.
const MAX_SCAN_DEPTH: usize = 64;

fn scan_dir_bounded(dir: &str, results: &mut Vec<String>, depth: usize) {
    if depth > MAX_SCAN_DEPTH {
        return;
    }
    let Ok(entries) = fs::read_dir(dir) else { return };
    for entry in entries.flatten() {
        // file_type() берётся из данных листинга (без follow): реперс-поинты
        // (symlink/junction) видны как is_symlink — их не раскрываем.
        let Ok(ft) = entry.file_type() else { continue };
        if ft.is_symlink() {
            continue;
        }
        let path = entry.path();
        if ft.is_dir() {
            scan_dir_bounded(&path.to_string_lossy(), results, depth + 1);
        } else if is_audio_ext(&path) {
            if let Some(s) = path.to_str() { results.push(s.to_string()); }
        }
    }
}


#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};

    /// Клик-трек: короткий широкополосный затухающий клик на каждой доле.
    fn click_track(bpm: f32, sr: u32, secs: f32) -> Vec<f32> {
        let n = (sr as f32 * secs) as usize;
        let period = 60.0 / bpm * sr as f32;
        let mut v = vec![0.0f32; n];
        let mut t = 0.0f32;
        while (t as usize) < n {
            let start = t as usize;
            for k in 0..64usize.min(n - start) {
                let env = 1.0 - k as f32 / 64.0;
                v[start + k] += env * if k % 2 == 0 { 1.0 } else { -1.0 };
            }
            t += period;
        }
        v
    }

    /// Синтетический тональный сигнал: ноты гаммы root в четырёх октавах от C2
    /// (есть «бас» для басовой хромы), тоника и квинта громче остальных
    /// ступеней. detune_cents расстраивает весь сигнал целиком — имитация
    /// не-A440 строя.
    fn tonal_signal(root_pc: usize, minor: bool, sr: u32, secs: f32, detune_cents: f32) -> Vec<f32> {
        let degrees: &[usize] = if minor { &[0, 2, 3, 5, 7, 8, 10] } else { &[0, 2, 4, 5, 7, 9, 11] };
        let weights: &[f32] = &[1.0, 0.35, 0.6, 0.4, 0.85, 0.45, 0.35];
        let n = (sr as f32 * secs) as usize;
        let mut v = vec![0.0f32; n];
        for (di, &deg) in degrees.iter().enumerate() {
            for oct in 0..4usize {
                let midi = 36 + oct * 12 + (root_pc + deg) % 12;
                let freq = 440.0f32 * 2f32.powf((midi as f32 - 69.0 + detune_cents / 100.0) / 12.0);
                let w = weights[di] / (1.0 + oct as f32);
                let step = 2.0 * std::f32::consts::PI * freq / sr as f32;
                for (i, x) in v.iter_mut().enumerate() {
                    *x += w * (step * i as f32).sin();
                }
            }
        }
        let m = v.iter().fold(0.0f32, |a, &x| a.max(x.abs()));
        if m > 0.0 { for x in &mut v { *x /= m * 2.0; } }
        v
    }

    #[test]
    fn click_track_bpm_detected_accurately() {
        for &bpm in &[90.0f32, 120.0, 140.0, 160.0, 174.0] {
            let mono = click_track(bpm, 11025, 30.0);
            let got = compute_bpm(&mono, 11025).unwrap_or(0.0);
            assert!(
                (got - bpm).abs() / bpm < 0.02,
                "bpm {bpm}: detected {got}"
            );
        }
    }

    #[test]
    fn silence_gives_no_bpm() {
        let mono = vec![0.0f32; 11025 * 10];
        assert_eq!(compute_bpm(&mono, 11025), None);
    }

    /// Фиксирует корректность имён тональностей и формат меток Xmaj/Xmin.
    #[test]
    fn key_detection_labels_are_correct() {
        for &(root, label) in &[(0usize, "Cmaj"), (7, "Gmaj"), (2, "Dmaj"), (9, "Amaj"), (11, "Bmaj")] {
            let mono = tonal_signal(root, false, 11025, 12.0, 0.0);
            let (key, conf) = detect_key(&mono, 11025);
            assert_eq!(key.as_deref(), Some(label), "major root {root}, conf {conf:?}");
        }
        for &(root, label) in &[(9usize, "Amin"), (4, "Emin"), (6, "F#min")] {
            let mono = tonal_signal(root, true, 11025, 12.0, 0.0);
            let (key, conf) = detect_key(&mono, 11025);
            assert_eq!(key.as_deref(), Some(label), "minor root {root}, conf {conf:?}");
        }
    }

    /// Глобальный детюн (не-A440 строй) не должен менять ответ: оценка строя
    /// выравнивает сетку перед раскладкой по питч-классам. Старый детектор
    /// этот кейс проваливал — энергия уезжала в соседние питч-классы.
    #[test]
    fn key_detection_survives_detune() {
        for &cents in &[-30.0f32, 30.0] {
            let mono = tonal_signal(9, true, 11025, 12.0, cents);
            let (key, conf) = detect_key(&mono, 11025);
            assert_eq!(key.as_deref(), Some("Amin"), "detune {cents}c, conf {conf:?}");
        }
    }

    /// Громкая широкополосная перкуссия поверх тональной основы не должна
    /// менять ответ: ударные кадры гейтятся плоскостностью, а шумовой пол
    /// не проходит порог пиков. Старый детектор ссыпал её в хрому целиком.
    #[test]
    fn key_detection_ignores_percussion() {
        let tonal = tonal_signal(7, false, 11025, 12.0, 0.0);
        let clicks = click_track(140.0, 11025, 12.0);
        let mono: Vec<f32> = tonal.iter().zip(&clicks).map(|(&t, &c)| t + 1.2 * c).collect();
        let (key, conf) = detect_key(&mono, 11025);
        assert_eq!(key.as_deref(), Some("Gmaj"), "conf {conf:?}");
    }

    /// Белый шум атонален — ответа быть не должно.
    #[test]
    fn noise_gives_no_key() {
        let mut state = 0x12345678u32;
        let mono: Vec<f32> = (0..11025 * 12)
            .map(|_| {
                state = state.wrapping_mul(1664525).wrapping_add(1013904223);
                (state >> 8) as f32 / 8388608.0 - 1.0
            })
            .collect();
        let (key, _) = detect_key(&mono, 11025);
        assert_eq!(key, None);
    }

    #[test]
    fn filename_bpm_extraction() {
        assert_eq!(bpm_from_filename("Dark Trap Beat 140bpm.wav"), Some(140.0));
        assert_eq!(bpm_from_filename("BPM 95 - smooth.mp3"), Some(95.0));
        assert_eq!(bpm_from_filename("140_bpm_melody.wav"), Some(140.0));
        assert_eq!(bpm_from_filename("trap_155_dark.wav"), Some(155.0));
        assert_eq!(bpm_from_filename("808 Kick.wav"), None); // 808 вне диапазона
        assert_eq!(bpm_from_filename("kick_03.wav"), None); // < 60 — не темп
        assert_eq!(bpm_from_filename("sample 44100hz.wav"), None); // длинные числа не берём
        assert_eq!(bpm_from_filename("Snare Tight.wav"), None);
    }

    // ── Эвал на реальных файлах ───────────────────────────────────────────────
    //
    // Запуск: KEY_EVAL_DIR=<папка> cargo test key_eval -- --ignored --nocapture
    // Если в именах файлов есть тональности («Amin», «F#m», «C maj») — считает
    // точность по MIREX. Без меток — самосогласованность (половины трека) и
    // таблицу «старый vs новый».

    // Профили для сравнения вариантов в эвале. KS_* совпадают с продакшеном
    // (KEY_*) и используются легаси-детектором; Sha'ath и Temperley — альтернативы.
    const KS_MAJOR: [f64; 12] = [6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88];
    const KS_MINOR: [f64; 12] = [6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17];
    const SHAATH_MAJOR: [f64; 12] = [6.6, 2.0, 3.5, 2.3, 4.6, 4.0, 2.5, 5.2, 2.4, 3.7, 2.3, 3.4];
    const SHAATH_MINOR: [f64; 12] = [6.5, 2.7, 3.5, 5.4, 2.6, 3.5, 2.5, 5.2, 4.0, 2.7, 4.3, 3.2];
    const TEMPERLEY_MAJOR: [f64; 12] = [5.0, 2.0, 3.5, 2.0, 4.5, 4.0, 2.0, 4.5, 2.0, 3.5, 1.5, 4.0];
    const TEMPERLEY_MINOR: [f64; 12] = [5.0, 2.0, 3.5, 4.5, 2.0, 4.0, 2.0, 4.5, 3.5, 2.0, 1.5, 4.0];

    /// Прежний детектор целиком (для сравнения): вся энергия бинов
    /// 27.5–4200 Гц в питч-классы по STFT 2048/256, KS-профили, порог 0.55.
    fn legacy_key(mono: &[f32], sr: u32) -> Option<(usize, bool)> {
        use rustfft::{num_complex::Complex, FftPlanner};
        if (mono.len() as u32) < sr { return None; }
        let win = 2048usize;
        let hop = 256usize;
        let half = win / 2 + 1;
        let hann: Vec<f32> = (0..win)
            .map(|i| 0.5 * (1.0 - (2.0 * std::f32::consts::PI * i as f32 / (win - 1) as f32).cos()))
            .collect();
        let mut planner = FftPlanner::<f32>::new();
        let fft = planner.plan_fft_forward(win);
        let mut scratch = vec![Complex::new(0.0f32, 0.0); fft.get_inplace_scratch_len()];
        let mut buf = vec![Complex::new(0.0f32, 0.0); win];
        let freq_per_bin = sr as f64 / win as f64;
        let pc_table: Vec<i8> = (0..half)
            .map(|k| {
                let f = k as f64 * freq_per_bin;
                if !(27.5..=4200.0).contains(&f) { return -1; }
                let midi = 69.0 + 12.0 * (f / 440.0).log2();
                ((midi.round() as i32).rem_euclid(12)) as i8
            })
            .collect();
        let mut acc = [0.0f64; 12];
        for start in (0..mono.len().saturating_sub(win)).step_by(hop) {
            for i in 0..win {
                buf[i] = Complex::new(mono[start + i] * hann[i], 0.0);
            }
            fft.process_with_scratch(&mut buf, &mut scratch);
            for k in 0..half {
                let pc = pc_table[k];
                if pc >= 0 { acc[pc as usize] += buf[k].norm() as f64; }
            }
        }
        let sum: f64 = acc.iter().sum();
        if sum < 1e-12 { return None; }
        let ch: Vec<f64> = acc.iter().map(|&x| x / sum).collect();
        let mut best = (f64::NEG_INFINITY, 0usize, false);
        for root in 0..12usize {
            let maj: Vec<f64> = (0..12).map(|p| KS_MAJOR[(p + 12 - root) % 12]).collect();
            let min_: Vec<f64> = (0..12).map(|p| KS_MINOR[(p + 12 - root) % 12]).collect();
            let rm = pearson_correlation(&ch, &maj);
            if rm > best.0 { best = (rm, root, false); }
            let rn = pearson_correlation(&ch, &min_);
            if rn > best.0 { best = (rn, root, true); }
        }
        if best.0 < 0.55 { return None; }
        Some((best.1, best.2))
    }

    /// (pc, minor) из метки детектора вида «F#min» / «Cmaj».
    fn parse_label(label: &str) -> Option<(usize, bool)> {
        let (root, minor) = if let Some(r) = label.strip_suffix("min") {
            (r, true)
        } else if let Some(r) = label.strip_suffix("maj") {
            (r, false)
        } else {
            return None;
        };
        NOTE_NAMES.iter().position(|&n| n == root).map(|pc| (pc, minor))
    }

    /// Тональность из имени файла (ground truth сэмпл-паков): «Amin», «F#m»,
    /// «Db minor», «C maj», «Am»… None — метки нет или несколько противоречат.
    fn key_from_filename(stem: &str) -> Option<(usize, bool)> {
        let lower = stem.to_ascii_lowercase();
        let toks: Vec<&str> = lower
            .split(|c: char| !(c.is_ascii_alphanumeric() || c == '#'))
            .filter(|t| !t.is_empty())
            .collect();
        // Нота: [a-g] + опциональный # или b. Возвращает (pc, съедено символов).
        fn note_pc(s: &str) -> Option<(usize, usize)> {
            let b = s.as_bytes();
            let base = match b.first()? {
                b'c' => 0, b'd' => 2, b'e' => 4, b'f' => 5, b'g' => 7, b'a' => 9, b'b' => 11,
                _ => return None,
            };
            match b.get(1) {
                Some(b'#') => Some(((base + 1) % 12, 2)),
                Some(b'b') => Some(((base + 11) % 12, 2)),
                _ => Some((base, 1)),
            }
        }
        let mut found: Vec<(usize, bool)> = Vec::new();
        for (i, t) in toks.iter().enumerate() {
            let Some((pc, used)) = note_pc(t) else { continue };
            let quality = match &t[used..] {
                "m" | "min" | "minor" => Some(true),
                "maj" | "major" => Some(false),
                // Качество может быть отдельным токеном: «F# min», «C major».
                "" => match toks.get(i + 1).copied() {
                    Some("m") | Some("min") | Some("minor") => Some(true),
                    Some("maj") | Some("major") => Some(false),
                    _ => None,
                },
                _ => None,
            };
            if let Some(minor) = quality { found.push((pc, minor)); }
        }
        let first = *found.first()?;
        if found.iter().all(|&k| k == first) { Some(first) } else { None }
    }

    /// Классификация ошибки по MIREX + вес для weighted score.
    fn mirex_class(gt: (usize, bool), est: (usize, bool)) -> (&'static str, f64) {
        if est == gt { return ("точно", 1.0); }
        let (gr, gm) = gt;
        let (er, em) = est;
        if em == gm && (er == (gr + 7) % 12 || er == (gr + 5) % 12) { return ("квинта", 0.5); }
        let rel = if gm { (gr + 3) % 12 } else { (gr + 9) % 12 };
        if em != gm && er == rel { return ("относительная", 0.3); }
        if em != gm && er == gr { return ("параллельная", 0.2); }
        ("мимо", 0.0)
    }

    fn fmt_key(k: Option<(usize, bool)>) -> String {
        match k {
            Some((pc, minor)) => format!("{}{}", NOTE_NAMES[pc], if minor { "m" } else { "" }),
            None => "—".to_string(),
        }
    }

    #[test]
    #[ignore] // ручной запуск: см. комментарий к разделу
    fn key_eval_folder() {
        use rayon::prelude::*;

        let dir = std::env::var("KEY_EVAL_DIR").unwrap_or_else(|_| r"E:\BEATS\MP3s".to_string());
        let mut files = Vec::new();
        scan_dir_recursive(&dir, &mut files);
        files.sort();
        // KEY_EVAL_GT_ONLY=1 — анализировать только файлы с тональностью в
        // имени: быстрая итерация при калибровке (44 файла вместо 1105).
        if std::env::var("KEY_EVAL_GT_ONLY").is_ok() {
            files.retain(|p| {
                Path::new(p)
                    .file_stem()
                    .and_then(|s| s.to_str())
                    .and_then(key_from_filename)
                    .is_some()
            });
        }
        println!("== KEY EVAL: {dir} ==");
        println!("аудиофайлов найдено: {}", files.len());
        if files.is_empty() { return; }

        struct Row {
            name: String,
            gt: Option<(usize, bool)>,
            old: Option<(usize, bool)>,
            new_prod: Option<(usize, bool)>,
            new_shaath: Option<(usize, bool)>,
            new_temperley: Option<(usize, bool)>,
            conf: f32,
            old_halves: (Option<(usize, bool)>, Option<(usize, bool)>),
            new_halves: (Option<(usize, bool)>, Option<(usize, bool)>),
        }

        fn central_slice(m: &[f32], sr: u32, secs: f64) -> &[f32] {
            let n = (secs * sr as f64) as usize;
            if m.len() <= n { m } else { let s = (m.len() - n) / 2; &m[s..s + n] }
        }

        let done = AtomicUsize::new(0);
        let total = files.len();
        let mut rows: Vec<Row> = files
            .par_iter()
            .filter_map(|path| {
                let stem = Path::new(path).file_stem()?.to_str()?.to_string();
                let (_, duration_s, sr0, _, _) = probe_container(path).ok()?;
                let n_frames = if duration_s > 0.0 { (duration_s * sr0 as f64) as u64 } else { 0 };
                let streamed = stream_peaks_and_dsp(path, n_frames, 64, 11025, 240.0, duration_s);
                let n = done.fetch_add(1, Ordering::Relaxed) + 1;
                if n % 50 == 0 { eprintln!("  …{n}/{total}"); }
                let dsp = &streamed.dsp_mono;
                let sr = streamed.dsp_sr;
                if dsp.len() < sr as usize * 4 { return None; }

                // Полный трек: старый и новый (все наборы профилей на одной хроме).
                let old = legacy_key(central_slice(dsp, sr, 50.0), sr);
                let feats = chroma_features(dsp, sr);
                let by = |maj: &[f64; 12], min: &[f64; 12]| -> (Option<(usize, bool)>, f32) {
                    match &feats {
                        Some(f) => {
                            let (k, c) = match_key(f, maj, min);
                            (k.as_deref().and_then(parse_label), c.unwrap_or(0.0))
                        }
                        None => (None, 0.0),
                    }
                };
                let (new_prod, conf) = by(&KEY_MAJOR, &KEY_MINOR);
                let (new_shaath, _) = by(&SHAATH_MAJOR, &SHAATH_MINOR);
                let (new_temperley, _) = by(&TEMPERLEY_MAJOR, &TEMPERLEY_MINOR);

                // Самосогласованность: детектор отдельно на половинах буфера.
                let (a, b) = dsp.split_at(dsp.len() / 2);
                let old_halves = (
                    legacy_key(central_slice(a, sr, 50.0), sr),
                    legacy_key(central_slice(b, sr, 50.0), sr),
                );
                let nh = |m: &[f32]| detect_key(m, sr).0.as_deref().and_then(parse_label);
                let new_halves = (nh(a), nh(b));

                Some(Row {
                    name: stem.clone(),
                    gt: key_from_filename(&stem),
                    old,
                    new_prod,
                    new_shaath,
                    new_temperley,
                    conf,
                    old_halves,
                    new_halves,
                })
            })
            .collect();
        rows.sort_by(|a, b| a.name.cmp(&b.name));
        println!("проанализировано: {}", rows.len());

        // ── Точность по меткам из имён (если они есть) ──
        let gt_rows: Vec<&Row> = rows.iter().filter(|r| r.gt.is_some()).collect();
        if gt_rows.is_empty() {
            println!("\nметок тональности в именах файлов нет — точность по GT пропущена");
        } else {
            println!("\n-- Точность по меткам в именах: {} файлов --", gt_rows.len());
            println!(
                "{:<16} {:>6} {:>7} {:>6} {:>6} {:>5} {:>5} | {:>8}",
                "алгоритм", "точно", "квинта", "относ", "парал", "мимо", "нет", "weighted"
            );
            let variants: Vec<(&str, Box<dyn Fn(&Row) -> Option<(usize, bool)>>)> = vec![
                ("old KS", Box::new(|r: &Row| r.old)),
                ("new KS (prod)", Box::new(|r: &Row| r.new_prod)),
                ("new Shaath", Box::new(|r: &Row| r.new_shaath)),
                ("new Temperley", Box::new(|r: &Row| r.new_temperley)),
            ];
            for (label, get) in &variants {
                let (mut ex, mut fi, mut re, mut pa, mut ot, mut no) = (0, 0, 0, 0, 0, 0);
                let mut wsum = 0.0f64;
                for r in &gt_rows {
                    match get(r) {
                        None => no += 1,
                        Some(est) => {
                            let (cls, w) = mirex_class(r.gt.unwrap(), est);
                            wsum += w;
                            match cls {
                                "точно" => ex += 1,
                                "квинта" => fi += 1,
                                "относительная" => re += 1,
                                "параллельная" => pa += 1,
                                _ => ot += 1,
                            }
                        }
                    }
                }
                let n = gt_rows.len() as f64;
                println!(
                    "{label:<16} {ex:>6} {fi:>7} {re:>6} {pa:>6} {ot:>5} {no:>5} | {:>7.1}%",
                    100.0 * wsum / n
                );
            }

            // Per-file разбор: направление ошибок (минор→мажор или наоборот).
            println!("\n-- GT-файлы: файл | GT | old | new (класс ошибки new) --");
            for r in &gt_rows {
                let gt = r.gt.unwrap();
                let cls = match r.new_prod {
                    Some(est) => mirex_class(gt, est).0,
                    None => "нет ответа",
                };
                let name: String = r.name.chars().take(44).collect();
                println!(
                    "{:<44} {:>4} {:>4} {:>4}  {}",
                    name,
                    fmt_key(r.gt),
                    fmt_key(r.old),
                    fmt_key(r.new_prod),
                    cls
                );
            }
        }

        // ── Самосогласованность: половины трека должны давать одну тональность ──
        println!("\n-- Самосогласованность (1-я половина трека vs 2-я) --");
        let sc = |label: &str, get: &dyn Fn(&Row) -> (Option<(usize, bool)>, Option<(usize, bool)>)| {
            let (mut both, mut same, mut related, mut diff) = (0usize, 0usize, 0usize, 0usize);
            for r in &rows {
                if let (Some(x), Some(y)) = get(r) {
                    both += 1;
                    if x == y {
                        same += 1;
                    } else if mirex_class(x, y).1 > 0.0 {
                        related += 1;
                    } else {
                        diff += 1;
                    }
                }
            }
            println!(
                "{label:<12} ответил на обеих половинах: {both}/{}  совпало: {same} ({:.1}%)  родственная: {related}  разное: {diff}",
                rows.len(),
                100.0 * same as f64 / both.max(1) as f64
            );
        };
        sc("old KS", &|r| r.old_halves);
        sc("new (prod)", &|r| r.new_halves);

        // ── Старый vs новый на полном треке ──
        println!("\n-- Старый vs новый (полный трек) --");
        let (mut same, mut fifth, mut rel, mut par, mut other) = (0, 0, 0, 0, 0);
        let (mut old_none_new_some, mut new_none) = (0, 0);
        for r in &rows {
            match (r.old, r.new_prod) {
                (Some(o), Some(n)) => match mirex_class(o, n).0 {
                    "точно" => same += 1,
                    "квинта" => fifth += 1,
                    "относительная" => rel += 1,
                    "параллельная" => par += 1,
                    _ => other += 1,
                },
                (None, Some(_)) => old_none_new_some += 1,
                (_, None) => new_none += 1,
            }
        }
        println!("совпало: {same}, квинта: {fifth}, относительная: {rel}, параллельная: {par}, прочее: {other}");
        println!("старый молчал/новый ответил: {old_none_new_some}, новый без ответа: {new_none}");

        // ── Таблица по файлам ──
        println!("\n-- Таблица (файл | old | new | conf) --");
        for r in &rows {
            let name: String = r.name.chars().take(52).collect();
            println!(
                "{:<52} {:>4} {:>4} {:>4.0}%",
                name,
                fmt_key(r.old),
                fmt_key(r.new_prod),
                r.conf * 100.0
            );
        }
    }
}

