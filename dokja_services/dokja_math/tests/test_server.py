import base64

import pytest
from fastapi.testclient import TestClient

from math_render import MathRenderError, png_size, render_math
from server import create_app


@pytest.fixture()
def client():
    return TestClient(create_app())


def test_status_answers_ok(client):
    response = client.get("/status")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_renders_subscripts_superscripts_and_text():
    # The constructs the built-in Go renderer cannot do: ^, _, \frac, \mathrm, \quad, accents.
    data = render_math(r"\rho_\beta = \frac{e^{-\beta \hat{H}}}{Z}, \quad Z = \mathrm{Tr}(e^{-\beta \hat{H}})")
    assert data[:8] == b"\x89PNG\r\n\x1a\n"
    width, height = png_size(data)
    assert 10 < width <= 2200
    assert 10 < height <= 1000


def test_white_card_background():
    data = render_math(r"e^{x}")
    assert data[:8] == b"\x89PNG\r\n\x1a\n"


def test_render_route_returns_base64_png(client):
    response = client.post("/math/render", json={"expression": r"k_B T"})
    assert response.status_code == 200
    body = response.json()
    assert body["content_type"] == "image/png"
    assert base64.b64decode(body["data_b64"])[:8] == b"\x89PNG\r\n\x1a\n"


def test_render_route_rejects_unrenderable_math(client):
    for expression in ["", "x" * 1025, r"\frac{1}{", "https://example.com/x", "a\nb"]:
        response = client.post("/math/render", json={"expression": expression})
        assert response.status_code == 422, expression


def test_render_route_rejects_unknown_content_type_shape(client):
    response = client.post("/math/render", json={"expression": 42})
    assert response.status_code == 422


def test_render_math_bounds():
    with pytest.raises(MathRenderError):
        render_math("")
    with pytest.raises(MathRenderError):
        render_math("x" * 1025)
    with pytest.raises(MathRenderError):
        render_math("https://example.com/page")
