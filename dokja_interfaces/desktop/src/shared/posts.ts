// A post link the owner opened: what the screen gets back from digest.link.read. Its media are
// files the shell downloaded into a temporary folder, reachable only through this scheme.
export const POST_SCHEME = "ningo-post";

// Why a link was not shown. The orchestrator's Digest domain names one of these; the screen words it.
export const POST_REFUSALS = ["unsupported", "login_required", "unavailable", "no_reader", "failed"] as const;
export type PostRefusal = (typeof POST_REFUSALS)[number];

export type PostMedia = { kind: "image" | "video"; src: string };

export type PostView = {
  documentId: string;
  url: string;
  author: string;
  caption: string;
  published: string;
  media: PostMedia[];
  // Files the reader listed that could not be downloaded.
  missing: number;
};

export type PostReply = { status: "ok"; post: PostView } | { status: "refused"; refused: PostRefusal };

