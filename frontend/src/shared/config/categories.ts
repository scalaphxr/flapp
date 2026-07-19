export type ColorGroup =
  | "808"
  | "kick"
  | "snare"
  | "clap"
  | "hihat"
  | "openhat"
  | "perc"
  | "vox"
  | "fx"
  | "loop"
  | "drumloop"
  | "unsorted";

// 11 canonical categories matching backend domain.AllCategories.
export const ALL_CATEGORIES: string[] = [
  "808", "Kick", "Snare", "Clap", "Hi-Hat", "Open Hat",
  "Perc", "Vox", "FX", "Loop", "Drum Loop",
];

const CATEGORY_GROUP: Record<string, ColorGroup> = {
  "808":      "808",
  Kick:       "kick",
  Snare:      "snare",
  Clap:       "clap",
  "Hi-Hat":   "hihat",
  "Open Hat": "openhat",
  Perc:       "perc",
  Vox:        "vox",
  FX:         "fx",
  Loop:       "loop",
  "Drum Loop":"drumloop",
};

export function groupOf(category: string): ColorGroup {
  return CATEGORY_GROUP[category] ?? "unsorted";
}

export function groupColor(group: ColorGroup): { color: string; bg: string } {
  return {
    color: `var(--cat-${group})`,
    bg: `var(--cat-${group}-bg)`,
  };
}

// Dimmed Terminal-Core palette — держать в синхроне с --cat-* в tokens.css.
export const GROUP_HEX: Record<ColorGroup, string> = {
  "808":      "#C77B5A",
  kick:       "#C9915C",
  snare:      "#C47385",
  clap:       "#BB7E9E",
  hihat:      "#6FB3A6",
  openhat:    "#6FA3B8",
  perc:       "#7E90B5",
  vox:        "#A585B8",
  fx:         "#B87FA8",
  loop:       "#8985B0",
  drumloop:   "#C2A15E",
  unsorted:   "#7A7A7A",
};

export function groupHex(group: ColorGroup): string {
  return GROUP_HEX[group];
}
