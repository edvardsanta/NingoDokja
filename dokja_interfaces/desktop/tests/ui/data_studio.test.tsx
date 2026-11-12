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

const sqliteRows = ok({ result: { columns: ["id", "name"], rows: [[1, "Ada"]], total: 1 } });
const postgresRows = ok({ result: { columns: ["merchant"], rows: [["loja-a"]], total: 1 } });

const show = (transport: Parameters<typeof DataStudioCard>[0]["transport"]) =>
  render(<I18nProvider locale="en"><DataStudioCard transport={transport} /></I18nProvider>);

describe("the data studio", () => {
  it("opens a table on its rows, with structure one tab away and its SQL pre-filled", async () => {
    const { transport, calls } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.sqlite.schema": schema,
      "datastudio.sqlite.table": table,
      "datastudio.sqlite.rows": sqliteRows,
    });
    show(transport);

    await expectChips();
    fireEvent.click(screen.getByRole("button", { name: /^ningo\.db/ }));

    await screen.findByText("Objects (3)");
    fireEvent.click(screen.getByRole("button", { name: "table: chat_profiles" }));

    // the table opens on its rows, no SQL written
    await screen.findByText('"Ada"');
    expect(calls.map((c) => [c.type, c.payload])).toEqual([
      ["datastudio.sources.list", {}],
      ["datastudio.sqlite.schema", { path: "/data/ningo.db" }],
      ["datastudio.sqlite.table", { path: "/data/ningo.db", table: "chat_profiles" }],
      ["datastudio.sqlite.rows", { path: "/data/ningo.db", table: "chat_profiles", limit: 100, offset: 0 }],
    ]);

    // structure holds the columns
    fireEvent.click(screen.getByRole("tab", { name: /Structure \(2\)/ }));
    await screen.findByText("INTEGER");

    // the query box starts with a SELECT for this table
    expect(screen.getByRole("textbox")).toHaveProperty("value", 'SELECT * FROM "chat_profiles"');
  });

  it("running the pre-filled query replaces the rows; back to table restores them", async () => {
    const { transport, calls } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.sqlite.schema": schema,
      "datastudio.sqlite.table": table,
      "datastudio.sqlite.rows": sqliteRows,
      "datastudio.sqlite.query": query,
    });
    show(transport);

    fireEvent.click(await screen.findByRole("button", { name: /^ningo\.db/ }));
    await screen.findByText("Objects (3)");
    fireEvent.click(screen.getByRole("button", { name: "table: chat_profiles" }));
    await screen.findByText('"Ada"');

    // one result area: running the query swaps the paged rows for the query rows
    expect(screen.getByText("Page 1 of 1")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Run" }));
    await screen.findByText('"Bob"');
    expect(screen.queryByText("Page 1 of 1")).toBeNull();
    expect(calls.filter((c) => c.type === "datastudio.sqlite.query")).toEqual([
      { type: "datastudio.sqlite.query", payload: { path: "/data/ningo.db", query: 'SELECT * FROM "chat_profiles"', limit: 100, offset: 0 }, options: undefined },
    ]);

    fireEvent.click(screen.getByRole("button", { name: "Back to table" }));
    await screen.findByText("Page 1 of 1");
    expect(screen.queryByText('"Bob"')).toBeNull();
  });

  it("loads a JSON config and a persona file as soon as they are picked", async () => {
    const { transport, calls } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.json.read": ok({ content: { ningo: { profile: "arrogant" } } }),
      "datastudio.markdown.read": ok({ content: "# warm context" }),
    });
    show(transport);

    fireEvent.click(await screen.findByRole("button", { name: /^owner\.json/ }));
    await screen.findByText(/"profile": "arrogant"/);

    fireEvent.click(screen.getByRole("button", { name: /^warm\.md/ }));
    await screen.findByText("# warm context");

    expect(calls.map((c) => c.type)).toEqual([
      "datastudio.sources.list",
      "datastudio.json.read",
      "datastudio.markdown.read",
    ]);
  });

  it("pages through table rows with previous and next", async () => {
    const big: unknown[] = Array.from({ length: 100 }, (_, i) => [i + 1, `row-${i + 1}`]);
    const { transport, calls } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.sqlite.schema": schema,
      "datastudio.sqlite.table": ok({ schema: { columns: [
        { name: "id", type: "INTEGER", not_null: true, primary_key: true },
        { name: "name", type: "TEXT", not_null: false, primary_key: false },
      ], count: 150 } }),
      "datastudio.sqlite.rows": [
        ok({ result: { columns: ["id", "name"], rows: big, total: 100 } }),
        ok({ result: { columns: ["id", "name"], rows: [[101, "row-101"]], total: 1 } }),
      ],
    });
    show(transport);

    fireEvent.click(await screen.findByRole("button", { name: /^ningo\.db/ }));
    await screen.findByText("Objects (3)");
    fireEvent.click(screen.getByRole("button", { name: "table: chat_profiles" }));

    await screen.findByText('"row-1"');
    expect(screen.getByText("Page 1 of 2")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Previous" })).toHaveProperty("disabled", true);

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await screen.findByText('"row-101"');
    expect(screen.getByRole("button", { name: "Next" })).toHaveProperty("disabled", true);
    expect(calls.filter((c) => c.type === "datastudio.sqlite.rows").map((c) => c.payload?.offset)).toEqual([0, 100]);
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

  it("connects to PostgreSQL on demand, schemas then tables opening on rows, never showing the DSN", async () => {
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
      "datastudio.postgres.rows": postgresRows,
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
    await screen.findByText('"loja-a"');

    expect(calls.map((c) => [c.type, c.payload])).toEqual([
      ["datastudio.sources.list", {}],
      ["datastudio.postgres.schemas", { connection_id: "postgres:1" }],
      ["datastudio.postgres.tables", { connection_id: "postgres:1", schema: "dokja_price" }],
      ["datastudio.postgres.table", { connection_id: "postgres:1", schema: "dokja_price", table: "observations" }],
      ["datastudio.postgres.rows", { connection_id: "postgres:1", schema: "dokja_price", table: "observations", limit: 100, offset: 0 }],
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

  it("runs a PostgreSQL query from the table and replaces its rows", async () => {
    const { transport, calls } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.postgres.schemas": ok({ schemas: ["dokja_price"] }),
      "datastudio.postgres.tables": ok({ tables: [{ name: "offers", type: "table" }] }),
      "datastudio.postgres.table": ok({ schema: { columns: [{ name: "merchant", type: "text", not_null: true, primary_key: false }], count: 9 } }),
      "datastudio.postgres.rows": postgresRows,
      "datastudio.postgres.query": ok({ result: { columns: ["merchant", "cents"], rows: [["loja-b", 1990]], total: 1 } }),
    });
    show(transport);

    fireEvent.click(await screen.findByRole("button", { name: "price_history" }));
    fireEvent.click(await screen.findByRole("button", { name: "Connect" }));
    await screen.findByText("Schemas (1)");
    fireEvent.click(screen.getByRole("button", { name: "dokja_price" }));
    fireEvent.click(await screen.findByRole("button", { name: "table: offers" }));
    await screen.findByText('"loja-a"');

    const box = screen.getByRole("textbox");
    fireEvent.change(box, { target: { value: "SELECT merchant, cents FROM dokja_price.offers WHERE cents < 3000" } });
    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    await screen.findByText('"loja-b"');
    expect(screen.queryByText('"loja-a"')).toBeNull();
    expect(calls.filter((c) => c.type === "datastudio.postgres.query")).toEqual([
      { type: "datastudio.postgres.query", payload: { connection_id: "postgres:1", query: "SELECT merchant, cents FROM dokja_price.offers WHERE cents < 3000", limit: 100, offset: 0 }, options: undefined },
    ]);
  });

  it("announces a PostgreSQL query error without dropping the table rows", async () => {
    const { transport } = routedTransport({
      "datastudio.sources.list": sources,
      "datastudio.postgres.schemas": ok({ schemas: ["dokja_price"] }),
      "datastudio.postgres.tables": ok({ tables: [{ name: "offers", type: "table" }] }),
      "datastudio.postgres.table": ok({ schema: { columns: [{ name: "merchant", type: "text", not_null: true, primary_key: false }], count: 9 } }),
      "datastudio.postgres.rows": postgresRows,
      "datastudio.postgres.query": failure("orchestrator", "only SELECT statements are allowed"),
    });
    show(transport);

    fireEvent.click(await screen.findByRole("button", { name: "price_history" }));
    fireEvent.click(await screen.findByRole("button", { name: "Connect" }));
    await screen.findByText("Schemas (1)");
    fireEvent.click(screen.getByRole("button", { name: "dokja_price" }));
    fireEvent.click(await screen.findByRole("button", { name: "table: offers" }));
    await screen.findByText('"loja-a"');

    const box = screen.getByRole("textbox");
    fireEvent.change(box, { target: { value: "DELETE FROM dokja_price.offers" } });
    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    await screen.findByRole("alert");
    expect(screen.getByText(/only SELECT statements/)).toBeTruthy();
    expect(screen.getByText('"loja-a"')).toBeTruthy();
  });

  it("reads nothing before the operator asks, and only reads", async () => {
    const { transport, calls } = routedTransport({ "datastudio.sources.list": sources });
    show(transport);
    await expectChips();
    await waitFor(() => expect(calls).toHaveLength(1));
  });
});
