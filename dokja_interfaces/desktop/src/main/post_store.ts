import { copyFile, mkdir, mkdtemp, readdir, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { isRecord, text } from "../shared/records.js";
import { POST_SCHEME, type PostView } from "../shared/posts.js";
import type { Downloaded, DownloadOptions } from "./media_fetch.js";
import { createTranslator, type Translate } from "../renderer/i18n/i18n.js";

// The folders a run leaves in the system's temporary directory start with this, so the next run
// can sweep what a crash left behind.
const TEMP_PREFIX = "ningo-posts-";
// A temporary folder older than this is from a run that is over.
const STALE_MS = 24 * 60 * 60 * 1000;
// How many posts one run keeps; the oldest are erased first.
const MAX_POSTS = 30;
const DOCUMENT_ID = /^post-[0-9a-f]{16}$/;

const EXTENSIONS: Record<string, string> = {
  "image/png": "png",
  "image/jpeg": "jpg",
  "image/gif": "gif",
  "image/webp": "webp",
  "image/avif": "avif",
  "video/mp4": "mp4",
  "video/webm": "webm",
};

type HeldFile = { path: string; name: string; type: string };

type HeldPost = {
  documentId: string;
  url: string;
  author: string;
  caption: string;
  published: string;
  files: HeldFile[];
};

export type Download = (url: string, path: string, options?: DownloadOptions) => Promise<Downloaded>;

export type PostStoreOptions = {
  // Where the folder window opens the first time, and where posts go when there is no window.
  savedRoot: string;
  download: Download;
  // The local origin a post reader hands its files out from, if the owner named one.
  localOrigin?: string;
  // Asks the owner where to save, starting from a folder; undefined when the owner cancelled.
  // Without it, posts go to savedRoot without asking (tests).
  pickFolder?: (defaultPath: string, destination: "library" | "computer") => Promise<string | undefined>;
  // Words the Library entry's labels in the owner's language; English when absent.
  translate?: Translate;
  // Only for tests: where the temporary folder is made.
  tempParent?: string;
  now?: () => number;
};

// Holds the posts the owner opened during this run. Their files live in a temporary folder that
// is erased when the app quits (and by the next run, after a crash). Saving copies them to a
// folder that stays.
export class PostStore {
  private readonly posts = new Map<string, HeldPost>();
  private readonly saved = new Map<string, { folder: string; created: boolean }>();
  // The folder the owner chose last in this run, where the window opens next time.
  private lastParent?: string;
  private tempRoot?: Promise<string>;

  constructor(private readonly options: PostStoreOptions) {}

  private temp(): Promise<string> {
    this.tempRoot ??= mkdtemp(join(this.options.tempParent ?? tmpdir(), TEMP_PREFIX));
    return this.tempRoot;
  }

  // Downloads the media of a post the orchestrator answered, and gives the screen the post with
  // addresses it can only open through the shell. A file that cannot be fetched is counted, not fatal.
  async hold(projected: unknown): Promise<unknown> {
    if (!isRecord(projected) || !isRecord(projected.post)) return projected;
    const post = projected.post;
    const documentId = text(post.documentId);
    if (!DOCUMENT_ID.test(documentId)) return { status: "refused", refused: "failed" };
    await this.forget(documentId);
    const folder = join(await this.temp(), documentId);
    await mkdir(folder, { recursive: true });

    const files: HeldFile[] = [];
    let missing = 0;
    const media = Array.isArray(post.media) ? post.media.filter(isRecord) : [];
    for (const [index, item] of media.entries()) {
      const partial = join(folder, `${index}.part`);
      try {
        const downloaded = await this.options.download(text(item.url), partial, {
          ...(this.options.localOrigin ? { localOrigin: this.options.localOrigin } : {}),
        });
        const name = `${String(index + 1).padStart(2, "0")}.${EXTENSIONS[downloaded.type] ?? "bin"}`;
        const path = join(folder, name);
        await rm(path, { force: true });
        await copyFile(partial, path);
        await rm(partial, { force: true });
        files.push({ path, name, type: downloaded.type });
      } catch {
        missing += 1;
      }
    }
    const held: HeldPost = {
      documentId,
      url: text(post.url),
      author: text(post.author),
      caption: text(post.caption),
      published: text(post.published),
      files,
    };
    this.posts.set(documentId, held);
    await this.trim();
    const view: PostView = {
      documentId,
      url: held.url,
      author: held.author,
      caption: held.caption,
      published: held.published,
      media: files.map((file, index) => ({
        kind: file.type.startsWith("video/") ? "video" : "image",
        src: `${POST_SCHEME}://${documentId}/${index}`,
      })),
      missing,
    };
    return { status: "ok", post: view };
  }

  // The file behind a ningo-post address, or nothing when it is not one this run holds.
  file(address: string): HeldFile | undefined {
    let url: URL;
    try {
      url = new URL(address);
    } catch {
      return undefined;
    }
    if (url.protocol !== `${POST_SCHEME}:`) return undefined;
    const index = /^\/(\d{1,2})$/.exec(url.pathname)?.[1];
    return index === undefined ? undefined : this.posts.get(url.hostname)?.files[Number(index)];
  }

  // The Library entry for a post this run holds: its text, where it came from and the names of
  // the files saved with it. The screen names the post only by its id.
  ingestPayload(documentId: string, savedNames: string[]): Record<string, unknown> | undefined {
    const post = this.posts.get(documentId);
    if (!post) return undefined;
    const t = this.options.translate ?? createTranslator("en");
    const lines = [
      post.caption,
      "",
      ...(post.author ? [t("post_entry_author", { author: post.author })] : []),
      ...(post.published ? [t("post_entry_published", { date: post.published })] : []),
      t("post_entry_link", { link: post.url }),
      ...(savedNames.length > 0 ? [t("post_entry_files", { files: savedNames.join(", ") })] : []),
    ];
    const firstLine = post.caption.split("\n").find((line) => line.trim() !== "")?.trim() ?? "";
    const title = (firstLine || (post.author ? t("post_entry_title_by", { author: post.author }) : t("post_entry_title"))).slice(0, 300);
    return {
      title,
      body: lines.join("\n").trim(),
      kind: "post",
      source_ref: post.url,
      source_id: post.documentId,
    };
  }

  // Asks the owner, in the system's own folder window, where to keep a held post, then copies its
  // files there, into a folder named by the post's id. Says which names it kept, or "cancelled"
  // when the owner closed the window: choosing the folder is the owner's yes.
  async save(documentId: string): Promise<string[] | "cancelled" | undefined> {
    const post = this.posts.get(documentId);
    if (!post) return undefined;
    const parent = this.options.pickFolder
      ? await this.options.pickFolder(this.lastParent ?? this.options.savedRoot, "library")
      : this.options.savedRoot;
    if (!parent) return "cancelled";
    this.lastParent = parent;
    const folder = join(parent, documentId);
    const existed = (await stat(folder).catch(() => undefined))?.isDirectory() === true;
    await mkdir(folder, { recursive: true });
    for (const file of post.files) await copyFile(file.path, join(folder, file.name));
    this.saved.set(documentId, { folder, created: !existed });
    return post.files.map((file) => file.name);
  }

  // Copies media to a folder chosen on this computer without creating a Library entry.
  async saveLocally(documentId: string): Promise<number | "cancelled" | undefined> {
    const post = this.posts.get(documentId);
    if (!post) return undefined;
    const parent = this.options.pickFolder
      ? await this.options.pickFolder(this.lastParent ?? this.options.savedRoot, "computer")
      : this.options.savedRoot;
    if (!parent) return "cancelled";
    this.lastParent = parent;

    const urlPath = new URL(post.url).pathname;
    const shortcode = urlPath.split("/").filter(Boolean).at(-1) ?? documentId;
    const stem = /^[A-Za-z0-9_-]{1,100}$/.test(shortcode) ? shortcode : documentId;
    let folder = join(parent, stem);
    let suffix = 2;
    while (await stat(folder).then(() => true, () => false)) {
      folder = join(parent, `${stem} (${suffix++})`);
    }
    await mkdir(folder);
    try {
      for (const file of post.files) await copyFile(file.path, join(folder, file.name));
    } catch (error) {
      await rm(folder, { recursive: true, force: true });
      throw error;
    }
    this.saved.set(documentId, { folder, created: true });
    return post.files.length;
  }

  // The folder of a post saved in this run, or nothing: the page cannot name any other folder.
  async savedFolder(documentId: string): Promise<string | undefined> {
    const saved = DOCUMENT_ID.test(documentId) ? this.saved.get(documentId) : undefined;
    if (!saved) return undefined;
    const info = await stat(saved.folder).catch(() => undefined);
    return info?.isDirectory() ? saved.folder : undefined;
  }

  // Takes a saved post's folder away again, when the Library refused it. A folder that was there
  // before this save (the same post saved there earlier) is left alone.
  async unsave(documentId: string): Promise<void> {
    const saved = this.saved.get(documentId);
    if (!saved) return;
    this.saved.delete(documentId);
    if (saved.created) await rm(saved.folder, { recursive: true, force: true });
  }

  has(documentId: string): boolean {
    return this.posts.has(documentId);
  }

  private async forget(documentId: string): Promise<void> {
    this.posts.delete(documentId);
    if (this.tempRoot) await rm(join(await this.tempRoot, documentId), { recursive: true, force: true });
  }

  private async trim(): Promise<void> {
    for (const oldest of [...this.posts.keys()]) {
      if (this.posts.size <= MAX_POSTS) break;
      await this.forget(oldest);
    }
  }

  // Erases this run's temporary folder. Called when the app quits.
  async dispose(): Promise<void> {
    this.posts.clear();
    if (this.tempRoot) await rm(await this.tempRoot, { recursive: true, force: true });
  }

  // Erases what an earlier run that did not quit cleanly left in the temporary directory.
  async sweep(): Promise<void> {
    const parent = this.options.tempParent ?? tmpdir();
    const now = (this.options.now ?? Date.now)();
    const own = this.tempRoot ? await this.tempRoot : undefined;
    for (const name of await readdir(parent).catch(() => [] as string[])) {
      const path = join(parent, name);
      if (!name.startsWith(TEMP_PREFIX) || path === own) continue;
      const info = await stat(path).catch(() => undefined);
      if (info?.isDirectory() && now - info.mtimeMs > STALE_MS) await rm(path, { recursive: true, force: true });
    }
  }
}
