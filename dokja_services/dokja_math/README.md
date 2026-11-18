# dokja-math

Bounded LaTeX math rendering for chat formulas. The orchestrator's built-in
renderer (`go-latex/mtex`) covers only flat expressions; this service renders
the real subset physics and statistics ask for — subscripts, superscripts,
fractions, accents — with `matplotlib.mathtext`, and answers PNG bytes on
`POST /math/render` (`{expression}` → `{data_b64, content_type}`).

Expressions arrive without `$...$` delimiters; the service wraps them.
Everything is bounded like the built-in renderer: 1024 characters in, one PNG
of at most 256 KiB and 2200×1000 px out, non-math content (URLs, `$ % # &`,
newlines) refused with 422. Renders sit on a white card so black symbols read
on Discord's dark themes.

The orchestrator uses this service when `DOKJA_MATH_ENDPOINT` points at it and
falls back to the built-in renderer when the endpoint is absent or failing;
without the service the stack behaves exactly as before.

## Run

```
pip install -r requirements.txt
python main.py            # --reload for development
```

`GET /status` answers `{"status": "ok"}`; `POST /math/render` takes
`{"expression": "\\rho_\\beta = e^{-\\beta H}"}` and returns the PNG as
base64.

## Test

```
pip install pytest httpx
python -m pytest tests/ -v
```
