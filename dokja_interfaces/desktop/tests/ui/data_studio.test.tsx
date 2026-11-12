import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { DataStudioCard } from "../../src/renderer/cards/data_studio.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { failure, ok, routedTransport } from "./support.js";

const sources = ok({
  sources: [
    { id: "sqlite:/data/ningo.db", kind: "sqlite", name: "ningo.db", path: "/data/ningo.db", size: 1024 },
    { id: "postgres:1", kind: "postgres", name: "price_history", dsn: "postgres://user:secret@db.internal:5432/prices", database: "prices", size: 0 },
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

const expectChips = async () => {
  await screen.findByRole("listitem", { name: /SQLite/ });
  expect(screen.getByRole("listitem", { name: /price_history|PostgreSQL/ })).toBeTruthy();
  expect(screen.getByRole("listitem", { name: /JSON/ })).toBeTruthy();
  expect(screen.getByRole("listitem", { name: /Markdown/ })).toBeTruthy();
  expect(screen.getByRole("listitem", { name: /persona/i })).toBeTruthy();
};

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

    await expectChips();
    fireEvent.click(screen.getByRole("button", { name: /^ningo\.db/ }));

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

    fireEvent.click(await screen.findByRole("button", { name: /^ningo\.db/ }));
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

    fireEvent.click(await screen.findByRole("button", { name: /^owner\.json/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Load" }));
    await screen.findByText(/"profile": "arrogant"/);

    fireEvent.click(screen.getByRole("button", { name: /^warm\.md/ }));
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
    await expectChips();
    expect(calls.filter((c) => c.type === "datastudio.sources.list")).toHaveLength(2);
  });

  it("connects to PostgreSQL on demand, schemas then tables then columns, never showing the DSN", async () => {
    const { transport, calls } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.postgres.schemas": ok({ schemas: ["public", "dokja_price"] }),
      "datastudio.postgres.tables": ok({ tables: [
        { name: "observations", type: "table" },
        { name: "daily_lows", type: "view" },
      ] }),
      "datastudio.postgres.table": ok({ schema: { columns: [
        { name: "id", type: "bigint", not_null: true, primary_key: true },
        { name: "cents", type: "integer", not_null: true, primary_key: false },
      ], count: 42 } }),
    });
    show(transport);

    fireEvent.click(await screen.findByRole("button", { name: "price_history" }));
    fireEvent.click(await screen.findByRole("button", { name: "Connect" }));

    await screen.findByText("Schemas (2)");
    // the credentials of the DSN never reach the screen
    expect(screen.queryByText(/secret/)).toBeNull();
    screen.getByText("db.internal:5432/prices");

    fireEvent.click(screen.getByRole("button", { name: "dokja_price" }));
    await screen.findByText("table: observations");
    fireEvent.click(screen.getByRole("button", { name: "table: observations" }));
    await screen.findByText("Count: 42");

    expect(calls.map((c) => [c.type, c.payload])).toEqual([
      ["datastudio.sources.list", {}],
      ["datastudio.postgres.schemas", { id: "postgres:1" }],
      ["datastudio.postgres.tables", { id: "postgres:1", schema: "dokja_price" }],
      ["datastudio.postgres.table", { id: "postgres:1", schema: "dokja_price", table: "observations" }],
    ]);
  });

  it("says plainly when a PostgreSQL connection cannot be reached", async () => {
    const { transport } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.postgres.schemas": failure("timeout", "connection refused"),
    });
    show(transport);

    fireEvent.click(await screen.findByRole("button", { name: "price_history" }));
    fireEvent.click(await screen.findByRole("button", { name: "Connect" }));
    await screen.findByRole("alert");
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
  });

  it("reads nothing before the operator asks, and only reads", async () => {
    const { transport, calls } = routedTransport({ "datastudio.sources.list": sources });
    show(transport);
    await expectChips();
    await waitFor(() => expect(calls).toHaveLength(1));
  });
});
