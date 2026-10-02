// What the screen may add to the research base, and how much. The shell enforces these limits; the
// screen checks the same numbers first so it can say why before anything is sent. They are lower
// than the knowledge service's own (2 million characters, 20 MB) on purpose.
export const INGEST_MODES = ["note", "address", "file"] as const;
export type IngestMode = (typeof INGEST_MODES)[number];

export const INGEST_LIMITS = {
  titleChars: 300,
  noteChars: 200_000,
  addressChars: 2048,
  fileBytes: 8 * 1024 * 1024,
  filenameChars: 120,
  tags: 8,
  tagChars: 40,
} as const;

// The length of the base64 text of that many bytes.
export const base64Length = (bytes: number): number => Math.ceil(bytes / 3) * 4;
