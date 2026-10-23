import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "../../src/renderer/app.js";
import { CARDS } from "../../src/renderer/cards/registry.js";
import en from "../../src/renderer/i18n/locales/en.json";
import pt from "../../src/renderer/i18n/locales/pt.json";
import type { ActionType } from "../../src/shared/actions.js";
import type { Locale } from "../../src/shared/locale.js";
import type { TransportResult } from "../../src/shared/transport.js";
import { ALL_UP, KNOWLEDGE_STATUS, OFF, SERVICES, deferred, failure, ok, routedTransport, statusReply } from "./support.js";

const wordmark = () => screen.getByRole("heading", { level: 1 });
const tabNamed = (name: string) => screen.getByRole("tab", { name });
const selectedTab = () =>
  screen.getAllByRole("tab").find((tab) => tab.getAttribute("aria-selected") === "true")?.querySelector(".tab-label")?.textContent;

type Route = TransportResult | (() => Promise<TransportResult>);

// Every card the screen shows needs a route. The cards a test does not care about answer that
// their service is switched off, so adding a card means adding one line here.
const QUIET: Partial<Record<ActionType, Route>> = {
  "digest.status": ok(OFF),
  "memory.status": ok(OFF),
  "knowledge.status": ok(OFF),
};

const screenTransport = (health: Route, overrides: Partial<Record<ActionType, Route>> = {}) =>
  routedTransport({ ...QUIET, "ningo.status": health, ...overrides });

describe("the screen", () => {
  it("shows the name and the workspaces, with the conversation open, in the language it was given", async () => {
    const { transport } = screenTransport(statusReply(ALL_UP));
    render(<App transport={transport} locale="pt" />);

    expect(wordmark().textContent).toBe("Ningo");
    expect(screen.getByRole("tablist", { name: "Áreas de trabalho" })).toBeTruthy();
    expect(screen.getAllByRole("tab").map((tab) => tab.textContent)).toEqual([
      "Conversa",
      "Biblioteca",
      "Caderno",
      "Mensagens",
      "Sistema",
    ]);
    expect(selectedTab()).toBe("Conversa");
    await waitFor(() => expect(wordmark().dataset.pulse).toBe("calm"));
    expect(selectedTab()).toBe("Conversa");
    expect(document.documentElement.lang).toBe("pt");
  });

  it("opens System instead when the orchestrator does not answer", async () => {
    const { transport } = screenTransport(failure("unavailable"));
    render(<App transport={transport} locale="en" />);

    await waitFor(() => expect(selectedTab()).toBe("System"));
    expect(tabNamed("System").querySelector(".tab-count")?.textContent).toBe("offline");
  });

  it("stays where the owner went when the status fails afterwards", async () => {
    const gate = deferred<TransportResult>();
    const { transport } = screenTransport(() => gate.promise);
    render(<App transport={transport} locale="en" />);

    fireEvent.click(tabNamed("Notebook"));
    gate.resolve(failure("unavailable"));
    await waitFor(() => expect(wordmark().dataset.pulse).toBe("lost"));
    expect(selectedTab()).toBe("Notebook");
  });

  it("shows the dot of the chat service on the conversation", async () => {
    const { transport } = screenTransport(statusReply(SERVICES));
    render(<App transport={transport} locale="en" />);

    await waitFor(() => expect(tabNamed("Talk").querySelector(".tab-dot")?.getAttribute("data-tone")).toBe("problem"));
  });

  it("asks nothing for a card nobody opened", async () => {
    const { transport, calls } = screenTransport(statusReply(ALL_UP));
    render(<App transport={transport} locale="en" />);

    await waitFor(() => expect(wordmark().dataset.pulse).toBe("calm"));
    expect(calls.map((call) => call.type)).toEqual(["ningo.status"]);
    expect(screen.queryByRole("heading", { name: "Experience memory" })).toBeNull();
  });

  it("keeps a knowledge search while the sources are looked at", async () => {
    const { transport, calls } = screenTransport(statusReply(ALL_UP), { "knowledge.status": ok(KNOWLEDGE_STATUS) });
    render(<App transport={transport} locale="en" />);

    fireEvent.click(tabNamed("Library"));
    const field = (await screen.findByRole("searchbox")) as HTMLInputElement;
    fireEvent.change(field, { target: { value: "stoics" } });
    fireEvent.click(screen.getByRole("tab", { name: "Readings" }));
    await screen.findByRole("heading", { name: "Reading digest" });
    fireEvent.click(screen.getByRole("tab", { name: "Documents" }));

    expect((screen.getByRole("searchbox") as HTMLInputElement).value).toBe("stoics");
    expect(calls.filter((call) => call.type === "knowledge.status")).toHaveLength(1);
    expect(calls.filter((call) => call.type === "digest.status")).toHaveLength(1);
  });

  it("gives the Library its views as tabs the arrows move between", async () => {
    const { transport } = screenTransport(statusReply(ALL_UP));
    render(<App transport={transport} locale="en" />);

    fireEvent.click(tabNamed("Library"));
    const views = screen.getByRole("tablist", { name: "Library" });
    const documents = screen.getByRole("tab", { name: "Documents" });
    expect(documents.getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(views, { key: "ArrowRight" });
    const readings = screen.getByRole("tab", { name: "Readings" });
    expect(readings.getAttribute("aria-selected")).toBe("true");
    expect(document.activeElement).toBe(readings);
    await screen.findByRole("heading", { name: "Reading digest" });
    expect(screen.getByRole("region", { name: "Library › Readings" })).toBeTruthy();
  });

  it("opens each workspace from the sidebar", async () => {
    const { transport, calls } = screenTransport(statusReply(ALL_UP));
    render(<App transport={transport} locale="en" />);

    fireEvent.click(tabNamed("Notebook"));
    await screen.findByRole("heading", { name: "Experience memory" });
    fireEvent.click(tabNamed("Library"));
    await screen.findByRole("heading", { name: "Library" });
    fireEvent.click(screen.getByRole("tab", { name: "Readings" }));
    await screen.findByRole("heading", { name: "Reading digest" });

    await waitFor(() =>
      expect(calls.map((call) => call.type)).toEqual([
        "ningo.status",
        "memory.status",
        "knowledge.status",
        "digest.status",
      ]),
    );
  });

  it("keeps the other cards when one fails", async () => {
    const { transport } = screenTransport(statusReply(ALL_UP), { "memory.status": failure("unavailable") });
    render(<App transport={transport} locale="en" />);
    await waitFor(() => expect(wordmark().dataset.pulse).toBe("calm"));

    fireEvent.click(tabNamed("Notebook"));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("The orchestrator is not answering. Is it running?");

    fireEvent.click(tabNamed("Library"));
    await screen.findByText("Switched off.");
    fireEvent.click(tabNamed("System"));
    expect(screen.getByRole("tabpanel").textContent).toContain("2 up, 0 with problems, 0 switched off");
  });

  it("has one card per kind, each kind once", () => {
    const kinds = CARDS.map((card) => card.kind);
    expect(kinds).toContain("health");
    expect(new Set(kinds).size).toBe(kinds.length);
  });

  describe("the pulse of the name", () => {
    const pulseFor = async (reply: Route) => {
      const { transport } = screenTransport(reply);
      render(<App transport={transport} locale="en" />);
      return transport;
    };

    it("is tuning while the status loads, then calm when everything answers", async () => {
      const gate = deferred<TransportResult>();
      await pulseFor(() => gate.promise);
      expect(wordmark().dataset.pulse).toBe("tuning");

      gate.resolve(statusReply(ALL_UP));
      await waitFor(() => expect(wordmark().dataset.pulse).toBe("calm"));
    });

    it("is unwell when a service has a problem", async () => {
      await pulseFor(statusReply(SERVICES));
      await waitFor(() => expect(wordmark().dataset.pulse).toBe("unwell"));
    });

    it("is lost when nothing answers", async () => {
      await pulseFor(failure("unavailable"));
      await waitFor(() => expect(wordmark().dataset.pulse).toBe("lost"));
    });
  });

  describe("languages do not leak into each other", () => {
    // Short messages ("up", "erro") hide inside other words, so only the longer ones are checked.
    const longer = (catalog: Record<string, string>) =>
      Object.values(catalog).filter((message) => message.length >= 7 && !message.includes("{"));
    // A message counts only as a whole word: "Conversa" is not a leak inside "Conversation".
    const shows = (text: string, message: string) =>
      new RegExp(`(?<!\\p{L})${message.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}(?!\\p{L})`, "u").test(text);

    const screenText = async (locale: Locale) => {
      const { transport } = screenTransport(statusReply(SERVICES));
      render(<App transport={transport} locale={locale} />);
      // every card, not only the open one: a closed workspace keeps its text in the page
      for (const tab of screen.getAllByRole("tab")) fireEvent.click(tab);
      await screen.findByText("Memes");
      await waitFor(() => expect(screen.queryAllByRole("status", { hidden: true })).toHaveLength(0));
      return document.body.textContent ?? "";
    };

    it("English shows nothing from the Portuguese catalog", async () => {
      const text = await screenText("en");
      const english = new Set(Object.values(en));
      for (const message of longer(pt)) {
        if (!english.has(message)) expect(shows(text, message), message).toBe(false);
      }
    });

    it("Portuguese shows nothing from the English catalog", async () => {
      const text = await screenText("pt");
      const portuguese = new Set(Object.values(pt));
      for (const message of longer(en)) {
        if (!portuguese.has(message)) expect(shows(text, message), message).toBe(false);
      }
    });
  });
});
