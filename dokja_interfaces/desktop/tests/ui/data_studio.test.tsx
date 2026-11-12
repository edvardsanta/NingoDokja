import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { DataStudioCard } from "../../src/renderer/cards/data_studio.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { failure, ok, routedTransport } from "./support.js";

const sources = ok({
  sources: [
    { id: "sqlite:/data/ningo.db", kind: "sqlite", name: "ningo.db", path: "/data/ningo.db", size: 1024 },
    { id: "json:/data/owner.json", kind: "json", name: "owner.json", path: "/data/owner.json", size: 100 },
    { id: "markdown:/data/AGENTS.md", kind: "markdown", name: "AGENTS.md", path: "/data/AGENTS.md", size: 200 },
    { id: "persona:/data/warm.md", kind: "persona", name: "warm.md", path: "/data/warm.md", size: 300 },
  ],
});

const schema = ok({ schema: { objects: [
  { name: "chat_profiles", type: "table" },
  { name: "settings", type: "table" },
  { name: "idx_name", type: "index" },
] } });

const table = ok({ schema: { columns: [
  { name: "id", type: "INTEGER", not_null: true, primary_key: true },
  { name: "name", type: "TEXT", not_null: false, primary_key: false },
], count: 2 } });

const query = ok({ result: { columns: ["id", "name"], rows: [[1, "Ada"], [2, "Bob"]], total: 2 } });

const show = (transport: Parameters<typeof DataStudioCard>[0]["transport"]) =>
  render(<I18nProvider locale="en"><DataStudioCard transport={transport} /></I18nProvider>);

describe("the data studio", () => {
  it("lists the sources by kind as it opens and reads a database on selection", async () => {
    const { transport, calls } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.sqlite.schema": schema,
      "datastudio.sqlite.table": table,
    });
    show(transport);

    await screen.findByText("1 SQLite, 0 PostgreSQL, 1 JSON, 1 Markdown, 1 persona");
    fireEvent.click(screen.getByRole("button", { name: "ningo.db" }));

    await screen.findByText("Objects (3)");
    fireEvent.click(screen.getByRole("button", { name: "table: chat_profiles" }));

    await screen.findByText("Count: 2");
    expect(calls.map((c) => [c.type, c.payload])).toEqual([
      ["datastudio.sources.list", {}],
      ["datastudio.sqlite.schema", { path: "/data/ningo.db" }],
      ["datastudio.sqlite.table", { path: "/data/ningo.db", table: "chat_profiles" }],
    ]);
  });

  it("runs the typed query and shows the rows it gets back", async () => {
    const { transport, calls } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.sqlite.schema": schema,
      "datastudio.sqlite.query": query,
    });
    show(transport);

    fireEvent.click(await screen.findByRole("button", { name: "ningo.db" }));
    fireEvent.click(await screen.findByRole("button", { name: "Run" }));

    await screen.findByText('"Ada"');
    expect(screen.getByText('"Bob"')).toBeTruthy();
    expect(calls.filter((c) => c.type === "datastudio.sqlite.query")).toEqual([
      { type: "datastudio.sqlite.query", payload: { path: "/data/ningo.db", query: "SELECT * FROM ", limit: 100, offset: 0 }, options: undefined },
    ]);
  });

  it("loads a JSON config and a persona file on demand", async () => {
    const { transport, calls } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.json.read": ok({ content: { ningo: { profile: "arrogant" } } }),
      "datastudio.markdown.read": ok({ content: "# warm context" }),
    });
    show(transport);

    fireEvent.click(await screen.findByRole("button", { name: "owner.json" }));
    fireEvent.click(await screen.findByRole("button", { name: "Load" }));
    await screen.findByText(/"profile": "arrogant"/);

    fireEvent.click(screen.getByRole("button", { name: "warm.md" }));
    fireEvent.click(screen.getByRole("button", { name: "Load" }));
    await screen.findByText("# warm context");

    expect(calls.map((c) => c.type)).toEqual([
      "datastudio.sources.list",
      "datastudio.json.read",
      "datastudio.markdown.read",
    ]);
  });

  it("reports the issues the diagnostics run finds", async () => {
    const { transport } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.diagnostics.run": ok({ sources: 4, issues: [{ kind: "json", name: "owner.json", error: "invalid json" }] }),
    });
    show(transport);

    fireEvent.click(await screen.findByRole("button", { name: "Diagnostics" }));
    await screen.findByText("Count: 4");
    expect(screen.getAllByText(/owner.json/).length).toBeGreaterThan(0);
  });

  it("says so plainly when the diagnostics run finds no issue", async () => {
    const { transport } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.diagnostics.run": ok({ sources: 4, issues: [] }),
    });
    show(transport);

    fireEvent.click(await screen.findByRole("button", { name: "Diagnostics" }));
    await screen.findByText("No issues found.");
  });

  it("offers a retry when the sources cannot be read", async () => {
    const { transport, calls } = routedTransport({
      "datastudio.sources.list": [failure("unavailable"), sources],
    });
    show(transport);

    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    await screen.findByText("1 SQLite, 0 PostgreSQL, 1 JSON, 1 Markdown, 1 persona");
    expect(calls.filter((c) => c.type === "datastudio.sources.list")).toHaveLength(2);
  });

  it("reads nothing before the operator asks, and only reads", async () => {
    const { transport, calls } = routedTransport({ "datastudio.sources.list": sources });
    show(transport);
    await screen.findByText("1 SQLite, 0 PostgreSQL, 1 JSON, 1 Markdown, 1 persona");
    await waitFor(() => expect(calls).toHaveLength(1));
  });
});
