// AudioWorklet processor: resamples microphone input to 16 kHz PCM16 (~100 ms blocks).
class Pcm16 extends AudioWorkletProcessor {
  constructor() {
    super();
    this.ratio = sampleRate / 16000;
    this.acc = 0;
    this.buf = new Int16Array(1600);
    this.n = 0;
  }
  process(inputs) {
    const ch = inputs[0] && inputs[0][0];
    if (!ch) return true;
    let peak = 0;
    for (let i = 0; i < ch.length; i++) {
      const s = ch[i];
      const a = s < 0 ? -s : s;
      if (a > peak) peak = a;
      this.acc += 1;
      if (this.acc >= this.ratio) {
        this.acc -= this.ratio;
        this.buf[this.n++] = Math.max(-1, Math.min(1, s)) * 0x7fff;
        if (this.n === this.buf.length) {
          this.port.postMessage({ pcm: this.buf.buffer, peak }, [this.buf.buffer]);
          this.buf = new Int16Array(1600);
          this.n = 0;
          peak = 0;
        }
      }
    }
    return true;
  }
}
registerProcessor('loom-pcm16', Pcm16);
