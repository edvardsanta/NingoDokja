import type { Session } from "electron";
import { isTrustedSender } from "./guards.js";

type PolicyTarget = Pick<
  Session,
  "setPermissionRequestHandler" | "setPermissionCheckHandler" | "setDevicePermissionHandler"
>;

// Fullscreen is available only to the app's own top-level document. Other
// permissions and requests from subframes retain the default denial.
export function installPermissionPolicy(session: PolicyTarget, appUrl?: string): void {
  const trusted = (contents: { getURL(): string } | null, details: { isMainFrame: boolean; requestingUrl?: string }) =>
    Boolean(appUrl && contents && details.isMainFrame &&
      isTrustedSender(contents.getURL(), appUrl) && isTrustedSender(details.requestingUrl, appUrl));
  session.setPermissionRequestHandler((contents, permission, callback, details) =>
    callback(permission === "fullscreen" && trusted(contents, details)));
  session.setPermissionCheckHandler((contents, permission, _origin, details) =>
    permission === "fullscreen" && trusted(contents, details));
  session.setDevicePermissionHandler(() => false);
}
