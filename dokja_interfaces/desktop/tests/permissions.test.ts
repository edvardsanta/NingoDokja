import test from "node:test";
import assert from "node:assert/strict";

import { installPermissionPolicy } from "../src/main/permissions.js";

type Target = Parameters<typeof installPermissionPolicy>[0];

// Electron's Session cannot be created outside Electron, so this records the handlers the policy
// installs and calls them the way Electron would.
function fakeSession() {
  const handlers: {
    request?: (contents: unknown, permission: string, callback: (granted: boolean) => void) => void;
    check?: () => boolean;
    device?: () => boolean;
  } = {};
  const session = {
    setPermissionRequestHandler: (handler: typeof handlers.request) => {
      handlers.request = handler;
    },
    setPermissionCheckHandler: (handler: typeof handlers.check) => {
      handlers.check = handler;
    },
    setDevicePermissionHandler: (handler: typeof handlers.device) => {
      handlers.device = handler;
    },
  };
  return { handlers, session: session as unknown as Target };
}

const PERMISSIONS = [
  "media",
  "mediaKeySystem",
  "geolocation",
  "notifications",
  "clipboard-read",
  "clipboard-sanitized-write",
  "display-capture",
  "fullscreen",
  "midi",
  "openExternal",
  "hid",
  "serial",
  "usb",
];

test("the session denies every permission request, check and device", () => {
  const { handlers, session } = fakeSession();
  installPermissionPolicy(session);

  assert.ok(handlers.request && handlers.check && handlers.device, "all three handlers are installed");
  for (const permission of PERMISSIONS) {
    const answers: boolean[] = [];
    handlers.request(null, permission, (granted) => answers.push(granted));
    assert.deepEqual(answers, [false], `request for ${permission}`);
  }
  assert.equal(handlers.check(), false);
  assert.equal(handlers.device(), false);
});
