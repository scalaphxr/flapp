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
  seek: (ratio: number) => void;
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

  seek: (ratio) => {
    const { audio } = get();
    if (audio && audio.duration > 0 && isFinite(audio.duration)) {
      audio.currentTime = Math.max(0, Math.min(1, ratio)) * audio.duration;
    }
  },
}));
