// Stream received bytes to the browser's download manager, using its save settings.
(function () {
  "use strict";
  const script = new URL(document.currentScript.src);
  const scope = new URL("./", script);
  const workerURL = new URL("file_download_sw.js", scope);
  workerURL.search = script.search;
  const TIMEOUT = 30000;
  const FAILURE = "浏览器下载已中断，请检查下载权限、存储空间后重新发送。";
  let registration;

  function getWorker() {
    if (!registration) {
      registration = navigator.serviceWorker.register(workerURL.href, {
        scope: scope.href, updateViaCache: "none"
      }).then(async reg => {
        if (reg.active?.state === "activated") return reg.active;
        const worker = reg.installing || reg.waiting || reg.active;
        if (!worker) throw new Error("Download worker unavailable");
        await new Promise((resolve, reject) => {
          const timer = setTimeout(() => finish(new Error("Download worker timed out")), TIMEOUT);
          const changed = () => {
            if (worker.state === "activated") finish();
            else if (worker.state === "redundant") finish(new Error("Download worker failed"));
          };
          function finish(error) {
            clearTimeout(timer);
            worker.removeEventListener("statechange", changed);
            if (error) reject(error); else resolve();
          }
          worker.addEventListener("statechange", changed);
          changed();
        });
        return worker;
      }).catch(error => { registration = null; throw error; });
    }
    return registration;
  }

  function waitForControl(worker) {
    // On the first reception, activation can precede delivery of clients.claim()
    // to this page. A download started too soon would bypass the worker.
    return new Promise((resolve, reject) => {
      const changed = () => {
        if (navigator.serviceWorker.controller?.scriptURL === worker.scriptURL) finish();
      };
      const timer = setTimeout(() => finish(new Error("Download worker control timed out")), TIMEOUT);
      function finish(error) {
        clearTimeout(timer);
        navigator.serviceWorker.removeEventListener("controllerchange", changed);
        if (error) reject(error); else resolve();
      }
      navigator.serviceWorker.addEventListener("controllerchange", changed);
      changed();
    });
  }

  class DownloadWriter {
    static get supported() {
      return window.isSecureContext && ["https:", "http:"].includes(location.protocol) &&
        !!navigator.serviceWorker && typeof MessageChannel === "function" &&
        typeof ReadableStream === "function";
    }

    constructor(name, size, onFailure) {
      this.name = name;
      this.size = size;
      this.onFailure = onFailure;
      this.pending = new Map();
      this.sequence = 0;
      this.closed = false;
      this.port = null;
    }

    wait(key) {
      if (this.closed) return Promise.reject(new Error(FAILURE));
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => this.fail(), TIMEOUT);
        this.pending.set(key, { resolve, reject, timer });
      });
    }

    receive(message) {
      if (message?.type === "error") { this.fail(); return; }
      const key = message?.type === "ack" ? message.sequence : message?.type;
      const request = this.pending.get(key);
      if (!request) return;
      clearTimeout(request.timer);
      this.pending.delete(key);
      request.resolve(message);
    }

    async open() {
      try {
        if (!DownloadWriter.supported) throw new DOMException("Streaming download unavailable", "NotSupportedError");
        const worker = await getWorker();
        await waitForControl(worker);
        if (this.closed) throw new Error(FAILURE);
        const channel = new MessageChannel();
        this.port = channel.port1;
        this.port.onmessage = event => this.receive(event.data);
        this.port.onmessageerror = () => this.fail();
        const token = crypto.randomUUID();
        const ready = this.wait("ready");
        try {
          worker.postMessage({ type: "open", version: 1, token, name: this.name, size: this.size }, [channel.port2]);
        } catch (error) { this.fail(); }
        const message = await ready;
        const expectedURL = new URL(`file-transfer-download/${token}`, scope).href;
        if (message.url !== expectedURL || this.closed) throw new Error(FAILURE);
        const started = this.wait("started");
        // A download attribute can bypass Service Worker fetch handling. A
        // navigation uses the streaming attachment response without leaving the UI.
        this.frame = document.createElement("iframe");
        this.frame.hidden = true;
        this.frame.src = expectedURL;
        document.body.appendChild(this.frame);
        await started;
      } catch (error) {
        this.fail();
        throw error;
      }
    }

    request(type, data) {
      if (this.closed || !this.port) return Promise.reject(new Error(FAILURE));
      const sequence = ++this.sequence;
      const result = this.wait(sequence);
      try {
        this.port.postMessage({ type, sequence, data }, data ? [data] : []);
      } catch (error) { this.fail(); }
      return result;
    }

    write(bytes) {
      // Only copy the current chunk; the transferred buffer is released by the page.
      return this.request("chunk", bytes.slice().buffer);
    }

    async close() {
      await this.request("end");
      this.closed = true;
      this.port.close();
      // The attachment navigation is handed to the download manager
      // asynchronously, especially for empty files. Removing its frame here
      // can cancel the download before the browser even creates the entry.
      const frame = this.frame;
      this.frame = null;
      setTimeout(() => frame?.remove(), 60000);
    }

    fail(notify = true) {
      if (this.closed) return;
      this.closed = true;
      if (this.port) {
        try { this.port.postMessage({ type: "abort" }); } catch (error) {}
        this.port.close();
      }
      for (const request of this.pending.values()) {
        clearTimeout(request.timer);
        request.reject(new Error(FAILURE));
      }
      this.pending.clear();
      this.frame?.remove();
      if (notify) this.onFailure?.(FAILURE);
    }

    abort() { this.fail(false); }
  }
  window.CrossDeskDownloadWriter = DownloadWriter;
})();
