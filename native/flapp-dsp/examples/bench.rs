// Analysis throughput benchmark for the native DSP pipeline.
//
//   cargo run -p flapp-dsp --example bench --release -- "C:\path\to\samples"
//
// Reports: file count, cold parallel analysis (fresh cache), warm parallel
// (cache hits), and single-threaded cold, plus files/sec for each.

use std::path::PathBuf;
use std::time::Instant;

use rayon::prelude::*;

use flapp_dsp::{analyze_one, profile_one, scan_dir_recursive, AnalyzerCache};

fn main() {
    let dir = std::env::args().nth(1).expect("usage: bench <folder> [profile]");
    let profile = std::env::args().nth(2).as_deref() == Some("profile");
    if profile {
        run_profile(&dir);
        return;
    }
    let tmp = std::env::temp_dir().join(format!("flapp-bench-{}", std::process::id()));

    println!("scanning {dir} …");
    let mut paths: Vec<String> = Vec::new();
    let t = Instant::now();
    scan_dir_recursive(&dir, &mut paths);
    paths.sort();
    let scan_s = t.elapsed().as_secs_f64();
    let n = paths.len();
    println!("scan: {n} audio files in {scan_s:.3}s");
    if n == 0 {
        return;
    }

    // Cold, single-threaded (fresh cache) — baseline per-core cost.
    let cache1 = AnalyzerCache::new(Some(tmp.join("st")));
    let t = Instant::now();
    for p in &paths {
        let m = analyze_one(p);
        cache1.set(p, m);
    }
    let st_s = t.elapsed().as_secs_f64();
    report("cold single-thread", n, st_s);

    // Cold, parallel (fresh cache) — this is what the app does.
    let cache2 = AnalyzerCache::new(Some(tmp.join("mt")));
    let t = Instant::now();
    paths.par_iter().for_each(|p| {
        let m = analyze_one(p);
        cache2.set(p, m);
    });
    let mt_s = t.elapsed().as_secs_f64();
    report("cold parallel", n, mt_s);

    // Warm, parallel (cache hits) — reopening the same folder.
    let t = Instant::now();
    paths.par_iter().for_each(|p| {
        let _ = cache2.get(p);
    });
    let warm_s = t.elapsed().as_secs_f64();
    report("warm parallel (cache)", n, warm_s);

    println!("\nparallel speedup vs single-thread: {:.1}x", st_s / mt_s.max(1e-9));
    let _ = std::fs::remove_dir_all(PathBuf::from(&tmp));
}

/// Single-threaded per-phase breakdown over a subset, to locate the dominant cost.
fn run_profile(dir: &str) {
    let mut paths: Vec<String> = Vec::new();
    scan_dir_recursive(dir, &mut paths);
    paths.sort();
    let sample = paths.len().min(200);
    println!("profiling {sample} of {} files (single-thread)…", paths.len());

    let (mut probe, mut decode, mut bpm, mut key) = (0.0, 0.0, 0.0, 0.0);
    for p in paths.iter().take(sample) {
        let t = profile_one(p);
        probe += t.probe;
        decode += t.decode;
        bpm += t.bpm;
        key += t.key;
    }
    let n = sample as f64;
    let total = probe + decode + bpm + key;
    println!("\navg ms/file over {sample} files:");
    let row = |label: &str, v: f64| {
        println!("  {label:<22} {:>7.2} ms   {:>5.1}%", v / n, v / total * 100.0);
    };
    row("probe (header)", probe);
    row("decode+peaks+dsp", decode);
    row("bpm (STFT 50s)", bpm);
    row("key (HPCP full)", key);
    println!("  {:<22} {:>7.2} ms", "TOTAL", total / n);
}

fn report(label: &str, n: usize, secs: f64) {
    let per = if n > 0 { secs / n as f64 * 1000.0 } else { 0.0 };
    let fps = if secs > 0.0 { n as f64 / secs } else { f64::INFINITY };
    println!("{label:<24} {secs:>7.3}s   {fps:>8.1} files/s   {per:>6.2} ms/file");
}
