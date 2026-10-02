import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { MemesCard } from "../../src/renderer/cards/memes.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Locale } from "../../src/shared/locale.js";
import type { Transport, TransportResult } from "../../src/shared/transport.js";
import {
  MEME_STATUS,
  OFF,
  deferred,
  failure,
  meme,
  memePage,
  ok,
  previewer,
  routedTransport,
} from "./support.js";

function renderCard(transport: Transport, locale: Locale = "en") {
  return render(
    <I18nProvider locale={locale}>
      <MemesCard transport={transport} />
    </I18nProvider>,
  );
}

const listCalls = (calls: Array<{ type: string; payload?: Record<string, unknown> }>) =>
  calls.filter((call) => call.type === "meme.list").map((call) => call.payload);

const items = () => [...document.querySelectorAll(".memes li")];
const titles = () => items().map((item) => item.querySelector(".meme-title")?.textContent);

describe("the memes card", () => {
  it("shows the queue counts and the first page of what is waiting", async () => {
    const { transport, calls } = routedTransport(
      {
        "meme.status": ok(MEME_STATUS),
        "meme.list": ok(memePage([meme(1), meme(2, { title: "" })], { total: 30 })),
      },
      previewer().preview,
    );
    renderCard(transport);

    await screen.findByText("30 waiting, 8 sent");
    await screen.findByText("Meme 1");
    expect(titles()).toEqual(["Meme 1", "untitled"]);
    screen.getByText("1 to 2 of 30");
    expect(calls.map((call) => call.type)).toEqual(["meme.status", "meme.list"]);
    expect(listCalls(calls)).toEqual([{ scope: "unsent", limit: 12, offset: 0 }]);
    expect(screen.getAllByText("added 2026-09-03")).toHaveLength(2);
  });

  it("previews each picture through the shell, and only what is a picture", async () => {
    const { asked, preview } = previewer(["broken"]);
    const { transport } = routedTransport(
      {
        "meme.status": ok(MEME_STATUS),
        "meme.list": ok(
          memePage([
            meme(1),
            meme(2, { url: "https://images.example/clip.mp4" }),
            meme(3, { url: "https://images.example/broken.png" }),
            meme(4, { url: "" }),
          ]),
        ),
      },
      preview,
    );
    const view = renderCard(transport);

    await waitFor(() => expect(view.container.querySelectorAll(".preview img")).toHaveLength(1));
    const image = view.container.querySelector(".preview img") as HTMLImageElement;
    expect(image.getAttribute("src")).toBe(`data:image/png;base64,${btoa("https://images.example/1.png")}`);
    expect(image.getAttribute("alt")).toBe("");
    expect(screen.getAllByText("video, no preview")).toHaveLength(2);
    await screen.findByText("no preview");
    expect(asked.sort()).toEqual(["https://images.example/1.png", "https://images.example/broken.png"]);
  });

  it("pages forward and back, and the buttons stop at the ends", async () => {
    const pages = [
      memePage([meme(1)], { total: 14, offset: 0 }),
      memePage([meme(13), meme(14)], { total: 14, offset: 12 }),
    ];
    const { transport, calls } = routedTransport(
      {
        "meme.status": ok(MEME_STATUS),
        "meme.list": [ok(pages[0]), ok(pages[1]), ok(pages[0])],
      },
      previewer().preview,
    );
    renderCard(transport);

    await screen.findByText("Meme 1");
    expect((screen.getByRole("button", { name: "Previous" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Next" }));

    await screen.findByText("Meme 13");
    expect((screen.getByRole("button", { name: "Next" }) as HTMLButtonElement).disabled).toBe(true);
    screen.getByText("13 to 14 of 14");
    fireEvent.click(screen.getByRole("button", { name: "Previous" }));
    await screen.findByText("Meme 1");

    expect(listCalls(calls).map((payload) => payload?.offset)).toEqual([0, 12, 0]);
  });

  it("switches between waiting and sent, and starts again from the first page", async () => {
    const { transport, calls } = routedTransport(
      {
        "meme.status": ok(MEME_STATUS),
        "meme.list": [
          ok(memePage([meme(1)], { total: 30 })),
          ok(memePage([meme(2, { title: "Old meme", sentAt: "2026-10-01T09:05:00" })], { total: 8 })),
        ],
      },
      previewer().preview,
    );
    renderCard(transport);

    await screen.findByText("Meme 1");
    expect(screen.getByRole("button", { name: "Waiting" }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Sent" }));

    await screen.findByText("Old meme");
    screen.getByText("sent 2026-10-01");
    expect(screen.getByRole("button", { name: "Sent" }).getAttribute("aria-pressed")).toBe("true");
    expect(listCalls(calls)).toEqual([
      { scope: "unsent", limit: 12, offset: 0 },
      { scope: "sent", limit: 12, offset: 0 },
    ]);
  });

  it("says when a view is empty", async () => {
    const { transport } = routedTransport({ "meme.status": ok({ ...MEME_STATUS, sent: 0 }), "meme.list": ok(memePage([])) });
    renderCard(transport);

    await screen.findByText("No memes in this view.");
  });

  it("says a switched-off pool is switched off and asks for no list", async () => {
    const { transport, calls } = routedTransport({ "meme.status": ok(OFF) });
    renderCard(transport);

    await screen.findByText("Switched off.");
    expect(calls.map((call) => call.type)).toEqual(["meme.status"]);
  });

  it("keeps the counts when only the list fails, and tries again on request", async () => {
    const { transport, calls } = routedTransport(
      {
        "meme.status": ok(MEME_STATUS),
        "meme.list": [failure("timeout"), ok(memePage([meme(1)]))],
      },
      previewer().preview,
    );
    renderCard(transport);

    expect((await screen.findByRole("alert")).textContent).toBe("The orchestrator took too long to answer.");
    screen.getByText("30 waiting, 8 sent");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));

    await screen.findByText("Meme 1");
    expect(listCalls(calls)).toHaveLength(2);
  });

  it("explains a failure of the counts", async () => {
    const { transport } = routedTransport({ "meme.status": failure("unavailable") });
    renderCard(transport);

    expect((await screen.findByRole("alert")).textContent).toBe("The orchestrator is not answering. Is it running?");
  });

  it("drops the page that arrives after another view was chosen", async () => {
    const slow = deferred<TransportResult>();
    const { transport } = routedTransport(
      {
        "meme.status": ok(MEME_STATUS),
        "meme.list": [ok(memePage([meme(1)], { total: 30 })), () => slow.promise, ok(memePage([meme(2, { title: "Sent one" })], { total: 8 }))],
      },
      previewer().preview,
    );
    renderCard(transport);
    await screen.findByText("Meme 1");

    fireEvent.click(screen.getByRole("button", { name: "Next" })); // a slow page 2 of waiting
    fireEvent.click(screen.getByRole("button", { name: "Sent" })); // then the sent view
    await screen.findByText("Sent one");

    await act(async () => {
      slow.resolve(ok(memePage([meme(13, { title: "Too late" })], { total: 30, offset: 12 })));
    });
    expect(screen.queryByText("Too late")).toBeNull();
    expect(titles()).toEqual(["Sent one"]);
  });

  it("shows a meme's text as text, never as markup", async () => {
    const hostile = '<img src="x" onerror="window.__owned = true">';
    const { transport } = routedTransport(
      { "meme.status": ok(MEME_STATUS), "meme.list": ok(memePage([meme(1, { title: hostile, tags: hostile, source: hostile })])) },
      previewer().preview,
    );
    const view = renderCard(transport);

    await screen.findAllByText(hostile, { exact: false });
    expect([...view.container.querySelectorAll("img")].every((image) => image.closest(".preview"))).toBe(true);
    expect((window as unknown as { __owned?: boolean }).__owned).toBeUndefined();
  });

  it("speaks Portuguese when asked", async () => {
    const { transport } = routedTransport(
      { "meme.status": ok(MEME_STATUS), "meme.list": ok(memePage([meme(1, { title: "" }), meme(2, { url: "https://images.example/a.mp4" })], { total: 30 })) },
      previewer().preview,
    );
    renderCard(transport, "pt");

    await screen.findByText("30 na fila, 8 enviados");
    screen.getByRole("heading", { name: "Fila de memes" });
    screen.getByText("sem título");
    screen.getByText("vídeo, sem prévia");
    screen.getByText("1 a 2 de 30");
    within(screen.getByRole("group")).getByRole("button", { name: "Na fila" });
  });
});
