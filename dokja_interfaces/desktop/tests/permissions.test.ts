import test from "node:test";
import assert from "node:assert/strict";

import { installPermissionPolicy } from "../src/main/permissions.js";

type Target = Parameters<typeof installPermissionPolicy>[0];

// Electron's Session cannot be created outside Electron, so this records the handlers the policy
// installs and calls them the way Electron would.
function fakeSession() {
  const handlers: {
    request?: (contents: unknown, permission: string, callback: (granted: boolean) => void, details?: { isMainFrame: boolean; requestingUrl?: string }) => void;
    check?: (contents?: unknown, permission?: string, origin?: string, details?: { isMainFrame: boolean; requestingUrl?: string }) => boolean;
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

test("fullscreen is allowed only for the trusted app main frame", () => {
  const { handlers, session } = fakeSession();
  const page = "file:///app/index.html";
  installPermissionPolicy(session, page);
  const contents = { getURL: () => page };
  const details = { isMainFrame: true, requestingUrl: page };
  const request = (permission: string, frame = details, sender: unknown = contents) => {
    let granted: boolean | undefined;
    handlers.request!(sender, permission, (answer: boolean) => { granted = answer; }, frame);
    return granted;
  };
  assert.equal(request("fullscreen"), true);
  assert.equal(handlers.check!(contents, "fullscreen", "file://", details), true);
  for (const permission of PERMISSIONS.filter((p) => p !== "fullscreen")) assert.equal(request(permission), false);
  for (const frame of [{ ...details, isMainFrame: false }, { ...details, requestingUrl: "https://example.com/" }]) {
    assert.equal(request("fullscreen", frame), false);
    assert.equal(handlers.check!(contents, "fullscreen", "file://", frame), false);
  }
  assert.equal(request("fullscreen", details, null), false);
  assert.equal(request("fullscreen", details, { getURL: () => "file:///other.html" }), false);
});
