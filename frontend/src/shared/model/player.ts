// Audio preview player. A single shared <audio> element plays one sample at a
// time; components call toggle(id) and read playingId to render play/pause.
// seek(ratio) устанавливает позицию воспроизведения в диапазоне 0..1.

import { create } from "zustand";
import { audioUrl } from "@/shared/api/client";

interface PlayerState {
  playingId: number | null;
  audio: HTMLAudioElement | null;
  toggle: (id: number) => Promise<void>;
  stop: () => void;
  seek: (ratio: number, knownDuration?: number) => void;
}

export const usePlayerStore = create<PlayerState>((set, get) => ({
  playingId: null,
  audio: null,

  toggle: async (id) => {
    const { playingId, audio } = get();
    if (playingId === id && audio) {
      audio.pause();
      set({ playingId: null });
      return;
    }
    if (audio) {
      audio.pause();
    }
    const el = audio ?? new Audio();
    el.onended = () => set({ playingId: null });
    el.onerror = () => set({ playingId: null });
    try {
      el.src = await audioUrl(id);
      await el.play();
      set({ audio: el, playingId: id });
    } catch {
      set({ playingId: null });
    }
  },

  stop: () => {
    const { audio } = get();
    if (audio) audio.pause();
    set({ playingId: null });
  },

  // knownDuration (harvest-computed, e.g. sample.features.durationSeconds) is
  // preferred over audio.duration: for VBR MP3 the browser's estimate can be
  // noticeably off, which used to make click position and seek target
  // disagree — the waveform playhead already trusts the harvest duration
  // (see WaveformCanvas in SoundTable), so seek must use the same source.
  seek: (ratio, knownDuration) => {
    const { audio } = get();
    if (!audio) return;
    const dur = knownDuration && knownDuration > 0
      ? knownDuration
      : (isFinite(audio.duration) && audio.duration > 0 ? audio.duration : 0);
    if (dur > 0) {
      audio.currentTime = Math.max(0, Math.min(1, ratio)) * dur;
    }
  },
}));
