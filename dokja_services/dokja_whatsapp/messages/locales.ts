import pt from "./locales/pt-BR.json" with { type: "json" };
import en from "./locales/en.json" with { type: "json" };

export function whatsappLocale(locale?: string): "pt-BR" | "en" {
  return !locale || locale.toLowerCase().startsWith("pt") ? "pt-BR" : "en";
}

export function whatsappMessage(locale: string | undefined, key: string): string {
  const catalog: Record<string, string> = whatsappLocale(locale) === "pt-BR" ? pt : en;
  return catalog[key] ?? catalog.failure!;
}
