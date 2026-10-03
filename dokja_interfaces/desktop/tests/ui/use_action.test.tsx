import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useAction } from "../../src/renderer/cards/use_action.js";
import type { LoadResult } from "../../src/renderer/cards/use_card.js";
import { deferred } from "./support.js";

const answer = (value: string): LoadResult<string> => ({ ok: true, data: value });

describe("useAction", () => {
  it("starts idle, loads, then holds the answer", async () => {
    const gate = deferred<LoadResult<string>>();
    const { result } = renderHook(() => useAction((_: string) => gate.promise));

    expect(result.current.state).toEqual({ phase: "idle" });
    act(() => result.current.start("q"));
    expect(result.current.state).toEqual({ phase: "loading" });

    await act(async () => gate.resolve(answer("found")));
    expect(result.current.state).toEqual({ phase: "ready", data: "found" });
  });

  it("drops an answer that arrives after a newer request was started", async () => {
    const first = deferred<LoadResult<string>>();
    const second = deferred<LoadResult<string>>();
    const gates = [first, second];
    let turn = 0;
    const { result } = renderHook(() => useAction((_: string) => gates[turn++]!.promise));

    act(() => result.current.start("first"));
    act(() => result.current.start("second"));
    await act(async () => second.resolve(answer("second answer")));
    await act(async () => first.resolve(answer("first answer, too late")));

    expect(result.current.state).toEqual({ phase: "ready", data: "second answer" });
  });

  it("reports a failed request as an error and a rejected one as unavailable", async () => {
    const failed: LoadResult<string> = { ok: false, error: { code: "timeout", message: "slow" } };
    // created when asked for: a rejection made early would be reported as unhandled
    const outcomes: Array<() => Promise<LoadResult<string>>> = [
      () => Promise.resolve(failed),
      () => Promise.reject(new Error("boom")),
    ];
    let turn = 0;
    const { result } = renderHook(() => useAction((_: string) => outcomes[turn++]!()));

    await act(async () => result.current.start("a"));
    expect(result.current.state).toEqual({ phase: "error", error: { code: "timeout", message: "slow" } });

    await act(async () => result.current.start("b"));
    expect(result.current.state).toEqual({ phase: "error", error: { code: "unavailable", message: "request failed" } });
  });

  it("drops an answer that arrives after the card went away", async () => {
    const gate = deferred<LoadResult<string>>();
    const { result, unmount } = renderHook(() => useAction((_: string) => gate.promise));

    act(() => result.current.start("q"));
    const before = result.current.state;
    unmount();
    await act(async () => gate.resolve(answer("late")));

    expect(before).toEqual({ phase: "loading" });
    expect(result.current.state).toEqual({ phase: "loading" });
  });
});
