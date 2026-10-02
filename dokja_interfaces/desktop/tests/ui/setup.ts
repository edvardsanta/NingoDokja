import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

import { clearPreviewCache } from "../../src/renderer/cards/use_preview.js";

afterEach(() => {
  cleanup();
  clearPreviewCache();
});
