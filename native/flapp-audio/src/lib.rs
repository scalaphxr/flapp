use std::fs::File;
use std::io::BufReader;
use std::path::Path;
use std::sync::Mutex;
use std::time::Instant;

use rodio::{Decoder, OutputStream, OutputStreamHandle, Sink};

/// Native audio player. Owns the output stream + a rodio Sink and mirrors
/// playback position with a PositionTracker (probed duration is authoritative).
pub struct Player {
    _stream: OutputStream,
    handle: OutputStreamHandle,
    sink: Option<Sink>,
    tracker: Mutex<PositionTracker>,
}

impl Player {
    pub fn new() -> anyhow::Result<Self> {
        let (_stream, handle) = OutputStream::try_default()?;
        Ok(Self { _stream, handle, sink: None, tracker: Mutex::new(PositionTracker::new(0.0)) })
    }

    /// Start playing `path`. `duration_sec` is the probed duration (from flapp-dsp),
    /// used as the authoritative scale for position/seek.
    pub fn play(&mut self, path: &Path, duration_sec: f32) -> anyhow::Result<()> {
        let sink = Sink::try_new(&self.handle)?;
        let file = BufReader::new(File::open(path)?);
        let source = Decoder::new(file)?;
        sink.append(source);
        sink.play();
        self.sink = Some(sink);
        let mut t = self.tracker.lock().unwrap();
        *t = PositionTracker::new(duration_sec);
        t.start();
        Ok(())
    }

    pub fn pause(&self) {
        if let Some(s) = &self.sink {
            s.pause();
        }
        self.tracker.lock().unwrap().pause();
    }

    pub fn resume(&self) {
        if let Some(s) = &self.sink {
            s.play();
        }
        self.tracker.lock().unwrap().start();
    }

    pub fn stop(&mut self) {
        if let Some(s) = self.sink.take() {
            s.stop();
        }
    }

    pub fn seek(&self, secs: f32) -> anyhow::Result<()> {
        if let Some(s) = &self.sink {
            // If the format/decoder can't seek, position still tracks by wall time.
            let _ = s.try_seek(std::time::Duration::from_secs_f32(secs.max(0.0)));
        }
        self.tracker.lock().unwrap().seek_to(secs);
        Ok(())
    }

    pub fn position(&self) -> f32 {
        self.tracker.lock().unwrap().position()
    }

    pub fn is_playing(&self) -> bool {
        self.sink.as_ref().map(|s| !s.is_paused() && !s.empty()).unwrap_or(false)
    }
}

/// Tracks playback position against a known (probed) duration. Position is
/// derived from an anchor offset plus elapsed wall time while playing, clamped
/// to [0, duration]. Uses probed duration, not decoder-reported (VBR mp3 lies).
pub struct PositionTracker {
    duration_sec: f32,
    anchor_sec: f32, // position at the last start/seek
    playing_since: Option<Instant>,
}

impl PositionTracker {
    pub fn new(duration_sec: f32) -> Self {
        Self { duration_sec: duration_sec.max(0.0), anchor_sec: 0.0, playing_since: None }
    }
    pub fn start(&mut self) {
        if self.playing_since.is_none() {
            self.playing_since = Some(Instant::now());
        }
    }
    pub fn pause(&mut self) {
        self.anchor_sec = self.position();
        self.playing_since = None;
    }
    pub fn seek_to(&mut self, secs: f32) {
        self.anchor_sec = secs.clamp(0.0, self.duration_sec);
        if self.playing_since.is_some() {
            self.playing_since = Some(Instant::now());
        }
    }
    pub fn position(&self) -> f32 {
        let elapsed = self.playing_since.map(|t| t.elapsed().as_secs_f32()).unwrap_or(0.0);
        (self.anchor_sec + elapsed).clamp(0.0, self.duration_sec)
    }
}

#[cfg(test)]
mod pos_tests {
    use super::*;
    use std::{thread, time::Duration};

    #[test]
    fn position_advances_from_zero_and_clamps() {
        let mut t = PositionTracker::new(2.0);
        assert_eq!(t.position(), 0.0);
        t.start();
        thread::sleep(Duration::from_millis(50));
        let p = t.position();
        assert!(p > 0.0 && p < 2.0, "expected 0<p<2, got {p}");
    }

    #[test]
    fn seek_sets_position_and_clamps_to_duration() {
        let mut t = PositionTracker::new(2.0);
        t.seek_to(1.5);
        assert!((t.position() - 1.5).abs() < 1e-6);
        t.seek_to(99.0);
        assert_eq!(t.position(), 2.0);
        t.seek_to(-5.0);
        assert_eq!(t.position(), 0.0);
    }

    #[test]
    fn pause_freezes_position() {
        let mut t = PositionTracker::new(10.0);
        t.seek_to(3.0);
        t.start();
        t.pause();
        let a = t.position();
        thread::sleep(Duration::from_millis(30));
        assert_eq!(a, t.position());
    }
}
