// TypeScript mirrors of the backend JSON shapes. Field names follow the Go
// json tags (camelCase). These are the single source of truth for API typing
// on the client; keep them in sync with internal/domain.

export interface AudioFeatures {
  sampleRate: number;
  channels: number;
  bitDepth: number;
  durationSeconds: number;
  rms: number;
  peakAmplitude: number;
  spectralCentroid: number;
  zeroCrossRate: number;
  lowEnergyRatio: number;
  highEnergyRatio: number;
  attackTime: number;
  analyzed: boolean;
}

export interface Sample {
  id: number;
  name: string;
  path: string;
  ext: string;
  size: number;
  category: string;
  auto: boolean;
  origin: string; // archive | project | folder | both
  sourceLabel: string;
  sourcePath: string;
  md5: string;
  sha256: string;
  fingerprint: string;
  features: AudioFeatures;
  bpm: number;
  keyName: string;
  tags: string[] | null;
  favorite: boolean;
  rating: number;
  usedCount: number;
  addedAt: number;
  modifiedAt: number;
}

export type JobType =
  | "harvest"
  | "export_pack"
  | "import_folder"
  | "rename"
  | "reanalyze"
  | "extract_midi";

export type JobStatus =
  | "queued"
  | "running"
  | "completed"
  | "failed"
  | "canceled";

export interface Job {
  id: string;
  type: JobType;
  status: JobStatus;
  progress: number; // 0..1
  stage: string;
  detail: string;
  error: string;
  result: Record<string, unknown> | null;
  createdAt: number;
  updatedAt: number;
}

export interface SearchResult {
  items: Sample[];
  total: number;
}

export interface HarvestRequest {
  inputs: string[];
  drumkitsDir?: string;
  guess?: boolean;
  extraFormats?: boolean;
  deepDedup?: boolean;
  onlyFromFlp?: boolean;
  generateTags?: boolean;
  acousticThreshold?: number;
}

export interface CategoryCount {
  category: string;
  colorGroup: string;
  count: number;
  bytes: number;
}

export interface SampleRef {
  id: number;
  name: string;
  used: number;
  cat: string;
}

export interface BPMCount {
  bpm: number;
  count: number;
}
export interface KeyCount {
  key: string;
  count: number;
}
export interface TagCount {
  tag: string;
  count: number;
}

export interface Analytics {
  projects: number;
  samples: number;
  uniqueSamples: number;
  duplicates: number;
  bytesTotal: number;
  bytesSaved: number;
  byCategory: CategoryCount[];
  topUsed: SampleRef[];
  topBpm: BPMCount[];
  topKeys: KeyCount[];
  topTags: TagCount[];
}

export interface Interpretation {
  categories: string[] | null;
  tags: string[] | null;
  minBpm: number;
  maxBpm: number;
  freeText: string;
}

export interface SmartResult {
  items: Sample[];
  total: number;
  interpretation: Interpretation;
}

// --- MIDI ---

export type MidiCategory =
  | "808/Bass"
  | "Melody"
  | "Kick"
  | "Snare"
  | "Clap"
  | "Hi-Hat"
  | "Open Hat"
  | "Perc"
  | "Drums"
  | "FX"
  | "Other";

export const ALL_MIDI_CATEGORIES: MidiCategory[] = [
  "808/Bass", "Melody",
  "Kick", "Snare", "Clap", "Hi-Hat", "Open Hat", "Perc", "Drums",
  "FX", "Other",
];

// CacheStats — ответ POST /api/cache/clear.
export interface CacheStats {
  midiFiles: number;
  midiBytes: number;
  exportFiles: number;
  exportBytes: number;
  totalBytes: number;
}

export interface MidiClip {
  id: string;
  projectPath: string;
  projectName: string;
  bpm: number;
  patternIndex: number;
  patternName: string;
  channelIndex: number;
  channelName: string;
  samplePath?: string;
  plugin?: string;
  category: MidiCategory;
  categoryOverride: boolean; // true = задана пользователем вручную
  decisionSource: string;
  noteCount: number;
  durationTicks: number;
  durationSec: number;
  minKey: number;
  maxKey: number;
  filePath?: string;
  fileName: string;
  sourceType: string;   // "flp" | "zip"
  sourceName: string;   // отображаемое имя источника
  contentHash?: string; // хеш содержимого нот для детекта дубликатов
}

export interface MidiDedupResult {
  removed: number;
  groups: number;
}

export interface MidiExtractRequest {
  inputs: string[];
  outputDir?: string;
  ignoreEmptySamplers?: boolean;
}

export interface MidiNote {
  tick: number;
  durationTicks: number;
  pitch: number;
  velocity: number;
}

export interface MidiNotesResult {
  bpm: number;
  ticksPerBeat: number;
  durationTicks: number;
  notes: MidiNote[];
}

// ---

export interface Settings {
  language: string;
  theme: string;
  exportDir: string;
  midiOutputDir: string; // папка для .mid файлов и MIDI-паков
  workers: number;
  dedupThreshold: number;
  deepDedup: boolean;
  generateTags: boolean;
  gpu: boolean;
  autoUpdate: boolean;
  backupOnExit: boolean;
}

