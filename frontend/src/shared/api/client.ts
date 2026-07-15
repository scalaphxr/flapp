// API client. In the packaged app the Go backend listens on a random localhost
// port; the Tauri shell discovers it and exposes it via the get_backend_port
// command plus a "backend-ready" event. In a plain browser (npm run dev without
// Tauri) we fall back to a fixed dev port. All endpoint wrappers funnel through
// request(), which resolves the base URL lazily and caches it.

import type {
  Analytics,
  Job,
  MidiClip,
  MidiExtractRequest,
  MidiNotesResult,
  Sample,
  SearchResult,
  Settings,
  SmartResult,
} from "./types";

const DEV_FALLBACK = "http://127.0.0.1:8765";

function isTauri(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

let baseUrlPromise: Promise<string> | null = null;

async function resolveBaseUrl(): Promise<string> {
  if (!isTauri()) {
    const envBase = (import.meta as any).env?.VITE_API_BASE as string | undefined;
    return envBase || DEV_FALLBACK;
  }
  const { invoke } = await import("@tauri-apps/api/core");
  const { listen } = await import("@tauri-apps/api/event");

  // The backend may already be up; poll the command a few times first.
  for (let i = 0; i < 50; i++) {
    const port = (await invoke<number | null>("get_backend_port").catch(() => null)) ?? null;
    if (port) return `http://127.0.0.1:${port}`;
    await new Promise((r) => setTimeout(r, 100));
  }
  // Otherwise wait for the event.
  return new Promise<string>((resolve) => {
    listen<number>("backend-ready", (event) => {
      resolve(`http://127.0.0.1:${event.payload}`);
    });
  });
}

function baseUrl(): Promise<string> {
  if (!baseUrlPromise) baseUrlPromise = resolveBaseUrl();
  return baseUrlPromise;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const base = await baseUrl();
  const res = await fetch(base + path, {
    headers: { "Content-Type": "application/json", ...(init?.headers || {}) },
    ...init,
  });
  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      /* ignore parse error */
    }
    throw new Error(message);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

// audioUrl returns a directly-usable <audio> src for a sample preview.
export async function audioUrl(id: number): Promise<string> {
  const base = await baseUrl();
  return `${base}/api/samples/${id}/audio`;
}

// eventsUrl returns the SSE endpoint for job updates.
export async function eventsUrl(): Promise<string> {
  const base = await baseUrl();
  return `${base}/api/events`;
}

export interface SearchParams {
  q?: string;
  categories?: string[];
  origins?: string[];
  tags?: string[];
  minBpm?: number;
  maxBpm?: number;
  favorite?: boolean;
  minRating?: number;
  sort?: string;
  order?: string;
  limit?: number;
  offset?: number;
}

function searchQuery(p: SearchParams): string {
  const q = new URLSearchParams();
  if (p.q) q.set("q", p.q);
  if (p.categories?.length) q.set("categories", p.categories.join(","));
  if (p.origins?.length) q.set("origins", p.origins.join(","));
  if (p.tags?.length) q.set("tags", p.tags.join(","));
  if (p.minBpm) q.set("minBpm", String(p.minBpm));
  if (p.maxBpm) q.set("maxBpm", String(p.maxBpm));
  if (p.favorite) q.set("favorite", "true");
  if (p.minRating) q.set("minRating", String(p.minRating));
  if (p.sort) q.set("sort", p.sort);
  if (p.order) q.set("order", p.order);
  if (p.limit != null) q.set("limit", String(p.limit));
  if (p.offset != null) q.set("offset", String(p.offset));
  const s = q.toString();
  return s ? `?${s}` : "";
}

export const api = {
  // Harvest & jobs.
  harvest: (body: import("./types").HarvestRequest) =>
    request<{ jobId: string }>("/api/harvest", { method: "POST", body: JSON.stringify(body) }),
  jobs: () => request<Job[]>("/api/jobs"),
  job: (id: string) => request<Job>(`/api/jobs/${id}`),
  cancelJob: (id: string) =>
    request<{ canceled: boolean }>(`/api/jobs/${id}/cancel`, { method: "POST" }),

  // Library.
  searchSamples: (params: SearchParams) => request<SearchResult>(`/api/samples${searchQuery(params)}`),
  sample: (id: number) => request<Sample>(`/api/samples/${id}`),
  similar: (id: number, limit = 20) =>
    request<{ items: Sample[] }>(`/api/samples/${id}/similar?limit=${limit}`),
  samplePeaks: (id: number, bins = 1500) =>
    request<{ peaks: [number, number][] }>(`/api/samples/${id}/peaks?bins=${bins}`),
  sampleSpectrogram: (id: number, frames = 200, bins = 64) =>
    request<{ data: number[]; frames: number; bins: number }>(
      `/api/samples/${id}/spectrogram?frames=${frames}&bins=${bins}`
    ),
  setCategory: (id: number, category: string) =>
    request(`/api/samples/${id}/category`, { method: "POST", body: JSON.stringify({ category }) }),
  setFavorite: (id: number, favorite: boolean) =>
    request(`/api/samples/${id}/favorite`, { method: "POST", body: JSON.stringify({ favorite }) }),
  setRating: (id: number, rating: number) =>
    request(`/api/samples/${id}/rating`, { method: "POST", body: JSON.stringify({ rating }) }),
  clearSamples: () => request("/api/samples", { method: "DELETE" }),

  // Export to folder (copies files into category sub-folders).
  exportToFolder: (sampleIds: number[], destDir: string) =>
    request<{ jobId: string }>("/api/export/folder", {
      method: "POST",
      body: JSON.stringify({ sampleIds, destDir }),
    }),

  // Pack builder.
  buildPack: (body: {
    name: string;
    sampleIds: number[];
    groupByCategory: boolean;
    format: string;
    includeMidi?: boolean;
    midiGroupMode?: string;
  }) => request<{ jobId: string }>("/api/packs", { method: "POST", body: JSON.stringify(body) }),

  // Analytics and smart search.
  analytics: () => request<Analytics>("/api/analytics"),
  smartSearch: (query: string, limit = 200, offset = 0) =>
    request<SmartResult>("/api/smartsearch", {
      method: "POST",
      body: JSON.stringify({ query, limit, offset }),
    }),

  // Settings.
  getSettings: () => request<Settings>("/api/settings"),
  putSettings: (s: Settings) =>
    request<Settings>("/api/settings", { method: "PUT", body: JSON.stringify(s) }),

  // MIDI extraction.
  midiExtract: (req: MidiExtractRequest) =>
    request<{ jobId: string }>("/api/midi/extract", { method: "POST", body: JSON.stringify(req) }),
  midiClips: (category?: string) =>
    request<{ items: MidiClip[]; total: number }>(
      `/api/midi/clips${category ? `?category=${encodeURIComponent(category)}` : ""}`
    ),
  midiClipFileUrl: async (id: string): Promise<string> => {
    const base = await baseUrl();
    return `${base}/api/midi/clips/${id}/file`;
  },
  midiSetClipCategory: (id: string, category: string) =>
    request<{ ok: boolean }>(`/api/midi/clips/${id}/category`, {
      method: "POST",
      body: JSON.stringify({ category }),
    }),
  midiPack: (ids: string[], packName?: string, outputDir?: string) =>
    request<{ jobId: string }>("/api/midi/pack", {
      method: "POST",
      body: JSON.stringify({ ids, packName: packName ?? "", outputDir: outputDir ?? "" }),
    }),
  midiClear: () => request<{ ok: boolean }>("/api/midi/clips", { method: "DELETE" }),
  midiDedup: () => request<import("./types").MidiDedupResult>("/api/midi/dedup", { method: "POST" }),
  midiClipNotes: (id: string) =>
    request<MidiNotesResult>(`/api/midi/clips/${id}/notes`),
  midiClipSampleUrl: async (id: string): Promise<string> => {
    const base = await baseUrl();
    return `${base}/api/midi/clips/${id}/sample`;
  },
  // Производный кэш: .mid файлы + .zip паки. Не трогает library.db.
  cacheClear: () => request<import("./types").CacheStats>("/api/cache/clear", { method: "POST" }),

  // YouTube publishing.
  ytStatus: () => request<import("./types").YtStatus>("/api/youtube/status"),
  ytAuth: () => request<{ authUrl: string }>("/api/youtube/auth", { method: "POST" }),
  ytDisconnect: () => request<{ ok: boolean }>("/api/youtube/disconnect", { method: "POST" }),
  ytFfmpeg: () => request<{ found: boolean; path: string }>("/api/youtube/ffmpeg"),
  // Скачивает портативный ffmpeg в папку данных приложения (джоба с прогрессом).
  ytFfmpegDownload: () => request<{ jobId: string }>("/api/youtube/ffmpeg/download", { method: "POST" }),
  ytUpload: (body: {
    audioPath: string;
    imagePath: string;
    title: string;
    description: string;
    tags: string[];
    privacy: string;
    overlay?: boolean;
    overlayTitle?: string;
    overlaySub?: string;
    overlayFont?: string;
  }) => request<{ jobId: string }>("/api/youtube/upload", { method: "POST", body: JSON.stringify(body) }),
  // Рендерит короткий mp4-клип (без загрузки) с вшитым текстом — для превью видео.
  ytPreview: (body: {
    audioPath: string;
    imagePath: string;
    overlay?: boolean;
    overlayTitle?: string;
    overlaySub?: string;
    overlayFont?: string;
  }) => request<{ path: string }>("/api/youtube/preview", { method: "POST", body: JSON.stringify(body) }),
  // Автоподбор тегов по тайп-артистам (шаблоны + подсказки поиска YouTube).
  ytTags: (artists: string[]) =>
    request<{ tags: string[] }>(`/api/youtube/tags?artists=${encodeURIComponent(artists.join(","))}`),

  // Cover images (Pinterest search + local download for the renderer).
  coversSearch: (q: string, limit = 40) =>
    request<{ items: import("./types").CoverImage[] }>(
      `/api/covers/search?q=${encodeURIComponent(q)}&limit=${limit}`
    ),
  coversDownload: (url: string) =>
    request<{ path: string }>("/api/covers/download", { method: "POST", body: JSON.stringify({ url }) }),
};
