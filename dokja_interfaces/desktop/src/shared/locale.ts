export type Locale = "en" | "pt";

// Any value that starts with "pt" selects Portuguese; everything else is English, the default.
export function localeFromTag(tag: string | undefined): Locale {
  return tag?.trim().toLowerCase().startsWith("pt") ? "pt" : "en";
}
