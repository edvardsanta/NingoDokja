import { readFileSync } from "node:fs";
import { join } from "node:path";
import { parseEnv } from "node:util";

const TOKEN = "DOKJA_COMMUNICATIONS_TOKEN";

// Read only the operator credential in the trusted main process. Parsing an env
// file never executes shell expressions or exports other credentials.
export function communicationsToken(env: NodeJS.ProcessEnv, root?: string, envFile?: string): string | undefined {
  if (Object.hasOwn(env, TOKEN)) return env[TOKEN];
  const path = envFile ?? (root ? join(root, ".env") : undefined);
  if (!path) return undefined;
  try {
    return parseEnv(readFileSync(path, "utf8"))[TOKEN];
  } catch (error) {
    if (!envFile && (error as NodeJS.ErrnoException).code === "ENOENT") return undefined;
    // Parser/filesystem errors may contain private input; never expose them.
    throw new Error("Could not read the desktop communications env file.");
  }
}
