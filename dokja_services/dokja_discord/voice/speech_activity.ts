// A transport guard, not a speech recognizer. Ignore isolated quiet/impulsive
// packets before spending STT work; semantic eligibility is decided by the domain.
export class SpeechActivity {
  private voicedMs = 0;
  private runMs = 0;
  private longestRunMs = 0;

  push(pcm: Buffer): void {
    const frameBytes = 3840; // 20 ms of stereo 48 kHz PCM16
    for (let offset = 0; offset + 4 <= pcm.length; offset += frameBytes) {
      const end = Math.min(pcm.length, offset + frameBytes);
      let energy = 0;
      let samples = 0;
      for (let i = offset; i + 1 < end; i += 2) {
        const value = pcm.readInt16LE(i);
        energy += value * value;
        samples++;
      }
      const ms = samples / (48000 * 2) * 1000;
      if (Math.sqrt(energy / Math.max(1, samples)) >= 220) {
        this.voicedMs += ms;
        this.runMs += ms;
        this.longestRunMs = Math.max(this.longestRunMs, this.runMs);
      } else this.runMs = 0;
    }
  }

  get transcribable(): boolean { return this.voicedMs >= 120 && this.longestRunMs >= 80; }
  get sustained(): boolean { return this.voicedMs >= 450 && this.longestRunMs >= 250; }
}
