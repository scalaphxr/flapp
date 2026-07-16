// Settings store. Loads settings from the backend on startup, keeps them in
// memory, and persists changes. Reflects language choice into the i18n store.

import { create } from "zustand";
import { api } from "@/shared/api/client";
import type { Settings } from "@/shared/api/types";
import { useI18nStore, type Lang } from "@/shared/i18n";

interface SettingsState {
  settings: Settings | null;
  loading: boolean;
  // true после успешного GET с бэкенда. Пока false, update() не делает PUT:
  // иначе fallback-объект с пустыми полями затёр бы реальные настройки
  // (так однажды пропали YouTube-ключи).
  fromServer: boolean;
  load: () => Promise<void>;
  update: (patch: Partial<Settings>) => Promise<void>;
}

const fallback: Settings = {
  language: "en",
  // Рудимент: бэкенд хранит поле, фронт его не читает. Тема одна — Console.
  theme: "fl",
  exportDir: "",
  midiOutputDir: "",
  workers: 4,
  dedupThreshold: 80,
  deepDedup: true,
  generateTags: true,
  gpu: false,
  autoUpdate: true,
  backupOnExit: false,
  ffmpegPath: "",
  ytClientId: "",
  ytClientSecret: "",
  ytNickname: "",
  ytNoTextOverlay: false,
  ytFont: "",
  ytAuthorAliases: {},
  ytDefaultImage: "",
  ytTitleTemplate: '[FREE] {type} Type Beat "{name}" | {bpm} BPM {key}',
  ytTitleTemplates: [
    '[FREE] {type} Type Beat "{name}" | {bpm} BPM {key}',
    "{name} | {type} type beat {bpm}bpm {key}",
    '[FREE] {type} x {nick} Type Beat "{name}"',
    "{type} type beat — {name}",
  ],
  ytDescription: "",
  ytDescTemplates: [],
  ytTags: "type beat, instrumental, beat, free type beat",
  ytPrivacy: "public",
};

export const useSettingsStore = create<SettingsState>((set, get) => ({
  settings: null,
  loading: false,
  fromServer: false,

  load: async () => {
    set({ loading: true });
    try {
      const s = await api.getSettings();
      set({ settings: s, loading: false, fromServer: true });
      useI18nStore.getState().setLang((s.language as Lang) || "en");
    } catch {
      set({ settings: fallback, loading: false });
    }
  },

  update: async (patch) => {
    // Если в сторе fallback (load не удался) — сперва перечитываем настройки,
    // чтобы PUT не отправил на сервер пустые поля вместо реальных.
    if (!get().fromServer) {
      await get().load();
    }
    const current = get().settings ?? fallback;
    const next = { ...current, ...patch };
    set({ settings: next });
    if (patch.language) {
      useI18nStore.getState().setLang(patch.language as Lang);
    }
    if (!get().fromServer) {
      return; // бэкенд недоступен — меняем только локально, без риска затирания
    }
    try {
      const saved = await api.putSettings(next);
      set({ settings: saved });
    } catch {
      /* keep optimistic value */
    }
  },
}));
