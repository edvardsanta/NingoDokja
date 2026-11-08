const RATE = 16000;
const MAX_SAMPLES = RATE * 8;

// A bounded transport segmenter. The server's acoustic gate decides actual speech.
export class PcmSegments {
  private chunks: Float32Array[] = [];
  private samples = 0;
  private voiced = 0;
  private silence = 0;
  reset(): void { this.chunks = []; this.samples = this.voiced = this.silence = 0; }
  push(chunk: Float32Array): Uint8Array | undefined {
    if (!chunk.length || chunk.length > 4096) return;
    let energy = 0;
    for (const value of chunk) energy += value * value;
    const audible = Math.sqrt(energy / chunk.length) >= 0.008;
    this.chunks.push(chunk.slice()); this.samples += chunk.length;
    if (audible) { this.voiced += chunk.length; this.silence = 0; }
    else this.silence += chunk.length;
    if (!this.voiced && this.samples > RATE / 5) {
      const dropped = this.chunks.shift(); this.samples -= dropped?.length ?? 0;
    }
    if (this.samples < MAX_SAMPLES && !(this.voiced && this.silence >= RATE * 0.6)) return;
    const enough = this.voiced >= RATE * 0.16;
    const samples = new Float32Array(Math.min(this.samples, MAX_SAMPLES));
    let offset = 0;
    for (const part of this.chunks) {
      const size = Math.min(part.length, samples.length - offset);
      samples.set(part.subarray(0, size), offset); offset += size;
      if (offset === samples.length) break;
    }
    this.reset();
    return enough ? pcmWav(samples) : undefined;
  }
}

export function pcmWav(samples: Float32Array): Uint8Array {
  const bytes = new Uint8Array(44 + samples.length * 2);
  const view = new DataView(bytes.buffer);
  const text = (offset: number, value: string) => { for (let i = 0; i < value.length; i++) bytes[offset + i] = value.charCodeAt(i); };
  text(0, "RIFF"); view.setUint32(4, bytes.length - 8, true); text(8, "WAVEfmt ");
  view.setUint32(16, 16, true); view.setUint16(20, 1, true); view.setUint16(22, 1, true);
  view.setUint32(24, RATE, true); view.setUint32(28, RATE * 2, true);
  view.setUint16(32, 2, true); view.setUint16(34, 16, true); text(36, "data");
  view.setUint32(40, samples.length * 2, true);
  for (let i = 0; i < samples.length; i++) view.setInt16(44 + i * 2, Math.round(Math.max(-1, Math.min(1, samples[i] ?? 0)) * 32767), true);
  return bytes;
}

export function audioBase64(bytes: Uint8Array): string {
  let raw = "";
  for (let i = 0; i < bytes.length; i += 8192) raw += String.fromCharCode(...bytes.subarray(i, i + 8192));
  return btoa(raw);
}
