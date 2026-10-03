import type { Session } from "electron";

type PolicyTarget = Pick<
  Session,
  "setPermissionRequestHandler" | "setPermissionCheckHandler" | "setDevicePermissionHandler"
>;

// Electron approves every permission request (camera, microphone, notifications...) unless the
// session has a handler, so this session denies them all. The microphone gets its own rule when
// voice arrives.
export function installPermissionPolicy(session: PolicyTarget): void {
  session.setPermissionRequestHandler((_contents, _permission, callback) => callback(false));
  session.setPermissionCheckHandler(() => false);
  session.setDevicePermissionHandler(() => false);
}
