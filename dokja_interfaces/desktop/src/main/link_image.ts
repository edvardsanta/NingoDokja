// Draws the link code the orchestrator hands over as rows of "0" and "1" (one character per module,
// no quiet zone) as an image the screen can show. The rows are data: the shell builds the picture
// itself, so nothing the interface or the orchestrator sent is ever shown as markup. Anything that is
// not a square grid within the sizes a QR code has is refused.
const MIN_SIDE = 21;
const MAX_SIDE = 177;
const QUIET_ZONE = 4;

export function linkImage(rows: unknown): string | undefined {
  if (!Array.isArray(rows)) return undefined;
  const side = rows.length;
  if (side < MIN_SIDE || side > MAX_SIDE) return undefined;
  if (!rows.every((row) => typeof row === "string" && row.length === side && /^[01]+$/.test(row))) return undefined;

  // One horizontal run of dark modules is one rectangle, which keeps the picture small.
  const runs: string[] = [];
  (rows as string[]).forEach((row, y) => {
    for (const run of row.matchAll(/1+/g)) {
      runs.push(`M${run.index + QUIET_ZONE} ${y + QUIET_ZONE}h${run[0].length}v1h-${run[0].length}z`);
    }
  });
  const size = side + QUIET_ZONE * 2;
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}" shape-rendering="crispEdges">` +
    `<rect width="${size}" height="${size}" fill="#fff"/><path d="${runs.join("")}" fill="#000"/></svg>`;
  return `data:image/svg+xml;base64,${Buffer.from(svg).toString("base64")}`;
}
