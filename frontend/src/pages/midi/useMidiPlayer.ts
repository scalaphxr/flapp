import React from "react";
import type { MidiNotesResult } from "@/shared/api/types";
import { api } from "@/shared/api/client";

interface UseMidiPlayerResult {
  isPlaying: boolean;
  isLoading: boolean;
  playheadSec: number;
  totalSec: number;
  error: string | null;
  play: () => void;
  stop: () => void;
  seek: (sec: number) => void;
}

// ── Synthetic piano buffer ──────────────────────────────────────────────────
// Module-level cache — generated once per sample rate, shared across all hooks.
let pianoCacheEntry: { sampleRate: number; buf: AudioBuffer } | null = null;

async function generatePianoBuffer(ctx: AudioContext): Promise<AudioBuffer> {
  if (pianoCacheEntry && pianoCacheEntry.sampleRate === ctx.sampleRate) {
    return pianoCacheEntry.buf;
  }

  const duration = 4.0;
  const sr = ctx.sampleRate;
  const offline = new OfflineAudioContext(1, Math.ceil(duration * sr), sr);

  // C4 = 261.63 Hz. Harmonics with individual amplitude & decay envelopes.
  // Higher partials decay faster — signature of a real piano string.
  const harmonics: [number, number, number][] = [
    // [n, peakAmp, decayEndSec]
    [1, 0.50, 3.8],
    [2, 0.30, 2.2],
    [3, 0.20, 1.4],
    [4, 0.12, 0.9],
    [5, 0.07, 0.6],
    [6, 0.04, 0.4],
    [7, 0.02, 0.25],
  ];

  for (const [n, amp, decayEnd] of harmonics) {
    const osc = offline.createOscillator();
    osc.type = "sine";
    osc.frequency.value = 261.63 * n;

    const g = offline.createGain();
    g.gain.setValueAtTime(0, 0);
    g.gain.linearRampToValueAtTime(amp, 0.006);       // fast hammer attack
    g.gain.exponentialRampToValueAtTime(amp * 0.4, 0.06); // initial drop
    g.gain.exponentialRampToValueAtTime(0.0001, decayEnd); // natural decay

    osc.connect(g);
    g.connect(offline.destination);
    osc.start(0);
    osc.stop(duration);
  }

  // Subtle "hammer" transient: short noise burst at the very start
  const noiseLen = Math.ceil(0.012 * sr);
  const noiseBuf = offline.createBuffer(1, noiseLen, sr);
  const noiseData = noiseBuf.getChannelData(0);
  for (let i = 0; i < noiseLen; i++) {
    noiseData[i] = (Math.random() * 2 - 1) * (1 - i / noiseLen) * 0.06;
  }
  const noiseSource = offline.createBufferSource();
  noiseSource.buffer = noiseBuf;
  noiseSource.connect(offline.destination);
  noiseSource.start(0);

  const buf = await offline.startRendering();
  pianoCacheEntry = { sampleRate: sr, buf };
  return buf;
}

// ── Hook ────────────────────────────────────────────────────────────────────
export function useMidiPlayer(
  clipId: string,
  notesResult: MidiNotesResult | null,
  hasSample: boolean,
  selfCut: boolean = false,
  fallbackPiano: boolean = false,
): UseMidiPlayerResult {
  const [isPlaying, setIsPlaying] = React.useState(false);
  const [isLoading, setIsLoading] = React.useState(false);
  const [playheadSec, setPlayheadSec] = React.useState(0);
  const [error, setError] = React.useState<string | null>(null);

  const audioCtxRef = React.useRef<AudioContext | null>(null);
  const sourcesRef = React.useRef<AudioBufferSourceNode[]>([]);
  const gainNodesRef = React.useRef<GainNode[]>([]);
  const bufferRef = React.useRef<{ url: string; buf: AudioBuffer } | null>(null);
  const rafRef = React.useRef<number>(0);
  const virtualStartRef = React.useRef<number>(0);

  const canPlay = hasSample || fallbackPiano;

  const totalSec = React.useMemo(() => {
    if (!notesResult?.notes.length || !notesResult.ticksPerBeat || !notesResult.bpm) return 0;
    const secPerTick = 60 / notesResult.bpm / notesResult.ticksPerBeat;
    const dur = notesResult.durationTicks > 0
      ? notesResult.durationTicks
      : notesResult.notes.reduce((m, n) => Math.max(m, n.tick + n.durationTicks), 0);
    return dur * secPerTick;
  }, [notesResult]);

  const stopSources = React.useCallback(() => {
    cancelAnimationFrame(rafRef.current);
    for (const s of sourcesRef.current) {
      try { s.stop(0); } catch { /* already stopped */ }
      try { s.disconnect(); } catch { /* already disconnected */ }
    }
    for (const g of gainNodesRef.current) {
      try { g.disconnect(); } catch { /* already disconnected */ }
    }
    sourcesRef.current = [];
    gainNodesRef.current = [];
  }, []);

  const stop = React.useCallback(() => {
    stopSources();
    setIsPlaying(false);
    setPlayheadSec(0);
  }, [stopSources]);

  React.useEffect(() => stop, [stop]);

  const playFrom = React.useCallback(async (offsetSec: number) => {
    if (!notesResult || !notesResult.notes.length || !canPlay) return;
    stopSources();
    setError(null);
    setIsLoading(true);

    try {
      if (!audioCtxRef.current || audioCtxRef.current.state === "closed") {
        audioCtxRef.current = new AudioContext();
      }
      const ctx = audioCtxRef.current;
      if (ctx.state === "suspended") await ctx.resume();

      let buf: AudioBuffer;

      if (hasSample) {
        // ── Real sample ──────────────────────────────────────────────────────
        const sampleUrl = await api.midiClipSampleUrl(clipId);
        if (!bufferRef.current || bufferRef.current.url !== sampleUrl) {
          const resp = await fetch(sampleUrl);
          if (!resp.ok) throw new Error(resp.status === 404 ? "sound_not_found" : `http_${resp.status}`);
          const arrayBuf = await resp.arrayBuffer();
          let decoded: AudioBuffer;
          try {
            decoded = await ctx.decodeAudioData(arrayBuf);
          } catch {
            throw new Error("decode_failed");
          }
          bufferRef.current = { url: sampleUrl, buf: decoded };
        }
        buf = bufferRef.current.buf;
      } else {
        // ── Synthetic piano ──────────────────────────────────────────────────
        if (!bufferRef.current || bufferRef.current.url !== "__piano__") {
          const pianoBuf = await generatePianoBuffer(ctx);
          bufferRef.current = { url: "__piano__", buf: pianoBuf };
        }
        buf = bufferRef.current.buf;
      }

      setIsLoading(false);
      setIsPlaying(true);

      const { ticksPerBeat, bpm, notes } = notesResult;
      const secPerTick = 60 / bpm / ticksPerBeat;

      const scheduleAt = ctx.currentTime + 0.05;
      virtualStartRef.current = scheduleAt - offsetSec;

      const newSources: AudioBufferSourceNode[] = [];
      const newGains: GainNode[] = [];

      const sortedNotes = selfCut
        ? [...notes].sort((a, b) => a.tick - b.tick)
        : notes;

      const activeForCut: Array<{ source: AudioBufferSourceNode; fireAt: number }> = [];

      for (const note of sortedNotes) {
        const noteAbsSec = note.tick * secPerTick;
        const noteDurSec = note.durationTicks * secPerTick;
        const noteEndSec = noteAbsSec + noteDurSec;

        if (noteEndSec <= offsetSec) continue;

        const playbackRate = Math.pow(2, (note.pitch - 60) / 12);
        const bufferOffsetSamplSec = Math.max(0, offsetSec - noteAbsSec) * playbackRate;
        const noteFireAt = scheduleAt + Math.max(0, noteAbsSec - offsetSec);

        if (selfCut) {
          for (const prev of activeForCut) {
            try { prev.source.stop(noteFireAt); } catch { /* already stopped */ }
          }
          activeForCut.length = 0;
        }

        const gainNode = ctx.createGain();
        gainNode.gain.value = note.velocity / 127;
        gainNode.connect(ctx.destination);

        const source = ctx.createBufferSource();
        source.buffer = buf;
        source.playbackRate.value = playbackRate;
        source.connect(gainNode);
        source.start(noteFireAt, bufferOffsetSamplSec);

        if (selfCut) {
          activeForCut.push({ source, fireAt: noteFireAt });
        }

        newSources.push(source);
        newGains.push(gainNode);
      }

      sourcesRef.current = newSources;
      gainNodesRef.current = newGains;

      function tick() {
        const ctx2 = audioCtxRef.current;
        if (!ctx2) return;
        const elapsed = ctx2.currentTime - virtualStartRef.current;
        setPlayheadSec(Math.max(0, elapsed));
        if (elapsed < totalSec + 0.1) {
          rafRef.current = requestAnimationFrame(tick);
        } else {
          stopSources();
          setIsPlaying(false);
          setPlayheadSec(0);
        }
      }
      rafRef.current = requestAnimationFrame(tick);
    } catch (e) {
      const msg = e instanceof Error ? e.message : "";
      if (msg === "sound_not_found") setError("Sound file not found");
      else if (msg === "decode_failed") setError("Format not supported");
      else setError("Playback error");
      console.error("[useMidiPlayer]", e);
      setIsLoading(false);
      setIsPlaying(false);
    }
  }, [clipId, notesResult, hasSample, canPlay, stopSources, totalSec]);

  const play = React.useCallback(() => playFrom(0), [playFrom]);

  const seek = React.useCallback((sec: number) => {
    const clamped = Math.max(0, Math.min(sec, totalSec));
    if (isPlaying) {
      playFrom(clamped);
    } else {
      setPlayheadSec(clamped);
    }
  }, [isPlaying, playFrom, totalSec]);

  return { isPlaying, isLoading, playheadSec, totalSec, error, play, stop, seek };
}
