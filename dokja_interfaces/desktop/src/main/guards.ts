const REMOTE_PROTOCOLS = new Set(["http:", "https:", "ws:", "wss:", "ftp:"]);

function parse(url: string | undefined): URL | undefined {
  if (!url) return undefined;
  try {
    return new URL(url);
  } catch {
    return undefined;
  }
}

// The same page, ignoring the query and the fragment.
export function isSameDocument(a: string | undefined, b: string | undefined): boolean {
  const first = parse(a);
  const second = parse(b);
  return (
    first !== undefined &&
    second !== undefined &&
    first.protocol === second.protocol &&
    first.host === second.host &&
    first.pathname === second.pathname
  );
}

// IPC from any other page (a frame, a navigation that slipped through) is not the app talking.
export function isTrustedSender(senderUrl: string | undefined, appUrl: string): boolean {
  return isSameDocument(senderUrl, appUrl);
}

// The renderer never loads anything from the network: its assets are local and the data arrives
// over IPC. Only the development server is let through, and only when one is configured.
export function shouldBlockRequest(url: string, devServerUrl?: string): boolean {
  const target = parse(url);
  if (!target) return true;
  if (!REMOTE_PROTOCOLS.has(target.protocol)) return false;
  const dev = parse(devServerUrl);
  return !(dev && target.host === dev.host);
}
