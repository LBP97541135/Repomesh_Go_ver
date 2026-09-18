import { access } from "node:fs/promises";
import { fileURLToPath } from "node:url";

export async function resolve(specifier, context, nextResolve) {
  try {
    return await nextResolve(specifier, context);
  } catch (error) {
    if (!specifier.startsWith(".") || /\.[a-z0-9]+$/i.test(specifier) || context.parentURL === undefined) throw error;
    for (const extension of [".ts", ".tsx"]) {
      const candidate = new URL(`${specifier}${extension}`, context.parentURL);
      try {
        await access(fileURLToPath(candidate));
        return { shortCircuit: true, url: candidate.href };
      } catch {
        // Continue to the next source extension.
      }
    }
    throw error;
  }
}
