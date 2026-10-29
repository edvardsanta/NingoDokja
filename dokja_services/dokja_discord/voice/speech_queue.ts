// Only accepted reply text enters this queue. Preview frames never become audio.
export function speechSegments(text: string): string[] {
  const sentences = [...new Intl.Segmenter("pt", { granularity: "sentence" }).segment(text.trim())]
    .map(segment => segment.segment);
  const segments: string[] = [];
  for (const sentence of sentences) {
    let chunk = "";
    for (const word of sentence.trim().split(/\s+/u)) {
      if (chunk && chunk.length + word.length + 1 > 280) {
        segments.push(chunk);
        chunk = "";
      }
      chunk = chunk ? `${chunk} ${word}` : word;
    }
    if (chunk) segments.push(chunk);
  }
  return segments;
}

type Prepared<T> = { value: T; error?: never } | { error: unknown; value?: never };

// Prepare one sentence ahead while the current one is playing. The first
// sentence can start without waiting for synthesis of the rest of the reply.
export async function playSpeechQueue<T>(text: string, options: {
  signal?: AbortSignal;
  prepare(text: string): Promise<T>;
  play(text: string, prepared: T): Promise<void>;
  waitForFloor?(): Promise<void>;
}): Promise<void> {
  const segments = speechSegments(text);
  const prepare = (segment: string): Promise<Prepared<T>> => {
    options.signal?.throwIfAborted();
    return options.prepare(segment).then(value => ({ value }), error => ({ error }));
  };
  let pending = segments.length ? prepare(segments[0]) : undefined;
  for (let index = 0; pending && index < segments.length; index++) {
    const prepared = await pending;
    options.signal?.throwIfAborted();
    if ("error" in prepared) throw prepared.error;
    await options.waitForFloor?.();
    options.signal?.throwIfAborted();
    pending = index + 1 < segments.length ? prepare(segments[index + 1]) : undefined;
    await options.play(segments[index], prepared.value);
  }
}
