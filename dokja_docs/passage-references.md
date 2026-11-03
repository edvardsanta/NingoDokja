# Exact passage references

Chat references identify retrieved passages individually, including separate passages
from the same book. Quotations, paraphrases and source-dependent interpretations
are prompted to cite their supporting passage numbers. Interpretation remains the
assistant's judgment: a valid location does not establish that its interpretation
is correct or that every statement needing evidence received a citation.

## Extraction and storage

The existing path remains Interface → Event/Request → Orchestrator → Knowledge
Domain → Service. Reading-host plugins return optional `units` alongside each
`document.extract.v1` document. Legacy plugins without units still work. Update
the example extraction plugin and its host together: an older host rejects fields
it does not recognize.

- PDF units preserve every physical page, including empty pages, and the PDF's
  page label. A physical page index differs from an edition's printed label; the
  reader displays both without inventing chapter or page metadata.
- EPUB units preserve spine order, the archive member path, chapter label and
  extracted chapter text. Locations do not depend on font size or viewport width.
- Units must reconstruct the ingested body exactly. Located chunks preserve
  original unit characters, stay within that unit and carry code-point offsets.
  Ordinary documents continue to use the existing heading-aware chunker.
- Knowledge retains original PDF/EPUB bytes and units in `document_originals`.
  Original bytes have a SHA-256 `file_hash`; `version` additionally fingerprints
  the extracted units. Re-extraction changes cannot silently reinterpret an old
  location, even when the binary file did not change.
- Replacing a document retains its previous originals. Deleting a document
  cascades to its retained originals. Backups and database sizing must account
  for original files, base64 encoding and retained versions.

Direct uploads and fetched PDF/EPUB documents use the same storage mechanism.
Files read through Feeds retain their original in Knowledge through the domain;
file bytes never enter the bounded task workspace.

## Citation contract

A retrieved passage may carry a `locator` containing:

| Field | Meaning |
| --- | --- |
| `version` | Fingerprint of original bytes and extraction units |
| `format` | `pdf` or `epub` |
| `unit` | Zero-based index in the extraction's ordered units |
| `start`, `end` | Unicode code-point offsets in that unit; end exclusive |
| `page`, `label` | One-based physical PDF page and available page label |
| `href`, `label` | EPUB chapter archive member and chapter label |
| `passage_id` | Stable identity of the retrieved span |

Each supplied passage has its own citation number. The conversation archive
preserves every cited passage's excerpt and locator, rather than only the first
excerpt of a document. A shortened context budget adjusts the saved end offset
without adding an ellipsis to the source text. The desktop projects complete
bounded excerpts and allows every number in a grouped reference to open separately.

Cited quotations are checked against the passage numbers attached to them. Finding
the quote elsewhere in the same book cannot validate an unrelated reference.
This verifies text correspondence, not semantic support for an interpretation.

## Reader

`knowledge.list` accepts an optional locator when requesting one document by
`source_id`. It returns a `reading` result: `resolved`, `unavailable` or
`unresolved`. Resolution checks the stored version, unit, format-specific
location and range before returning the original, selected unit and navigation.
Normal document listings do not return original bytes.

The desktop verifies the original byte fingerprint before rendering. PDF pages
render through a lazy-loaded PDF renderer; highlights use the page's text-run
geometry. EPUB chapter content is read from the original archive and rendered
as semantic text, without executing book scripts or importing source URLs,
attributes or styles. The extracted unit is also available with the exact span
highlighted and surrounding context.

Visual alignment normalizes wrapping and ligatures. Repeated text is resolved
using the whole unit's alignment, or a unique matching passage. Ambiguous visual
matches show an explicit notice and retain the exact extracted context; they do
not silently highlight a different occurrence. Page/chapter navigation preserves
the original version and includes a return-to-citation action.

Existing documents need re-importing their originals to gain locations. Existing
archived citations without locators remain document references; neither page
numbers nor exact anchors are invented retroactively. Scanned PDFs still need a
future OCR extraction path; this change does not add OCR. No Calibre installation
or integration is required.

## Compatibility and verification

The event envelope, action names, commands, environment variables, ports and
compose volumes are unchanged. SQLite migrates from schema 1/2 to schema 3;
PostgreSQL migrates schema 1 to 2. The verified import/export utility accepts
SQLite schema 1/2/3 and preserves locators and original versions in both directions.
Older binaries refuse the newer schema: back up before upgrading, and use the
matching application and schema versions when rolling back. Restoring an older
snapshot loses writes after that snapshot; do not downgrade a live database.

Regression tests cover physical versus printed pages, empty pages, EPUB chapter
identity, repeated sentences, Unicode offsets, replaced editions, restart,
deleted originals, malformed locations, grouped references, source markup safety
and cited quotations pointing to the wrong passage. Built Electron checks open
PDF and EPUB references from an archived conversation through real IPC.
