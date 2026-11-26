// 纯 Go Agent 的 PCM 音频播放（iflygo-audio-v1）。
//
// 音频帧：16 字节头（CDA1 / seq / rate / channels / format）+ s16le PCM。
// 使用 Web Audio + ScriptProcessor 播放；AudioContext 在用户点击音频按钮时创建。
(function () {
  const MAGIC = 0x43444131; // "CDA1"
  const HEADER = 16;
  const RATE = 48000;
  const CHANNELS = 2;
  const FORMAT_S16 = 1;

  class PCMAudio {
    constructor() {
      this.channel = null;
      this.available = false;
      this.enabled = false;
      this.samples = 0;
      this.context = null;
      this.gain = null;
      this.processor = null;
      this.ring = new Float32Array(RATE * CHANNELS * 2);
      this.read = 0;
      this.write = 0;
      this.size = 0;
    }

    attach(channel) {
      if (this.channel === channel) return;
      this.reset();
      this.channel = channel;
      this.available = true;
      channel.binaryType = "arraybuffer";
      channel.addEventListener("message", (event) => {
        if (event.data instanceof ArrayBuffer) this.push(event.data);
      });
      channel.addEventListener("close", () => {
        if (this.channel === channel) this.reset();
      });
      window.dispatchEvent(new CustomEvent("crossdesk-pcm-audio"));
    }

    reset() {
      this.disable();
      this.available = false;
      this.channel = null;
      this.samples = 0;
      this.read = this.write = this.size = 0;
      if (this.ring) this.ring.fill(0);
      window.dispatchEvent(new CustomEvent("crossdesk-pcm-audio"));
    }

    async enable() {
      if (!this.available) return false;
      if (!this.context) {
        const Ctx = window.AudioContext || window.webkitAudioContext;
        if (!Ctx) return false;
        try {
          this.context = new Ctx({ sampleRate: RATE });
        } catch (_) {
          this.context = new Ctx();
        }
        this.gain = this.context.createGain();
        this.gain.gain.value = 0;
        this.gain.connect(this.context.destination);
        // ScriptProcessor 已弃用但兼容性最好；2048 帧约 43ms。
        this.processor = this.context.createScriptProcessor(2048, 0, CHANNELS);
        const ring = this.ring;
        this.processor.onaudioprocess = (event) => {
          const left = event.outputBuffer.getChannelData(0);
          const right = CHANNELS > 1 ? event.outputBuffer.getChannelData(1) : left;
          if (!this.enabled) {
            left.fill(0);
            if (right !== left) right.fill(0);
            return;
          }
          for (let i = 0; i < left.length; i++) {
            if (this.size < 2) {
              left[i] = 0;
              right[i] = 0;
              continue;
            }
            left[i] = ring[this.read];
            right[i] = ring[(this.read + 1) % ring.length];
            this.read = (this.read + 2) % ring.length;
            this.size -= 2;
          }
        };
        this.processor.connect(this.gain);
      }
      if (this.context.state === "suspended") {
        try { await this.context.resume(); } catch (_) {}
      }
      this.gain.gain.value = 1;
      this.enabled = true;
      return true;
    }

    disable() {
      this.enabled = false;
      if (this.gain) this.gain.gain.value = 0;
    }

    push(buffer) {
      if (buffer.byteLength <= HEADER) return;
      const view = new DataView(buffer);
      if (view.getUint32(0) !== MAGIC) return;
      if (view.getUint16(12, true) !== CHANNELS || view.getUint16(14, true) !== FORMAT_S16) return;
      if (view.getUint32(8, true) !== RATE) return;
      const pcm = new Int16Array(buffer, HEADER);
      const ring = this.ring;
      for (let i = 0; i + 1 < pcm.length; i += 2) {
        if (this.size + 2 > ring.length) {
          this.read = (this.read + 2) % ring.length;
          this.size -= 2;
        }
        ring[this.write] = pcm[i] / 32768;
        ring[(this.write + 1) % ring.length] = pcm[i + 1] / 32768;
        this.write = (this.write + 2) % ring.length;
        this.size += 2;
      }
      this.samples += pcm.length / 2;
    }
  }

  window.CrossDeskPCMAudio = new PCMAudio();
})();
