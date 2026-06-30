// Opens the backend's Server-Sent Events stream and invokes a callback for
// every job update. Returns a disposer that closes the connection. The stream
// auto-reconnects (EventSource default), and we re-resolve the URL each time so
// a backend restart with a new port is picked up on the next mount.

import { eventsUrl } from "./client";
import type { Job } from "./types";

export async function subscribeJobs(onJob: (job: Job) => void): Promise<() => void> {
  const url = await eventsUrl();
  const es = new EventSource(url);
  es.addEventListener("job", (e) => {
    try {
      const job = JSON.parse((e as MessageEvent).data) as Job;
      onJob(job);
    } catch {
      /* ignore malformed frame */
    }
  });
  return () => es.close();
}
