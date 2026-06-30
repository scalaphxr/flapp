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
  | "bass"      // repurposed for Loop
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
  Loop:       "bass",      // --cat-bass (dusty violet) used for Loop
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

export const GROUP_HEX: Record<ColorGroup, string> = {
  "808":      "#D98C6A",
  kick:       "#E0926A",
  snare:      "#E0A455",
  clap:       "#D98E8E",
  hihat:      "#C7B36A",
  openhat:    "#B6B97A",
  perc:       "#A8B27A",
  vox:        "#C79BC4",
  fx:         "#C98BA8",
  bass:       "#B79BD4",  // Loop
  drumloop:   "#C9A06A",
  unsorted:   "#998C7C",
};

export function groupHex(group: ColorGroup): string {
  return GROUP_HEX[group];
}
