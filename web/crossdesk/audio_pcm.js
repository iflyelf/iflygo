// 纯 Go Agent 的 PCM 音频播放（iflygo-audio-v1）。
//
// 音频帧：16 字节头（CDA1 / seq / rate / channels / format）+ s16le PCM。
// 使用 Web Audio + ScriptProcessor 播放；AudioContext 在用户点击音频按钮时创建。
(function () {
  const MAGIC = 0x43444131; // "CDA1"
  const HEADER = 16;
  const RATE = 48000;
  const CHANNELS = 2;
  const FORMAT_PCM = 1;
  const FORMAT_ADPCM = 2;
  const FORMAT_ADPCM_RESET = 3;
  const IMA_STEP = [
    7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 19, 21, 23, 25, 28, 31, 34, 37, 41, 45,
    50, 55, 60, 66, 73, 80, 88, 97, 107, 118, 130, 143, 157, 173, 190, 209, 230,
    253, 279, 307, 337, 371, 408, 449, 494, 544, 598, 658, 724, 796, 876, 963,
    1060, 1166, 1282, 1411, 1552, 1707, 1878, 2066, 2272, 2499, 2749, 3024, 3327,
    3660, 4026, 4428, 4871, 5358, 5894, 6484, 7132, 7845, 8630, 9493, 10442,
    11487, 12635, 13899, 15289, 16818, 18500, 20350, 22385, 24623, 27086, 29794,
    32767,
  ];
  const IMA_INDEX = [-1, -1, -1, -1, 2, 4, 6, 8, -1, -1, -1, -1, 2, 4, 6, 8];

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
      this.adpcm = [{ predictor: 0, index: 0 }, { predictor: 0, index: 0 }];
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
      this.adpcm = [{ predictor: 0, index: 0 }, { predictor: 0, index: 0 }];
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
      if (view.getUint16(12, true) !== CHANNELS) return;
      if (view.getUint32(8, true) !== RATE) return;
      const format = view.getUint16(14, true);
      if (format === FORMAT_PCM) {
        const pcm = new Int16Array(buffer, HEADER);
        for (let i = 0; i + 1 < pcm.length; i += 2) {
          this.pushFrame(pcm[i] / 32768, pcm[i + 1] / 32768);
        }
        this.samples += pcm.length / 2;
        return;
      }
      if (format !== FORMAT_ADPCM && format !== FORMAT_ADPCM_RESET) return;
      if (format === FORMAT_ADPCM_RESET) {
        this.adpcm = [{ predictor: 0, index: 0 }, { predictor: 0, index: 0 }];
      }
      const bytes = new Uint8Array(buffer, HEADER);
      for (let i = 0; i < bytes.length; i++) {
        const byte = bytes[i];
        this.pushFrame(this.decodeADPCM(0, byte & 0x0f) / 32768,
          this.decodeADPCM(1, byte >> 4) / 32768);
      }
      this.samples += bytes.length;
    }

    pushFrame(left, right) {
      const ring = this.ring;
      if (this.size + 2 > ring.length) {
        this.read = (this.read + 2) % ring.length;
        this.size -= 2;
      }
      ring[this.write] = left;
      ring[(this.write + 1) % ring.length] = right;
      this.write = (this.write + 2) % ring.length;
      this.size += 2;
    }

    decodeADPCM(channel, nibble) {
      const state = this.adpcm[channel];
      const step = IMA_STEP[state.index];
      let diff = step >> 3;
      if (nibble & 4) diff += step;
      if (nibble & 2) diff += step >> 1;
      if (nibble & 1) diff += step >> 2;
      state.predictor += (nibble & 8) ? -diff : diff;
      if (state.predictor > 32767) state.predictor = 32767;
      else if (state.predictor < -32768) state.predictor = -32768;
      state.index += IMA_INDEX[nibble];
      if (state.index < 0) state.index = 0;
      else if (state.index > 88) state.index = 88;
      return state.predictor;
    }
  }

  window.CrossDeskPCMAudio = new PCMAudio();
})();
