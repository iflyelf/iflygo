// 浏览器麦克风回传：16kHz 单声道 s16le PCM，经 iflygo-mic-v1 发送给 Agent。
(function () {
  const MAGIC = 0x43444131; // "CDA1"
  const HEADER = 16;
  const RATE = 16000;
  const CHANNELS = 1;
  const FORMAT_PCM = 1;
  const MAX_CHUNK = 4096;
  const encoder = new TextEncoder();

  class MicAudio {
    constructor() {
      this.channel = null;
      this.available = false;
      this.active = false;
      this.sentBytes = 0;
      this.sequence = 0;
      this.context = null;
      this.source = null;
      this.processor = null;
      this.stream = null;
      this.silent = null;
    }

    attach(channel) {
      if (this.channel === channel) return;
      this.reset();
      this.channel = channel;
      this.available = true;
      channel.addEventListener("close", () => {
        if (this.channel === channel) this.reset();
      });
      window.dispatchEvent(new CustomEvent("crossdesk-mic-audio"));
    }

    reset() {
      this.disable();
      this.available = false;
      this.channel = null;
      this.sentBytes = 0;
      this.sequence = 0;
      window.dispatchEvent(new CustomEvent("crossdesk-mic-audio"));
    }

    async enable() {
      if (!this.available || this.active) return false;
      if (!navigator.mediaDevices?.getUserMedia) return false;
      try {
        this.stream = await navigator.mediaDevices.getUserMedia({
          audio: {
            channelCount: CHANNELS,
            echoCancellation: true,
            noiseSuppression: true,
            autoGainControl: true,
          },
        });
        const Ctx = window.AudioContext || window.webkitAudioContext;
        this.context = new Ctx({ sampleRate: 48000 });
        this.source = this.context.createMediaStreamSource(this.stream);
        this.processor = this.context.createScriptProcessor(2048, 1, 1);
        this.silent = this.context.createGain();
        this.silent.gain.value = 0;
        this.processor.onaudioprocess = (event) => this.capture(event.inputBuffer);
        this.source.connect(this.processor);
        this.processor.connect(this.silent);
        this.silent.connect(this.context.destination);
        if (this.context.state === "suspended") {
          try { await this.context.resume(); } catch (_) {}
        }
        this.active = true;
        return true;
      } catch (err) {
        console.warn("Microphone unavailable:", err);
        this.disable();
        return false;
      }
    }

    disable() {
      this.active = false;
      try { this.processor?.disconnect(); } catch (_) {}
      try { this.source?.disconnect(); } catch (_) {}
      try { this.silent?.disconnect(); } catch (_) {}
      try { this.stream?.getTracks?.().forEach((track) => track.stop()); } catch (_) {}
      try { this.context?.close?.(); } catch (_) {}
      this.processor = null;
      this.source = null;
      this.silent = null;
      this.stream = null;
      this.context = null;
    }

    capture(buffer) {
      if (!this.active || this.channel?.readyState !== "open") return;
      const input = buffer.getChannelData(0);
      const inputRate = this.context?.sampleRate || 48000;
      const ratio = inputRate / RATE;
      const length = Math.min(MAX_CHUNK, Math.max(1, Math.floor(input.length / ratio)));
      const pcm = new Int16Array(length);
      for (let i = 0; i < length; i++) {
        const sample = input[Math.min(input.length - 1, Math.floor(i * ratio))] || 0;
        pcm[i] = Math.max(-32768, Math.min(32767, Math.round(sample * 32767)));
      }
      const packet = new ArrayBuffer(HEADER + pcm.byteLength);
      const view = new DataView(packet);
      view.setUint32(0, MAGIC);
      view.setUint32(4, ++this.sequence >>> 0);
      view.setUint32(8, RATE, true);
      view.setUint16(12, CHANNELS, true);
      view.setUint16(14, FORMAT_PCM, true);
      new Uint8Array(packet, HEADER).set(new Uint8Array(pcm.buffer));
      try {
        this.channel.send(packet);
        this.sentBytes += pcm.byteLength;
      } catch (_) {}
    }
  }

  window.CrossDeskMic = new MicAudio();
})();
