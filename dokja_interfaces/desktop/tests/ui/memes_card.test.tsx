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

  it("previews each picture and each clip through the shell", async () => {
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
            meme(5, { url: "https://images.example/broken.mp4" }),
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

    // in the grid a clip plays by itself, silent, in a loop; the controls are in the viewer
    const clips = view.container.querySelectorAll(".preview video");
    expect(clips).toHaveLength(1);
    const clip = clips[0] as HTMLVideoElement;
    expect(clip.getAttribute("src")).toBe(`data:video/mp4;base64,${btoa("https://images.example/clip.mp4")}`);
    expect(clip.getAttribute("aria-label")).toBe("Meme 2");
    expect([clip.autoplay, clip.muted, clip.loop, clip.controls]).toEqual([true, true, true, false]);

    // a failed picture, a failed clip and a meme with no address each say so; only the first two are asked for
    await waitFor(() => expect(screen.getAllByText("no preview")).toHaveLength(3));
    expect(asked.sort()).toEqual([
      "https://images.example/1.png",
      "https://images.example/broken.mp4",
      "https://images.example/broken.png",
      "https://images.example/clip.mp4",
    ]);
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

  describe("opening a meme", () => {
    const threeMemes = () => {
      const { asked, preview } = previewer(["broken"]);
      const route = routedTransport(
        {
          "meme.status": ok(MEME_STATUS),
          "meme.list": ok(
            memePage(
              [
                meme(1, { title: "First", tags: "cat" }),
                meme(2, { title: "Second", url: "https://images.example/clip.mp4" }),
                meme(3, { title: "Third" }),
                meme(4, { title: "Broken", url: "https://images.example/broken.png" }),
              ],
              { total: 4 },
            ),
          ),
        },
        preview,
      );
      return { ...route, asked };
    };

    const tile = (name: string) => screen.findByRole("button", { name });
    const dialog = () => screen.getByRole("dialog");

    it("makes a tile a button naming its meme, and a meme with no preview has none", async () => {
      const { transport } = threeMemes();
      renderCard(transport);

      await tile("Open First");
      await tile("Open Second");
      await screen.findByText("no preview");
      expect(screen.queryByRole("button", { name: "Open Broken" })).toBeNull();
    });

    it("opens a picture large, with its details", async () => {
      const { transport } = threeMemes();
      const view = renderCard(transport);

      fireEvent.click(await tile("Open First"));

      expect(dialog().getAttribute("aria-modal")).toBe("true");
      expect(dialog().getAttribute("aria-label")).toBe("First");
      expect(within(dialog()).getByText("1 of 4")).toBeTruthy();
      expect(within(dialog()).getByText(/cat/)).toBeTruthy();
      expect(view.container.querySelectorAll(".viewer img")).toHaveLength(1);
    });

    it("opens a clip with its controls and its sound", async () => {
      const { transport } = threeMemes();
      const view = renderCard(transport);

      fireEvent.click(await tile("Open Second"));

      const clip = view.container.querySelector(".viewer video") as HTMLVideoElement;
      expect(clip.getAttribute("src")).toBe(`data:video/mp4;base64,${btoa("https://images.example/clip.mp4")}`);
      expect([clip.autoplay, clip.loop, clip.controls, clip.muted]).toEqual([true, true, true, false]);
    });

    it("opens without asking the shell again", async () => {
      const { transport, asked } = threeMemes();
      renderCard(transport);
      await tile("Open First");
      const before = asked.length;

      fireEvent.click(screen.getByRole("button", { name: "Open First" }));
      fireEvent.keyDown(document.body, { key: "ArrowRight" });

      expect(asked.length).toBe(before);
    });

    it("moves between the memes with the arrows and the buttons, and stops at the ends", async () => {
      const { transport } = threeMemes();
      renderCard(transport);
      fireEvent.click(await tile("Open First"));

      fireEvent.keyDown(document.body, { key: "ArrowLeft" });
      expect(within(dialog()).getByText("1 of 4")).toBeTruthy();
      expect((within(dialog()).getByRole("button", { name: "Previous meme" }) as HTMLButtonElement).disabled).toBe(true);

      fireEvent.keyDown(document.body, { key: "ArrowRight" });
      expect(within(dialog()).getByText("2 of 4")).toBeTruthy();
      expect(dialog().getAttribute("aria-label")).toBe("Second");
      fireEvent.click(within(dialog()).getByRole("button", { name: "Next meme" }));
      expect(within(dialog()).getByText("3 of 4")).toBeTruthy();
      fireEvent.click(within(dialog()).getByRole("button", { name: "Next meme" }));
      expect(within(dialog()).getByText("4 of 4")).toBeTruthy();
      await within(dialog()).findByText("no preview");
      expect((within(dialog()).getByRole("button", { name: "Next meme" }) as HTMLButtonElement).disabled).toBe(true);
      fireEvent.keyDown(document.body, { key: "ArrowRight" });
      expect(within(dialog()).getByText("4 of 4")).toBeTruthy();
      fireEvent.click(within(dialog()).getByRole("button", { name: "Previous meme" }));
      expect(within(dialog()).getByText("3 of 4")).toBeTruthy();
    });

    it("closes with Escape, the button or a click outside, and gives the focus back", async () => {
      const { transport } = threeMemes();
      renderCard(transport);

      const first = await tile("Open First");
      fireEvent.click(first);
      expect(document.activeElement).toBe(within(dialog()).getByRole("button", { name: "Close" }));
      fireEvent.keyDown(document.body, { key: "Escape" });
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(document.activeElement).toBe(first);

      fireEvent.click(first);
      fireEvent.click(within(dialog()).getByRole("button", { name: "Close" }));
      expect(screen.queryByRole("dialog")).toBeNull();

      fireEvent.click(first);
      fireEvent.click(dialog().parentElement as HTMLElement);
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    it("stays open when the click is inside it", async () => {
      const { transport } = threeMemes();
      renderCard(transport);
      fireEvent.click(await tile("Open First"));

      fireEvent.click(dialog());
      fireEvent.click(within(dialog()).getByText("First"));

      expect(screen.getByRole("dialog")).toBeTruthy();
    });

    it("speaks Portuguese when asked", async () => {
      const { transport } = threeMemes();
      renderCard(transport, "pt");

      fireEvent.click(await tile("Abrir First"));

      within(dialog()).getByText("1 de 4");
      within(dialog()).getByRole("button", { name: "Fechar" });
      within(dialog()).getByRole("button", { name: "Meme anterior" });
      within(dialog()).getByRole("button", { name: "Próximo meme" });
    });
  });

  it("speaks Portuguese when asked", async () => {
    const { transport } = routedTransport(
      {
        "meme.status": ok(MEME_STATUS),
        "meme.list": ok(
          memePage(
            [meme(1, { title: "" }), meme(2, { url: "https://images.example/a.mp4" }), meme(3, { url: "https://images.example/broken.png" })],
            { total: 30 },
          ),
        ),
      },
      previewer(["broken"]).preview,
    );
    const view = renderCard(transport, "pt");

    await screen.findByText("30 na fila, 8 enviados");
    screen.getByRole("heading", { name: "Fila de memes" });
    await screen.findByText("sem título");
    await screen.findByText("sem prévia");
    expect(view.container.querySelector(".preview video")?.getAttribute("aria-label")).toBe("Meme 2");
    screen.getByText("1 a 3 de 30");
    within(screen.getByRole("group")).getByRole("button", { name: "Na fila" });
  });
});
