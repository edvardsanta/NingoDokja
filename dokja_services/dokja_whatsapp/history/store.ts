import { appendFile, chmod, mkdir, readFile, readdir, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";

// Who wrote a recorded message. A phone number or a name never goes in: only this label.
export type Who = "owner" | "bot" | "contact" | "member";

export type HistoryEntry = {
  // Grows by one per message of the conversation; it is also the cursor of a page.
  seq: number;
  id: string;
  at: string;
  who: Who;
  text: string;
};

export const pageSize = 30;
const maxTextChars = 4000;
const maxEntriesPerConversation = 2000;
const aliasPattern = /^[a-z0-9][a-z0-9_-]{0,31}$/;

type Options = {
  dir: string;
  retentionMs: number;
  now?: () => number;
};

// The text of the conversations the owner named, one file per alias in a folder of its own.
// Nothing else is kept: no media, no numbers, no names. It is trimmed to a retention period and
// to a number of entries, and a file is only ever read or written under a valid alias.
export class HistoryStore {
  private readonly loaded = new Map<string, HistoryEntry[]>();
  private chain: Promise<unknown> = Promise.resolve();
  private readonly now: () => number;

  constructor(private readonly options: Options) {
    this.now = options.now ?? Date.now;
  }

  // Creates the folder (readable by its owner only) and drops what is past the retention.
  async init(): Promise<void> {
    await mkdir(this.options.dir, { recursive: true, mode: 0o700 });
    await chmod(this.options.dir, 0o700);
    await this.purge();
  }

  // Adds one message. A message seen before (same id) is not added twice.
  append(alias: string, entry: { id: string; at: number; who: Who; text: string }): Promise<void> {
    return this.serial(async () => {
      const entries = await this.entries(alias);
      const text = entry.text.trim().slice(0, maxTextChars);
      if (!text || !entry.id || entries.some((known) => known.id === entry.id)) {
        return;
      }
      const created: HistoryEntry = {
        seq: (entries[entries.length - 1]?.seq ?? 0) + 1,
        id: entry.id,
        at: new Date(entry.at).toISOString(),
        who: entry.who,
        text,
      };
      entries.push(created);
      await appendFile(this.file(alias), `${JSON.stringify(created)}\n`, { mode: 0o600 });
      if (entries.length > maxEntriesPerConversation) {
        entries.splice(0, entries.length - maxEntriesPerConversation);
        await this.rewrite(alias, entries);
      }
    });
  }

  // The newest `pageSize` messages older than `before` (a cursor from an earlier page), oldest first.
  // The cursor of the next page is only given when this page is full.
  page(alias: string, before?: number): Promise<{ messages: HistoryEntry[]; before: string }> {
    return this.serial(async () => {
      const entries = await this.entries(alias);
      const older = before === undefined ? entries : entries.filter((entry) => entry.seq < before);
      const messages = older.slice(-pageSize);
      const full = older.length > pageSize;
      return { messages, before: full && messages.length > 0 ? String(messages[0].seq) : "" };
    });
  }

  // Forgets whatever is past the retention, in every conversation, and rewrites the files.
  purge(): Promise<void> {
    return this.serial(async () => {
      let names: string[];
      try {
        names = await readdir(this.options.dir);
      } catch {
        return;
      }
      const limit = this.now() - this.options.retentionMs;
      for (const name of names) {
        const alias = name.replace(/\.jsonl$/, "");
        if (!name.endsWith(".jsonl") || !aliasPattern.test(alias)) {
          continue;
        }
        const kept = (await this.entries(alias)).filter((entry) => Date.parse(entry.at) >= limit);
        this.loaded.set(alias, kept);
        if (kept.length === 0) {
          this.loaded.delete(alias);
          await rm(this.file(alias), { force: true });
        } else {
          await this.rewrite(alias, kept);
        }
      }
    });
  }

  private async entries(alias: string): Promise<HistoryEntry[]> {
    const known = this.loaded.get(alias);
    if (known) {
      return known;
    }
    const entries: HistoryEntry[] = [];
    try {
      for (const line of (await readFile(this.file(alias), "utf8")).split("\n")) {
        const entry = parseEntry(line);
        if (entry) {
          entries.push(entry);
        }
      }
    } catch {
      // No file yet: an empty conversation.
    }
    this.loaded.set(alias, entries);
    return entries;
  }

  private file(alias: string): string {
    if (!aliasPattern.test(alias)) {
      throw new Error("invalid conversation alias");
    }
    return path.join(this.options.dir, `${alias}.jsonl`);
  }

  private async rewrite(alias: string, entries: HistoryEntry[]): Promise<void> {
    const target = this.file(alias);
    const temporary = `${target}.tmp`;
    await writeFile(temporary, entries.map((entry) => `${JSON.stringify(entry)}\n`).join(""), { mode: 0o600 });
    await rename(temporary, target);
  }

  private serial<T>(work: () => Promise<T>): Promise<T> {
    const next = this.chain.then(work, work);
    this.chain = next.catch(() => undefined);
    return next;
  }
}

function parseEntry(line: string): HistoryEntry | undefined {
  if (!line.trim()) {
    return undefined;
  }
  try {
    const value = JSON.parse(line) as Partial<HistoryEntry>;
    if (
      typeof value.seq === "number" &&
      typeof value.id === "string" &&
      typeof value.at === "string" &&
      typeof value.text === "string" &&
      (value.who === "owner" || value.who === "bot" || value.who === "contact" || value.who === "member")
    ) {
      return { seq: value.seq, id: value.id, at: value.at, who: value.who, text: value.text };
    }
  } catch {
    // A damaged line is dropped, not fatal.
  }
  return undefined;
}
