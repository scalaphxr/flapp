// Jobs store. Subscribes once to the backend SSE stream and keeps a map of jobs
// keyed by id. Pages read activeJob() to drive a progress bar, and can register
// completion callbacks (e.g. refresh results when a harvest finishes).

import { create } from "zustand";
import { subscribeJobs } from "@/shared/api/events";
import type { Job } from "@/shared/api/types";

type DoneListener = (job: Job) => void;

interface JobsState {
  jobs: Record<string, Job>;
  connected: boolean;
  doneListeners: Set<DoneListener>;
  connect: () => Promise<void>;
  upsert: (job: Job) => void;
  onDone: (fn: DoneListener) => () => void;
  activeJob: () => Job | null;
  latestOfType: (type: Job["type"]) => Job | null;
}

let disposer: (() => void) | null = null;

export const useJobsStore = create<JobsState>((set, get) => ({
  jobs: {},
  connected: false,
  doneListeners: new Set(),

  connect: async () => {
    if (disposer) return;
    // Mark connected optimistically; subscribeJobs resolves the URL lazily.
    set({ connected: true });
    disposer = await subscribeJobs((job) => {
      get().upsert(job);
    });
  },

  upsert: (job) => {
    const prev = get().jobs[job.id];
    set((s) => ({ jobs: { ...s.jobs, [job.id]: job } }));
    const becameTerminal =
      (job.status === "completed" || job.status === "failed" || job.status === "canceled") &&
      (!prev || prev.status !== job.status);
    if (becameTerminal) {
      get().doneListeners.forEach((fn) => fn(job));
    }
  },

  onDone: (fn) => {
    get().doneListeners.add(fn);
    return () => get().doneListeners.delete(fn);
  },

  activeJob: () => {
    const list = Object.values(get().jobs)
      .filter((j) => j.status === "running" || j.status === "queued")
      .sort((a, b) => b.updatedAt - a.updatedAt);
    return list[0] ?? null;
  },

  latestOfType: (type) => {
    const list = Object.values(get().jobs)
      .filter((j) => j.type === type)
      .sort((a, b) => b.updatedAt - a.updatedAt);
    return list[0] ?? null;
  },
}));
