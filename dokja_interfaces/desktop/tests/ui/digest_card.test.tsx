import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { DigestCard } from "../../src/renderer/cards/digest.js";
import { WorkspacePanel } from "../../src/renderer/cards/workspace_panel.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { DigestStatus } from "../../src/shared/replies.js";
import type { Locale } from "../../src/shared/locale.js";
import type { Transport, TransportResult } from "../../src/shared/transport.js";
import {
  DIGEST_STATUS,
  OFF,
  deferred,
  digestItem,
  digestPage,
  digestSource,
  failure,
  minutesAgo,
  ok,
  routedTransport,
} from "./support.js";

function renderCard(transport: Transport, locale: Locale = "en") {
  return render(
    <I18nProvider locale={locale}>
      {/* the readings are a view of the Library, whose panel holds the refresh */}
      <WorkspacePanel trail={["Library"]}>
        <DigestCard transport={transport} />
      </WorkspacePanel>
    </I18nProvider>,
  );
}

const itemCalls = (calls: Array<{ type: string; payload?: Record<string, unknown> }>) =>
  calls.filter((call) => call.type === "digest.items").map((call) => call.payload);

const titles = () => [...document.querySelectorAll(".digest li .digest-title")].map((node) => node.textContent);

const status = (overrides: Partial<DigestStatus>): DigestStatus => ({ ...DIGEST_STATUS, ...overrides });

describe("the digest card", () => {
  it("shows how the sources are doing and the first page of what they gave", async () => {
    const { transport, calls } = routedTransport({
      "digest.status": ok(DIGEST_STATUS),
      "digest.items": ok(digestPage([digestItem(1), digestItem(2, { summary: "", source: "", published: "" })])),
    });
    renderCard(transport);

    // what each state has, joined; a state with none is not said
    await screen.findByText("2 working · 1 failing · 1 off");
    expect(screen.queryByText(/starting|unusable/)).toBeNull();
    await screen.findByText("Entry 1");
    expect(titles()).toEqual(["Entry 1", "Entry 2"]);
    screen.getByText("Alpha source, 2 hours ago");
    screen.getByText("Summary 1");
    screen.getByText("updated 10 minutes ago");
    // an entry with nothing but a title shows only that
    const second = document.querySelectorAll(".digest li")[1] as HTMLElement;
    expect(second.querySelectorAll("p")).toHaveLength(1);
    expect(calls.map((call) => call.type)).toEqual(["digest.status", "digest.items"]);
    expect(itemCalls(calls)).toEqual([{ limit: 8, offset: 0 }]);
  });

  it("lists the sources, open when one of them has a problem", async () => {
    const { transport } = routedTransport({
      "digest.status": ok(DIGEST_STATUS),
      "digest.items": ok(digestPage([digestItem(1)])),
    });

    renderCard(transport);

    const sources = (await screen.findByText("Sources")).closest("details") as HTMLDetailsElement;
    expect(sources.open).toBe(true);
    const rows = [...sources.querySelectorAll("li")].map((row) => row.textContent);
    expect(rows).toEqual([
      "Alpha sourceworkingitems: 3, updated 10 minutes ago",
      "Beta sourceworkingitems: 2, updated 2 hours ago",
      "Gamma sourcefailingexit status 2",
      "Delta sourceoff",
    ]);
    expect([...sources.querySelectorAll("li")].map((row) => row.getAttribute("data-tone"))).toEqual([
      "ok", "ok", "problem", "off",
    ]);
  });

  it("shows processing plugins as available before their first on-demand use", async () => {
    const processing = [
      digestSource("media-library", { name: "Media Library", state: "pending" }),
      digestSource("audio-pages", { name: "Pages", state: "pending" }),
    ];
    const { transport } = routedTransport({
      "digest.status": ok(status({ providers: processing })),
      "digest.items": ok(digestPage([])),
    });
    renderCard(transport);

    const providers = (await screen.findByText("Processing plugins")).closest("details") as HTMLDetailsElement;
    expect([...providers.querySelectorAll("li")].map((row) => row.textContent)).toEqual([
      "Media Libraryavailable",
      "Pagesavailable",
    ]);
    expect([...providers.querySelectorAll("li")].map((row) => row.getAttribute("data-tone"))).toEqual(["ok", "ok"]);
  });

  it("keeps the source list closed while everything works", async () => {
    const healthy = status({
      failed: 0, disabled: 0,
      sources: [digestSource("alpha", { name: "Alpha source", items: 1, lastOk: minutesAgo(5) })],
    });
    const { transport } = routedTransport({ "digest.status": ok(healthy), "digest.items": ok(digestPage([digestItem(1)])) });
    renderCard(transport);

    const sources = (await screen.findByText("Sources")).closest("details") as HTMLDetailsElement;
    expect(sources.open).toBe(false);
  });

  it("says a source whose state it does not know is unknown", async () => {
    const odd = status({ sources: [digestSource("x", { name: "Odd", state: "unknown" })] });
    const { transport } = routedTransport({ "digest.status": ok(odd), "digest.items": ok(digestPage([])) });
    renderCard(transport);

    expect((await screen.findByText("Odd", { selector: ".service-name" })).parentElement?.textContent).toBe("Oddunknown");
  });

  describe("showing more", () => {
    it("asks for a longer page, keeping what is shown while it loads", async () => {
      const first = Array.from({ length: 8 }, (_, i) => digestItem(i + 1));
      const longer = Array.from({ length: 13 }, (_, i) => digestItem(i + 1));
      const slow = deferred<TransportResult>();
      const { transport, calls } = routedTransport({
        "digest.status": ok(DIGEST_STATUS),
        "digest.items": [ok(digestPage(first, { total: 13, more: 5 })), () => slow.promise],
      });
      renderCard(transport);

      await screen.findByText("Entry 8");
      fireEvent.click(screen.getByRole("button", { name: "Show 5 more" }));

      // the answer is still on its way: the eight stay, the button waits
      await screen.findByRole("status");
      expect(titles()).toHaveLength(8);
      expect((screen.getByRole("button", { name: "Show 5 more" }) as HTMLButtonElement).disabled).toBe(true);

      await act(async () => {
        slow.resolve(ok(digestPage(longer, { total: 13, more: 0 })));
      });
      await screen.findByText("Entry 13");
      expect(titles()).toHaveLength(13);
      expect(screen.queryByRole("button", { name: /Show .* more/ })).toBeNull();
      expect(itemCalls(calls)).toEqual([{ limit: 8, offset: 0 }, { limit: 16, offset: 0 }]);
    });

    it("offers one step, or what is left when that is less", async () => {
      const { transport } = routedTransport({
        "digest.status": ok(DIGEST_STATUS),
        "digest.items": ok(digestPage([digestItem(1)], { total: 4, more: 3 })),
      });
      renderCard(transport);

      await screen.findByRole("button", { name: "Show 3 more" });
    });

    it("stops at the most the digest hands over", async () => {
      const calls: Array<number> = [];
      const transport: Transport = {
        async request(type, payload) {
          if (type === "digest.status") return ok(DIGEST_STATUS);
          const limit = Number(payload?.limit);
          calls.push(limit);
          return ok(digestPage(Array.from({ length: limit }, (_, i) => digestItem(i + 1)), { total: 80, more: 80 - limit }));
        },
        preview: async () => ({ ok: false, error: { code: "denied", message: "" } }),
      };
      renderCard(transport);

      for (let step = 0; step < 6; step += 1) {
        const button = await screen.findByRole("button", { name: /Show \d+ more/ });
        await waitFor(() => expect((button as HTMLButtonElement).disabled).toBe(false));
        fireEvent.click(button);
      }
      await screen.findByText("Showing the first 50. The rest stays in the digest.");
      expect(screen.queryByRole("button", { name: /Show \d+ more/ })).toBeNull();
      expect(calls).toEqual([8, 16, 24, 32, 40, 48, 50]);
    });

    it("keeps the list and offers a retry when a longer page cannot be had", async () => {
      const first = Array.from({ length: 8 }, (_, i) => digestItem(i + 1));
      const { transport } = routedTransport({
        "digest.status": ok(DIGEST_STATUS),
        "digest.items": [
          ok(digestPage(first, { total: 20, more: 12 })),
          failure("timeout"),
          ok(digestPage([...first, ...Array.from({ length: 8 }, (_, i) => digestItem(i + 9))], { total: 20, more: 4 })),
        ],
      });
      renderCard(transport);

      await screen.findByText("Entry 8");
      fireEvent.click(screen.getByRole("button", { name: "Show 8 more" }));
      expect((await screen.findByRole("alert")).textContent).toBe("The orchestrator took too long to answer.");
      expect(titles()).toHaveLength(8);

      fireEvent.click(screen.getByRole("button", { name: "Try again" }));
      await screen.findByText("Entry 16");
      expect(titles()).toHaveLength(16);
    });
  });

  describe("when nothing is being followed", () => {
    const quiet = (overrides: Partial<DigestStatus>) => {
      const route = routedTransport({ "digest.status": ok(status({ ok: 0, failed: 0, pending: 0, disabled: 0, invalid: 0, sources: [], items: 0, ...overrides })) });
      renderCard(route.transport);
      return route;
    };

    it("says there is no source yet, and asks for no items", async () => {
      const { calls } = quiet({});
      await screen.findByText("No source yet. Add a plugin to the feeds service and its items appear here.");
      expect(screen.queryByText("Sources")).toBeNull();
      expect(calls.map((call) => call.type)).toEqual(["digest.status"]);
    });

    it("says the service has no plugins directory", async () => {
      quiet({ configured: false });
      expect((await screen.findByText("I'm reading nothing right now.")).className).toBe("voice");
      await screen.findByText("The feeds service has no plugins directory.");
      // what the operator sets is a line of its own
      expect(screen.getByText("FEEDS_PLUGINS_DIR is not set.").className).toBe("detail");
    });

    it("says why the directory cannot be read", async () => {
      quiet({ directoryError: "permission denied" });
      await screen.findByText("The plugins directory cannot be read: permission denied");
      // a directory that cannot be read is a problem, not a quiet shelf: no voice, and the fix is in System
      expect(screen.queryByText("I'm reading nothing right now.")).toBeNull();
      screen.getByRole("button", { name: "Open System" });
    });

    it("says no source is enabled, and shows the sources, open", async () => {
      const { calls } = quiet({
        disabled: 1,
        sources: [digestSource("idle", { name: "Idle one", state: "disabled" })],
      });
      await screen.findByText("I'm reading nothing right now.");
      await screen.findByText("No source is enabled.");
      expect(screen.getByText("Set \"enabled\": true in a plugin's manifest.").className).toBe("detail");
      expect(((await screen.findByText("Sources")).closest("details") as HTMLDetailsElement).open).toBe(true);
      expect(calls.map((call) => call.type)).toEqual(["digest.status"]);
    });
  });

  it("says there is nothing to read yet when sources work but gave nothing", async () => {
    const { transport } = routedTransport({ "digest.status": ok(DIGEST_STATUS), "digest.items": ok(digestPage([])) });
    renderCard(transport);

    expect((await screen.findByText("Nothing has reached me yet.")).className).toBe("voice");
    screen.getByText("Nothing to read yet.");
  });

  it("says a switched-off service is switched off and asks for no items", async () => {
    const { transport, calls } = routedTransport({ "digest.status": ok(OFF) });
    renderCard(transport);

    await screen.findByText("Switched off.");
    expect(calls.map((call) => call.type)).toEqual(["digest.status"]);
  });

  it("says so when only the items are switched off", async () => {
    const { transport } = routedTransport({ "digest.status": ok(DIGEST_STATUS), "digest.items": ok(OFF) });
    renderCard(transport);

    await screen.findByText("Switched off.");
  });

  it("explains a failure of the status and retries on request", async () => {
    const { transport, calls } = routedTransport({
      "digest.status": [failure("unavailable"), ok(DIGEST_STATUS)],
      "digest.items": ok(digestPage([digestItem(1)])),
    });
    renderCard(transport);

    expect((await screen.findByRole("alert")).textContent).toBe("The orchestrator is not answering. Is it running?");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await screen.findByText("Entry 1");
    expect(calls.filter((call) => call.type === "digest.status")).toHaveLength(2);
  });

  it("asks again for both when refreshed", async () => {
    const { transport, calls } = routedTransport({
      "digest.status": ok(DIGEST_STATUS),
      "digest.items": [ok(digestPage([digestItem(1)])), ok(digestPage([digestItem(2)]))],
    });
    renderCard(transport);

    await screen.findByText("Entry 1");
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("Entry 2");
    // the first pair is the opening; the refresh asks for both again, in whichever order the
    // sections' effects run (the readings are below the card and run first)
    expect(calls.slice(0, 2).map((call) => call.type)).toEqual(["digest.status", "digest.items"]);
    expect(calls.slice(2).map((call) => call.type).toSorted()).toEqual(["digest.items", "digest.status"]);
    expect(titles()).toEqual(["Entry 2"]);
  });

  describe("refreshing", () => {
    const many = (count: number) => Array.from({ length: count }, (_, index) => digestItem(index + 1));

    it("keeps the page that was loaded and the filters that were typed, and reads that page again", async () => {
      const { transport, calls } = routedTransport({
        "digest.status": ok(DIGEST_STATUS),
        "digest.items": (payload) =>
          Promise.resolve(ok(digestPage(many(Number(payload?.limit)), { total: 30, more: 30 - Number(payload?.limit) }))),
      });
      renderCard(transport);
      await screen.findByText("Entry 8");
      fireEvent.click(screen.getByRole("button", { name: "Show 8 more" }));
      await screen.findByText("Entry 16");
      const search = screen.getByLabelText("Search readings") as HTMLInputElement;
      fireEvent.change(search, { target: { value: "stoics" } });

      fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
      await waitFor(() => expect(calls.filter((call) => call.type === "digest.status")).toHaveLength(2));
      await waitFor(() => expect((screen.getByRole("button", { name: "Refresh" }) as HTMLButtonElement).disabled).toBe(false));

      // the same input is still there with its text, and the 16 are still shown
      expect(screen.getByLabelText("Search readings")).toBe(search);
      expect(search.value).toBe("stoics");
      expect(titles()).toHaveLength(16);
      expect(itemCalls(calls).at(-1)).toEqual({ limit: 16, offset: 0 });
    });

    it("keeps the readings on screen while the answer is on its way, and holds the refresh", async () => {
      const slow = deferred<TransportResult>();
      const { transport } = routedTransport({
        "digest.status": [ok(DIGEST_STATUS), () => slow.promise],
        "digest.items": ok(digestPage([digestItem(1)])),
      });
      renderCard(transport);
      await screen.findByText("Entry 1");

      fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
      await waitFor(() => expect((screen.getByRole("button", { name: "Refresh" }) as HTMLButtonElement).disabled).toBe(true));
      expect(titles()).toEqual(["Entry 1"]);
      expect(screen.queryByText("Loading…")).toBeNull();

      await act(async () => slow.resolve(ok(DIGEST_STATUS)));
      await waitFor(() => expect((screen.getByRole("button", { name: "Refresh" }) as HTMLButtonElement).disabled).toBe(false));
    });
  });

  it("shows what a source wrote as text, never as markup", async () => {
    const hostile = '<img src="x" onerror="window.__owned = true">';
    const { transport } = routedTransport({
      "digest.status": ok(DIGEST_STATUS),
      "digest.items": ok(digestPage([digestItem(1, { title: hostile, summary: hostile, source: hostile })])),
    });
    const view = renderCard(transport);

    await screen.findAllByText(hostile, { exact: false });
    expect(view.container.querySelectorAll("img")).toHaveLength(0);
    expect((window as unknown as { __owned?: boolean }).__owned).toBeUndefined();
  });

  it("gives the screen no address to follow", async () => {
    const { transport } = routedTransport({
      "digest.status": ok(DIGEST_STATUS),
      "digest.items": ok(digestPage([digestItem(1)])),
    });
    const view = renderCard(transport);

    await screen.findByText("Entry 1");
    expect(view.container.querySelectorAll("a, [href]")).toHaveLength(0);
  });

  it("speaks Portuguese when asked", async () => {
    const { transport } = routedTransport({
      "digest.status": ok(DIGEST_STATUS),
      "digest.items": ok(digestPage([digestItem(1)], { total: 6, more: 5 })),
    });
    renderCard(transport, "pt");

    await screen.findByText("2 funcionando · 1 com falha · 1 desligadas");
    screen.getByRole("heading", { name: "Leituras" });
    await screen.findByText("Alpha source, há 2 horas");
    screen.getByRole("button", { name: "Mostrar mais 5" });
    screen.getByText("atualizado há 10 minutos");
    const sources = screen.getByText("Fontes").closest("details") as HTMLDetailsElement;
    expect(within(sources).getAllByText("funcionando")).toHaveLength(2);
    within(sources).getByText("com falha");
    within(sources).getByText("desligada");
    within(sources).getByText("exit status 2");
  });
});


it("shows processing plugins with their errors even without configured sources", async () => {
  const { transport, calls } = routedTransport({
    "digest.status": ok(status({ configured: false, sources: [], providers: [digestSource("media-library", { name: "Media library", state: "failed", error: "cache unavailable" })] })),
  });
  renderCard(transport);
  const plugins = (await screen.findByText("Processing plugins")).closest("details") as HTMLDetailsElement;
  expect(plugins.open).toBe(true);
  expect(await screen.findByText("Media library")).toBeTruthy();
  expect(await screen.findByText(/cache unavailable/)).toBeTruthy();
  expect(itemCalls(calls)).toEqual([]);
});

it("saves a reading to the Library by its id alone, and says what happened", async () => {
  const { transport, calls } = routedTransport({
    "digest.status": ok(DIGEST_STATUS),
    "digest.items": ok(digestPage([digestItem(1)])),
    "knowledge.ingest": ok({ count: 1, created: 1, updated: 0, unchanged: 0, chunks: 4, degraded: false, reason: "" }),
  });
  renderCard(transport);

  fireEvent.click(await screen.findByRole("button", { name: "Save to the Library: Entry 1" }));
  await screen.findByText(/^Added/);
  expect(calls.filter((call) => call.type === "knowledge.ingest").map((call) => call.payload)).toEqual([{ mode: "reading", reading: "item-1" }]);
});


describe("reading previews and filters", () => {
  it("expands full plain text without interpreting markup", async () => {
    const fullText = "First paragraph.\n\n<img src=x onerror=alert(1)>\n" + "Full passage. ".repeat(100);
    const { transport } = routedTransport({ "digest.status": ok(DIGEST_STATUS), "digest.items": ok(digestPage([digestItem(1, { previewText: fullText })])) });
    renderCard(transport);
    await screen.findByText("Entry 1");
    expect(document.querySelector('.reading-preview-text')).toBeNull();
    fireEvent.click(screen.getByText("Preview"));
    await waitFor(() => expect(document.querySelector('.reading-preview-text')?.textContent).toBe(fullText.trim()));
    expect(document.querySelector('.reading-preview img')).toBeNull();
    expect(document.querySelector('.reading-preview-text')?.textContent?.length).toBeGreaterThan(1000);
  });

  it("loads a meme through the shell only when its preview is expanded", async () => {
    const requests: string[] = [];
    const url = "https://example.com/digest-preview-image.jpg";
    const { transport } = routedTransport({ "digest.status": ok(DIGEST_STATUS), "digest.items": ok(digestPage([digestItem(1, { mediaUrl: url })])) }, async address => {
      requests.push(address);
      return { ok: true, dataUrl: "data:image/png;base64,aW1hZ2U=" };
    });
    renderCard(transport);
    await screen.findByText("Entry 1");
    expect(requests).toEqual([]);
    fireEvent.click(screen.getByText("Preview"));
    const image = await screen.findByAltText("Entry 1");
    expect(image.getAttribute("src")).toBe("data:image/png;base64,aW1hZ2U=");
    expect(requests).toEqual([url]);
    screen.getByText("Source excerpt; full text was not supplied.");
  });

  it("previews a video with controls and no autoplay", async () => {
    const { transport } = routedTransport({ "digest.status": ok(DIGEST_STATUS), "digest.items": ok(digestPage([digestItem(1, { mediaUrl: "https://example.com/digest-preview-video.mp4" })])) }, async () => ({ ok: true, dataUrl: "data:video/mp4;base64,dmlkZW8=" }));
    renderCard(transport);
    await screen.findByText("Entry 1");
    fireEvent.click(screen.getByText("Preview"));
    await waitFor(() => expect(document.querySelector('.reading-preview video')).not.toBeNull());
    const video = document.querySelector('video')!;
    expect(video.controls).toBe(true);
    expect(video.autoplay).toBe(false);
  });

  it("submits source, search and dates to the domain and clears them", async () => {
    const { transport, calls } = routedTransport({ "digest.status": ok(DIGEST_STATUS), "digest.items": [ok(digestPage([digestItem(1)], { more: 3 })), ok(digestPage([])), ok(digestPage([digestItem(1)]))] });
    renderCard(transport);
    await screen.findByText("Entry 1");
    fireEvent.change(screen.getByLabelText("Source"), { target: { value: "beta" } });
    fireEvent.change(screen.getByLabelText("Search readings"), { target: { value: "charity" } });
    fireEvent.change(screen.getByLabelText("From date (UTC)"), { target: { value: "2026-10-01" } });
    fireEvent.change(screen.getByLabelText("To date (UTC)"), { target: { value: "2026-10-27" } });
    fireEvent.click(screen.getByText("Apply filters"));
    await screen.findByText("0 matching readings");
    expect(itemCalls(calls).at(-1)).toEqual({ limit: 8, offset: 0, source: "beta", query: "charity", from: "2026-10-01", to: "2026-10-27" });
    fireEvent.click(screen.getByText("Clear filters"));
    await screen.findByText("Entry 1");
    expect(itemCalls(calls).at(-1)).toEqual({ limit: 8, offset: 0 });
  });

  it("refuses an inverted date interval before requesting another page", async () => {
    const { transport, calls } = routedTransport({ "digest.status": ok(DIGEST_STATUS), "digest.items": ok(digestPage([digestItem(1)])) });
    renderCard(transport);
    await screen.findByText("Entry 1");
    fireEvent.change(screen.getByLabelText("From date (UTC)"), { target: { value: "2026-10-27" } });
    fireEvent.change(screen.getByLabelText("To date (UTC)"), { target: { value: "2026-10-01" } });
    expect(screen.getByRole("button", { name: "Apply filters" }).hasAttribute("disabled")).toBe(true);
    screen.getByRole("alert");
    expect(itemCalls(calls)).toHaveLength(1);
  });
});


it("keeps reading filters when loading the next page", async () => {
  const { transport, calls } = routedTransport({ "digest.status": ok(DIGEST_STATUS), "digest.items": [
    ok(digestPage([digestItem(1)])),
    ok(digestPage([digestItem(2)], { total: 20, more: 12 })),
    ok(digestPage([digestItem(2), digestItem(3)], { total: 20, more: 4 })),
  ] });
  renderCard(transport);
  await screen.findByText("Entry 1");
  fireEvent.change(screen.getByLabelText("Source"), { target: { value: "beta" } });
  fireEvent.click(screen.getByText("Apply filters"));
  await screen.findByText("Entry 2");
  fireEvent.click(screen.getByRole("button", { name: "Show 8 more" }));
  await screen.findByText("Entry 3");
  expect(itemCalls(calls).at(-1)).toEqual({ limit: 16, offset: 0, source: "beta" });
});

it("offers a retry when a reading media preview fails", async () => {
  let attempts = 0;
  const { transport } = routedTransport({ "digest.status": ok(DIGEST_STATUS), "digest.items": ok(digestPage([digestItem(1, { mediaUrl: "https://example.com/retry-reading-image.png" })])) }, async () => {
    attempts++;
    return attempts === 1 ? { ok: false, error: { code: "unavailable", message: "Preview unavailable" } } : { ok: true, dataUrl: "data:image/png;base64,aW1hZ2U=" };
  });
  renderCard(transport);
  await screen.findByText("Entry 1");
  fireEvent.click(screen.getByText("Preview"));
  fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
  await screen.findByAltText("Entry 1");
  expect(attempts).toBe(2);
});
