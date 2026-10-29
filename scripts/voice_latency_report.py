#!/usr/bin/env python3
"""Summarize sanitized Discord voice timing logs from stdin or a log file."""
import argparse
import json
import math
import re
import sys


def summarize(lines):
    traces = {}
    malformed = 0
    for line in lines:
        marker = "[dokja-discord] voice timing "
        if marker not in line:
            continue
        try:
            payload = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", line.split(marker, 1)[1])
            event = json.loads(payload)
            trace, stage, elapsed = event["traceId"], event["stage"], event["elapsedMs"]
            sentence = event.get("sentenceId", "")
            if (not isinstance(trace, str) or not isinstance(stage, str)
                    or not isinstance(sentence, str)
                    or isinstance(elapsed, bool) or not isinstance(elapsed, (int, float))
                    or not math.isfinite(elapsed) or elapsed < 0):
                raise ValueError("invalid timing")
            stages = traces.setdefault(trace, {})
            # Duplicate log delivery must not inflate sample counts.
            key = (stage, sentence)
            stages[key] = min(stages.get(key, elapsed), elapsed)
        except (ValueError, KeyError, TypeError):
            malformed += 1
    pairs = {
        "recognition_queue_ms": ("recognition_queued", "recognition_started"),
        "recognition_ms": ("recognition_started", "transcript_ready"),
        "dispatch_wait_ms": ("transcript_ready", "response_dispatched"),
        "tts_ms": ("tts_requested", "tts_ready"),
        "player_start_ms": ("playback_enqueued", "playback_started"),
        "playback_ms": ("playback_started", "playback_finished"),
    }
    samples = {name: [] for name in pairs}
    samples["capture_to_first_player_start_ms"] = []
    failures = {}
    with_playback = 0
    for stages in traces.values():
        for (stage, _), elapsed in stages.items():
            if stage.endswith(("failed", "cancelled", "overflow")):
                failures[stage] = failures.get(stage, 0) + 1
        for name, (start, end) in pairs.items():
            for (stage, sentence), elapsed in stages.items():
                if stage != start:
                    continue
                stop = stages.get((end, sentence))
                if stop is not None and stop >= elapsed:
                    samples[name].append(stop - elapsed)
        started = [t for (stage, _), t in stages.items() if stage == "playback_started"]
        capture = stages.get(("capture_started", ""))
        if started:
            with_playback += 1
            if capture is not None and min(started) >= capture:
                samples["capture_to_first_player_start_ms"].append(min(started) - capture)

    def stats(values):
        ordered = sorted(values)
        return {"samples": len(values), **{
            label: round(ordered[max(0, math.ceil(q * len(ordered)) - 1)], 2)
            if ordered else None
            for label, q in (("p50", .5), ("p95", .95), ("p99", .99))}}

    return {"traces": len(traces), "traces_with_playback": with_playback,
            "traces_without_playback": len(traces) - with_playback,
            "malformed_events": malformed, "failures": failures,
            "metrics": {name: stats(values) for name, values in samples.items()},
            "limitations": ["Capture-to-player includes human utterance duration; it is not end-of-speech latency.",
                            "Player Playing state does not establish remote audibility.",
                            "Traces without playback include passive, batched, unfinished and failed segments."]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("log", nargs="?", help="Log file; omit to read stdin")
    args = parser.parse_args()
    if args.log:
        with open(args.log, encoding="utf-8") as stream:
            result = summarize(stream)
    else:
        result = summarize(sys.stdin)
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
