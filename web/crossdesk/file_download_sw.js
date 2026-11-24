// A same-origin streaming response lets the browser save directly to Downloads.
// Other requests are left untouched; this worker does not cache application files.
"use strict";
const jobs = new Map();
const prefix = new URL("file-transfer-download/", self.registration.scope).href;
const MAX_CHUNK = 64 * 1024;
const IDLE_TIMEOUT = 45000;

self.addEventListener("activate", event => event.waitUntil(self.clients.claim()));

function touch(job) {
  clearTimeout(job.timer);
  job.timer = setTimeout(() => finish(job, true), IDLE_TIMEOUT);
}

function finish(job, failed = false) {
  if (job.done) return;
  job.done = true;
  clearTimeout(job.timer);
  jobs.delete(job.token);
  if (failed) {
    job.controller?.error(new Error("Download interrupted"));
    job.port.postMessage({ type: "error" });
  }
  job.pending = null;
  job.port.close();
  job.resolve();
}

function flush(job) {
  if (!job.demand || !job.pending || job.done) return;
  const { data, sequence } = job.pending;
  job.pending = null;
  job.demand = false;
  job.controller.enqueue(new Uint8Array(data));
  job.received += data.byteLength;
  // Confirm only when the browser consumes the chunk, propagating backpressure
  // through the page to the remote sender. This is not a filesystem fsync.
  job.port.postMessage({ type: "ack", sequence });
  touch(job);
}

self.addEventListener("message", event => {
  const message = event.data, port = event.ports[0];
  if (message?.type !== "open" || !port) return;
  const source = event.source;
  if (message.version !== 1 || !source?.url?.startsWith(self.registration.scope) ||
      !/^[0-9a-f-]{36}$/.test(message.token) || jobs.has(message.token) || jobs.size >= 32 ||
      typeof message.name !== "string" || !message.name.length || message.name.length > 65535 ||
      !Number.isSafeInteger(message.size) || message.size < 0) {
    port.postMessage({ type: "error" });
    port.close();
    return;
  }
  const name = message.name.replace(/[\x00-\x1f\x7f/\\]/g, "_");
  const job = { token: message.token, name, size: message.size, received: 0, sequence: 0,
    port, controller: null, demand: false, pending: null, started: false, done: false };
  // Keep preparation alive until the download fetch takes over. Do not hold a
  // message event open for the whole file: browsers cap event execution time.
  event.waitUntil(new Promise(resolve => { job.resolve = resolve; }));
  jobs.set(job.token, job);
  touch(job);
  port.onmessageerror = () => finish(job, true);
  port.onmessage = ({ data: request }) => {
    if (job.done) return;
    if (request?.type === "abort") { finish(job, true); return; }
    if (!job.started || request?.sequence !== job.sequence + 1 || job.pending) {
      finish(job, true); return;
    }
    job.sequence = request.sequence;
    if (request.type === "chunk" && request.data instanceof ArrayBuffer &&
        request.data.byteLength > 0 && request.data.byteLength <= MAX_CHUNK &&
        job.received + request.data.byteLength <= job.size) {
      job.pending = request;
      flush(job);
    } else if (request.type === "end" && job.received === job.size) {
      job.controller.close();
      port.postMessage({ type: "ack", sequence: request.sequence });
      finish(job);
    } else finish(job, true);
  };
  port.postMessage({ type: "ready", url: prefix + job.token });
});

self.addEventListener("fetch", event => {
  if (!event.request.url.startsWith(prefix)) return;
  const token = event.request.url.slice(prefix.length), job = jobs.get(token);
  if (event.request.method !== "GET" || !job || job.started) {
    event.respondWith(new Response("Download expired", { status: 410 }));
    return;
  }
  job.started = true;
  const stream = new ReadableStream({
    start(controller) { job.controller = controller; },
    pull() { job.demand = true; flush(job); },
    cancel() { finish(job, true); }
  }, { highWaterMark: 0 });
  const filename = encodeURIComponent(job.name).replace(/['()*]/g,
    char => `%${char.charCodeAt(0).toString(16).toUpperCase()}`);
  event.respondWith(new Response(stream, { headers: {
    "Content-Type": "application/octet-stream",
    "Content-Disposition": `attachment; filename="download"; filename*=UTF-8''${filename}`,
    "Cache-Control": "no-store",
    "X-Content-Type-Options": "nosniff"
    // Omit Content-Length so the browser waits for the explicit end of stream.
  } }));
  job.port.postMessage({ type: "started" });
  job.resolve();
  touch(job);
});
