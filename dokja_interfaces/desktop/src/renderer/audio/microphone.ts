export type VoiceAudio = { start(): Promise<void>; play(encoded: string): Promise<void>; stop(): void };
export type VoiceAudioFactory = (samples: (pcm: Float32Array) => void, ended: () => void) => VoiceAudio;

export class BrowserVoiceAudio implements VoiceAudio {
  private stopped = false;
  private stream?: MediaStream;
  private context?: AudioContext;
  private processor?: AudioWorkletNode;
  private playing?: AudioBufferSourceNode;
  constructor(private samples: (pcm: Float32Array) => void, private ended: () => void) {}
  async start(): Promise<void> {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: {
      channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true,
    }, video: false });
    if (this.stopped) { stream.getTracks().forEach(track => track.stop()); return; }
    this.stream = stream;
    for (const track of stream.getTracks()) track.onended = () => { if (!this.stopped) this.ended(); };
    const context = new AudioContext({ sampleRate: 16000 });
    this.context = context;
    if (context.sampleRate !== 16000) throw new Error("Unsupported capture rate");
    await context.audioWorklet.addModule(new URL("./pcm_worklet.js", import.meta.url).href);
    if (this.stopped) return;
    const processor = new AudioWorkletNode(context, "dokja-microphone");
    this.processor = processor;
    processor.port.onmessage = event => { if (!this.stopped && event.data instanceof Float32Array) this.samples(event.data); };
    const source = context.createMediaStreamSource(stream);
    source.connect(processor); processor.connect(context.destination);
    await context.resume();
  }
  async play(encoded: string): Promise<void> {
    const context = this.context;
    if (this.stopped || !context) return;
    const raw = atob(encoded);
    const bytes = Uint8Array.from(raw, character => character.charCodeAt(0));
    const audio = await context.decodeAudioData(bytes.buffer);
    if (this.stopped) return;
    const source = context.createBufferSource(); source.buffer = audio; source.connect(context.destination);
    this.playing = source;
    await new Promise<void>(resolve => { source.onended = () => { if (this.playing === source) this.playing = undefined; resolve(); }; source.start(); });
  }
  stop(): void {
    if (this.stopped) return;
    this.stopped = true;
    this.stream?.getTracks().forEach(track => { track.onended = null; track.stop(); });
    this.processor?.disconnect(); this.processor?.port.close();
    this.playing?.stop(); this.playing = undefined;
    if (this.context && this.context.state !== "closed") void this.context.close().catch(() => undefined);
  }
}
