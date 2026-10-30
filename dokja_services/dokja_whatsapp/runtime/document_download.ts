// Collect only the bounded, decrypted stream returned by the platform SDK.
export async function readDocumentStream(
  stream: AsyncIterable<Uint8Array> & { destroy(): unknown },
  maxBytes: number,
): Promise<Buffer> {
  const chunks: Buffer[] = [];
  let total = 0;
  try {
    for await (const chunk of stream) {
      const bytes = Buffer.from(chunk);
      total += bytes.length;
      if (total > maxBytes) {
        throw Object.assign(new Error("document too large"), { code: "document_too_large" });
      }
      chunks.push(bytes);
    }
  } finally {
    stream.destroy();
  }
  return Buffer.concat(chunks);
}
