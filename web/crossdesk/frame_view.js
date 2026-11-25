// CrossDesk 原生画面帧渲染。
//
// 纯 Go Agent 无法使用浏览器 WebRTC 视频编码器，而是通过 DataChannel
// 发送分片 JPEG（协议见 internal/crossdesk/agent/frames.go）。本模块负责
// 重组分片并绘制到 video 之上的 canvas，同时保持指针事件落到 video。
(function () {
  const MAGIC = 0x43445331; // "CDS1"

  class FrameView {
    constructor() {
      this.canvas = null;
      this.parts = null;
      this.seq = -1;
      this.received = 0;
      this.decoding = false;
      this.pending = null;
      this.frames = 0;
      this.gen = 0;
    }

    ensure() {
      if (this.canvas) return this.canvas;
      const container = document.getElementById("video-container");
      if (!container) return null;
      const canvas = document.createElement("canvas");
      canvas.id = "native-frame-canvas";
      canvas.className = "native-frame-canvas";
      canvas.style.cssText = [
        "position:absolute", "top:0", "left:0", "width:100%", "height:100%",
        "object-fit:contain", "pointer-events:none", "z-index:2",
        "display:none", "background:#000",
      ].join(";");
      container.appendChild(canvas);
      this.canvas = canvas;
      return canvas;
    }

    reset() {
      this.gen++;
      this.parts = null;
      this.seq = -1;
      this.received = 0;
      this.pending = null;
      if (this.canvas) {
        this.canvas.style.display = "none";
        const ctx = this.canvas.getContext("2d");
        ctx?.clearRect(0, 0, this.canvas.width, this.canvas.height);
      }
    }

    render(buffer) {
      if (!(buffer instanceof ArrayBuffer) || buffer.byteLength < 16) return;
      const view = new DataView(buffer);
      if (view.getUint32(0) !== MAGIC) return;
      const seq = view.getUint32(4);
      const index = view.getUint16(8);
      const total = view.getUint16(10);
      const width = view.getUint16(12);
      const height = view.getUint16(14);
      if (total === 0 || index >= total || total > 4096) return;
      if (index === 0 || this.seq !== seq || !this.parts || this.parts.length !== total) {
        this.seq = seq;
        this.parts = new Array(total);
        this.received = 0;
      }
      if (this.parts[index]) return;
      this.parts[index] = new Uint8Array(buffer, 16);
      this.received++;
      if (this.received < total) return;
      const parts = this.parts;
      this.parts = null;
      this.received = 0;
      this.draw(new Blob(parts, { type: "image/jpeg" }), width, height);
    }

    draw(blob, width, height) {
      if (this.decoding) {
        // 只保留最新一帧，避免解码积压导致延迟持续增大。
        this.pending = { blob, width, height };
        return;
      }
      this.decoding = true;
      const gen = this.gen;
      const done = () => {
        this.decoding = false;
        const next = this.pending;
        this.pending = null;
        if (next) this.draw(next.blob, next.width, next.height);
      };
      const paint = (source) => {
        if (gen !== this.gen) { done(); return; }
        const canvas = this.ensure();
        if (!canvas) { done(); return; }
        const w = source.width || source.naturalWidth || width;
        const h = source.height || source.naturalHeight || height;
        canvas.width = w;
        canvas.height = h;
        canvas.style.display = "block";
        canvas.getContext("2d").drawImage(source, 0, 0);
        source.close?.();
        const overlay = document.getElementById("connecting-overlay");
        if (overlay) overlay.style.display = "none";
        window.CrossDeskSessionControls?.setVideoReady?.();
        const led = document.getElementById("connected-status-led");
        if (led) {
          led.classList.remove("status-led-off");
          led.classList.add("status-led-on");
        }
        this.frames++;
        done();
      };
      if (window.createImageBitmap) {
        createImageBitmap(blob, { imageOrientation: "none", premultiplyAlpha: "none" })
          .then(paint)
          .catch(() => this.fallbackImage(blob, paint, done));
      } else {
        this.fallbackImage(blob, paint, done);
      }
    }

    fallbackImage(blob, paint, done) {
      const url = URL.createObjectURL(blob);
      const img = new Image();
      img.onload = () => {
        paint(img);
        URL.revokeObjectURL(url);
      };
      img.onerror = () => {
        URL.revokeObjectURL(url);
        done();
      };
      img.src = url;
    }
  }

  window.CrossDeskFrameView = new FrameView();
})();
