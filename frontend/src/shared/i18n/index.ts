// Tiny i18n: a zustand store holds the active language; useT() returns the full
// dictionary for that language so components read strings as t.harvest.title.
// Switching language is instant (no reload) and is persisted via the settings
// store by the Settings page.

import { create } from "zustand";
import { ru, type Dict } from "./ru";
import { en } from "./en";

export type Lang = "ru" | "en";

const dicts: Record<Lang, Dict> = { ru, en };

interface I18nState {
  lang: Lang;
  setLang: (lang: Lang) => void;
}

export const useI18nStore = create<I18nState>((set) => ({
  lang: "en",
  setLang: (lang) => set({ lang }),
}));

// useT returns the dictionary for the current language.
export function useT(): Dict {
  const lang = useI18nStore((s) => s.lang);
  return dicts[lang];
}

// dict returns a dictionary outside React (rarely needed).
export function dict(lang: Lang): Dict {
  return dicts[lang];
}
