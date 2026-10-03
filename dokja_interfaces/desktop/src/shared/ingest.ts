// What the screen may add to the research base, and how much. The shell enforces these limits; the
// screen checks the same numbers first so it can say why before anything is sent. The sizes are lower
// than the knowledge service's own (2 million characters, 20 MB) on purpose; the shapes of a kind and
// of an id are the service's.
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
  kindChars: 32,
  referenceChars: 2000,
  idChars: 200,
} as const;

// One lowercase word: note, article, book, paper...
export const INGEST_KIND = /^[a-z][a-z0-9_-]{0,31}$/;
// What a document is known by: letters, digits and . _ : / # @ -
export const INGEST_ID = /^[A-Za-z0-9._:/#@-]{1,200}$/;

// The length of the base64 text of that many bytes.
export const base64Length = (bytes: number): number => Math.ceil(bytes / 3) * 4;
