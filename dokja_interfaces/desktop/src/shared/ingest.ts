// What the screen may add to the research base. The shell enforces these limits; the screen checks the
// same numbers first so it can say why before anything is sent. A document's size has no limit: books
// are large, and only the memory of the machine bounds what can be read. The shapes of a kind and of
// an id are the service's.
export const INGEST_MODES = ["note", "address", "file"] as const;
export type IngestMode = (typeof INGEST_MODES)[number];

export const INGEST_LIMITS = {
  titleChars: 300,
  addressChars: 2048,
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

