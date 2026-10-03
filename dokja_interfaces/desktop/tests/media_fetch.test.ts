import test from "node:test";
import assert from "node:assert/strict";
import http, { type IncomingMessage, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";

import { MediaError, fetchMedia, isPublicAddress, sniffImage, sniffVideo } from "../src/main/media_fetch.js";

// A real HTTP server on this machine stands in for an image host. The shell refuses private
// addresses, so these tests lift the address check and name the port; the refusals are tested
// with the real check further down.
const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
  "base64",
);
const JPEG = Buffer.concat([Buffer.from([0xff, 0xd8, 0xff, 0xe0]), Buffer.alloc(64)]);
const GIF = Buffer.concat([Buffer.from("GIF89a"), Buffer.alloc(32)]);
const WEBP = Buffer.concat([Buffer.from("RIFF"), Buffer.alloc(4), Buffer.from("WEBPVP8 "), Buffer.alloc(16)]);
const MP4 = Buffer.concat([Buffer.from([0, 0, 0, 0x18]), Buffer.from("ftypmp42"), Buffer.alloc(40)]);
const WEBM = Buffer.concat([Buffer.from([0x1a, 0x45, 0xdf, 0xa3]), Buffer.alloc(40)]);
const AVIF_FILE = Buffer.concat([Buffer.alloc(4), Buffer.from("ftypavif"), Buffer.alloc(16)]);
const MIB = 1024 * 1024;

type Handler = (request: IncomingMessage, response: ServerResponse) => void;

async function host(handler: Handler) {
  const requests: IncomingMessage[] = [];
  const server = http.createServer((request, response) => {
    requests.push(request);
    handler(request, response);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;
  return {
    port,
    url: (path = "/a.png") => `http://127.0.0.1:${port}${path}`,
    requests,
    options: { guard: () => true, ports: [port] },
    close: () => new Promise<void>((resolve) => { server.closeAllConnections(); server.close(() => resolve()); }),
  };
}

function image(bytes: Buffer, type = "image/png"): Handler {
  return (_request, response) => {
    response.writeHead(200, { "content-type": type });
    response.end(bytes);
  };
}

async function refusedWith(promise: Promise<unknown>, code: string, message?: RegExp) {
  await assert.rejects(promise, (error: unknown) => {
    assert.ok(error instanceof MediaError, `expected an MediaError, got ${String(error)}`);
    assert.equal(error.code, code);
    if (message) assert.match(error.message, message);
    return true;
  });
}

test("a picture comes back as a data URL typed by what it is, with a plain GET", async () => {
  const server = await host(image(PNG));
  try {
    const dataUrl = await fetchMedia(server.url(), server.options);

    assert.equal(dataUrl, `data:image/png;base64,${PNG.toString("base64")}`);
    const [request] = server.requests;
    assert.equal(request?.method, "GET");
    assert.match(String(request?.headers["user-agent"]), /Mozilla/);
    assert.equal(request?.headers.referer, `http://127.0.0.1:${server.port}/`);
    assert.equal(request?.headers.cookie, undefined);
    assert.equal(request?.headers.authorization, undefined);
  } finally {
    await server.close();
  }
});

test("a JPEG, a GIF and a WebP are pictures too, whatever the header says about the subtype", async () => {
  for (const [bytes, expected] of [
    [JPEG, "image/jpeg"],
    [GIF, "image/gif"],
    [WEBP, "image/webp"],
  ] as const) {
    const server = await host(image(bytes, "image/jpg"));
    try {
      assert.match(await fetchMedia(server.url(), server.options), new RegExp(`^data:${expected};base64,`));
    } finally {
      await server.close();
    }
  }
});

test("a redirect is followed and checked again, up to three", async () => {
  const server = await host((request, response) => {
    const hop = Number(/\/hop(\d+)/.exec(request.url ?? "")?.[1] ?? "9");
    if (request.url === "/final.png") return image(PNG)(request, response);
    response.writeHead(302, { location: hop >= 2 ? "/final.png" : `/hop${hop + 1}` });
    response.end();
  });
  try {
    assert.match(await fetchMedia(server.url("/hop0"), server.options), /^data:image\/png/);
    assert.equal(server.requests.length, 4);
  } finally {
    await server.close();
  }
});

test("too many redirects, a loop included, are refused", async () => {
  const server = await host((_request, response) => {
    response.writeHead(302, { location: "/again" });
    response.end();
  });
  try {
    await refusedWith(fetchMedia(server.url("/start"), server.options), "unexpected", /redirects/);
    assert.equal(server.requests.length, 4, "the first request and three redirects, then it stops");
  } finally {
    await server.close();
  }
});

test("a redirect to another port or to a file is refused", async () => {
  const other = await host(image(PNG));
  const server = await host((_request, response) => {
    response.writeHead(302, { location: other.url() });
    response.end();
  });
  try {
    await refusedWith(fetchMedia(server.url(), server.options), "denied", /port/);
    assert.equal(other.requests.length, 0, "the second host never saw a request");
  } finally {
    await server.close();
    await other.close();
  }

  const toFile = await host((_request, response) => {
    response.writeHead(302, { location: "file:///etc/passwd" });
    response.end();
  });
  try {
    await refusedWith(fetchMedia(toFile.url(), toFile.options), "invalid");
  } finally {
    await toFile.close();
  }
});

test("what is not a picture is refused: a page, an SVG, a wrong type and a lie about the type", async () => {
  const cases: Array<[string, Buffer, string]> = [
    ["a page", Buffer.from("<html><body>hello</body></html>"), "text/html"],
    ["an SVG", Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"><script>1</script></svg>'), "image/svg+xml"],
    ["a script said to be a picture", Buffer.from("<script>alert(1)</script>"), "image/png"],
    ["no type at all", PNG, ""],
  ];
  for (const [name, bytes, type] of cases) {
    const server = await host((_request, response) => {
      response.writeHead(200, type ? { "content-type": type } : {});
      response.end(bytes);
    });
    try {
      await refusedWith(fetchMedia(server.url(), server.options), "unexpected", /picture/);
    } finally {
      await server.close();
    }
    assert.ok(name);
  }
});

test("an image that is too large is refused, announced or not", async () => {
  const announced = await host((_request, response) => {
    response.writeHead(200, { "content-type": "image/png", "content-length": "100000" });
    response.end(Buffer.concat([PNG, Buffer.alloc(99_900)]));
  });
  try {
    await refusedWith(fetchMedia(announced.url(), { ...announced.options, maxBytes: 5_000 }), "unexpected", /large/);
  } finally {
    await announced.close();
  }

  const streamed = await host((_request, response) => {
    response.writeHead(200, { "content-type": "image/png" }); // chunked, no length
    response.write(PNG);
    response.write(Buffer.alloc(8_000));
    response.end();
  });
  try {
    await refusedWith(fetchMedia(streamed.url(), { ...streamed.options, maxBytes: 5_000 }), "unexpected", /large/);
  } finally {
    await streamed.close();
  }
});

test("a host that is too slow times out, and one that errors is unavailable", async () => {
  const slow = await host(() => {
    // never answers
  });
  try {
    const started = Date.now();
    await refusedWith(fetchMedia(slow.url(), { ...slow.options, timeoutMs: 150 }), "timeout");
    assert.ok(Date.now() - started < 1_000);
  } finally {
    await slow.close();
  }

  const missing = await host((_request, response) => {
    response.writeHead(404);
    response.end();
  });
  try {
    await refusedWith(fetchMedia(missing.url(), missing.options), "unavailable", /404/);
  } finally {
    await missing.close();
  }
});

test("a private address is refused, and the host is never even asked", async () => {
  const server = await host(image(PNG));
  try {
    // no guard override: the real check runs, and 127.0.0.1 is not public
    await refusedWith(fetchMedia(server.url(), { ports: [server.port] }), "denied", /public/);
    await refusedWith(fetchMedia(`http://localhost:${server.port}/a.png`, { ports: [server.port] }), "denied");
    assert.equal(server.requests.length, 0);
  } finally {
    await server.close();
  }
});

test("an address written as numbers is checked too, in every way it can be written", async () => {
  const server = await host(image(PNG));
  try {
    // the real check, and a short time limit so a hole shows as a timeout, not as a long wait
    const forms = [
      `http://127.0.0.1:${server.port}/a.png`,
      `http://[::1]:${server.port}/a.png`,
      `http://[::ffff:127.0.0.1]:${server.port}/a.png`,
      `http://[::ffff:7f00:1]:${server.port}/a.png`,
      `http://2130706433:${server.port}/a.png`,
      `http://0x7f000001:${server.port}/a.png`,
      `http://0177.0.0.1:${server.port}/a.png`,
      `http://127.1:${server.port}/a.png`,
    ];
    for (const url of forms) {
      await refusedWith(fetchMedia(url, { ports: [server.port], timeoutMs: 500 }), "denied", /public/);
    }
    assert.equal(server.requests.length, 0, "no form of loopback reached the host");
  } finally {
    await server.close();
  }

  for (const url of [
    "http://169.254.169.254/latest/meta-data/",
    "http://10.0.0.1/",
    "http://192.168.0.1/admin",
    "http://172.16.5.4/",
    "http://[fe80::1]/",
    "http://[fd00::1]/",
    "https://100.64.0.1/",
  ]) {
    await refusedWith(fetchMedia(url, { timeoutMs: 500 }), "denied", /public/);
  }
});

test("a redirect to an address written as numbers is refused as well", async () => {
  const server = await host((_request, response) => {
    response.writeHead(302, { location: "http://169.254.169.254/latest/meta-data/" });
    response.end();
  });
  try {
    await refusedWith(fetchMedia(server.url(), { ...server.options, guard: (address) => address === "127.0.0.1", timeoutMs: 500 }), "denied");
    assert.equal(server.requests.length, 1);
  } finally {
    await server.close();
  }
});

test("addresses that are not plain http or https on a default port are refused before any network", async () => {
  const refused: Array<[string, string]> = [
    ["file:///etc/passwd", "invalid"],
    ["ftp://images.example/a.png", "invalid"],
    ["javascript:alert(1)", "invalid"],
    ["data:image/png;base64,AA==", "invalid"],
    ["not an address", "invalid"],
    ["", "invalid"],
    ["https://user:password@images.example/a.png", "denied"],
    ["https://images.example:8443/a.png", "denied"],
    ["http://images.example:22/a.png", "denied"],
  ];
  for (const [url, code] of refused) {
    await refusedWith(fetchMedia(url), code);
  }
});

test("only public addresses count as public, IPv4 wrapped in IPv6 included", () => {
  for (const address of ["93.184.216.34", "8.8.8.8", "1.1.1.1", "2606:4700::1111", "::ffff:93.184.216.34"]) {
    assert.equal(isPublicAddress(address), true, address);
  }
  for (const address of [
    "127.0.0.1",
    "127.1.2.3",
    "10.0.0.5",
    "172.16.0.1",
    "172.31.255.255",
    "192.168.1.1",
    "169.254.169.254",
    "100.64.0.1",
    "0.0.0.0",
    "224.0.0.1",
    "255.255.255.255",
    "198.18.0.1",
    "::1",
    "::",
    "fe80::1",
    "fc00::1",
    "fd12:3456:789a::1",
    "ff02::1",
    "::ffff:127.0.0.1",
    "::ffff:10.1.2.3",
    "::ffff:7f00:1",
    "64:ff9b::7f00:1",
    "2002:7f00:1::1",
    "2001:db8::1",
    "not an address",
    "",
    "93.184.216.34.5",
  ]) {
    assert.equal(isPublicAddress(address), false, address);
  }
});

test("only formats a browser shows as a picture are recognised", () => {
  assert.equal(sniffImage(PNG), "image/png");
  assert.equal(sniffImage(JPEG), "image/jpeg");
  assert.equal(sniffImage(GIF), "image/gif");
  assert.equal(sniffImage(WEBP), "image/webp");
  assert.equal(sniffImage(Buffer.concat([Buffer.alloc(4), Buffer.from("ftypavif"), Buffer.alloc(8)])), "image/avif");
  for (const bytes of ["<svg></svg>", "<html></html>", "%PDF-1.7", "MZ", ""]) {
    assert.equal(sniffImage(Buffer.from(bytes)), undefined, bytes);
  }
});

test("a video comes back as a data URL typed by its container", async () => {
  for (const [bytes, declared, expected] of [
    [MP4, "video/mp4", "video/mp4"],
    [WEBM, "video/webm", "video/webm"],
    [MP4, "video/quicktime", "video/mp4"],
  ] as const) {
    const server = await host(image(bytes, declared));
    try {
      assert.equal(await fetchMedia(server.url("/a.mp4"), server.options), `data:${expected};base64,${bytes.toString("base64")}`);
    } finally {
      await server.close();
    }
  }
});

test("the request starts with Host, the way a browser's does", async () => {
  const server = await host(image(PNG));
  try {
    await fetchMedia(server.url(), server.options);
    const raw = server.requests[0]?.rawHeaders ?? [];
    assert.deepEqual(raw.slice(0, 2), ["Host", `127.0.0.1:${server.port}`]);
    assert.deepEqual(raw.filter((_, index) => index % 2 === 0).slice(0, 4), ["Host", "User-Agent", "Accept", "Referer"]);
  } finally {
    await server.close();
  }
});

test("the request says it takes videos as well as pictures", async () => {
  const server = await host(image(MP4, "video/mp4"));
  try {
    await fetchMedia(server.url("/a.mp4"), server.options);
    assert.match(String(server.requests[0]?.headers.accept), /video\/mp4/);
  } finally {
    await server.close();
  }
});

test("a video may be larger than a picture may, up to its own cap", async () => {
  const clip = Buffer.concat([MP4, Buffer.alloc(5 * MIB)]);
  const bigPicture = Buffer.concat([JPEG, Buffer.alloc(5 * MIB)]);
  const server = await host((request, response) => {
    const body = request.url?.includes("clip") ? clip : bigPicture;
    response.writeHead(200, { "content-type": body === clip ? "video/mp4" : "image/jpeg", "content-length": body.length });
    response.end(body);
  });
  try {
    assert.match(await fetchMedia(server.url("/clip"), server.options), /^data:video\/mp4;base64,/);
    await refusedWith(fetchMedia(server.url("/picture"), server.options), "unexpected", /large/);
  } finally {
    await server.close();
  }
});

test("a video over its cap is refused, announced or streamed", async () => {
  const announced = await host((_request, response) => {
    response.writeHead(200, { "content-type": "video/mp4", "content-length": 21 * MIB });
    response.write(MP4);
    setTimeout(() => response.destroy(), 200);
  });
  const streamed = await host((_request, response) => {
    response.writeHead(200, { "content-type": "video/mp4" });
    response.write(MP4);
    for (let i = 0; i < 21; i += 1) response.write(Buffer.alloc(MIB));
    response.end();
  });
  try {
    await refusedWith(fetchMedia(announced.url("/a.mp4"), announced.options), "unexpected", /large/);
    await refusedWith(fetchMedia(streamed.url("/a.mp4"), streamed.options), "unexpected", /large/);
  } finally {
    await announced.close();
    await streamed.close();
  }
});

test("the bytes must be what the host said they are", async () => {
  const cases: Array<[string, Buffer, string]> = [
    ["a picture sent as a video", PNG, "video/mp4"],
    ["a video sent as a picture", MP4, "image/png"],
    ["noise sent as a video", Buffer.alloc(64, 7), "video/mp4"],
    ["a video with no declared type", MP4, "application/octet-stream"],
    ["a video sent as a document", MP4, "text/html"],
    ["a picture of the MP4 family sent as a video", AVIF_FILE, "video/mp4"],
  ];
  for (const [name, bytes, declared] of cases) {
    const server = await host(image(bytes, declared));
    try {
      await refusedWith(fetchMedia(server.url(), server.options), "unexpected", /picture or a video/);
    } catch (error) {
      throw new Error(`${name}: ${String(error)}`);
    } finally {
      await server.close();
    }
  }
});

test("videos are told apart from pictures by their first bytes", () => {
  assert.equal(sniffVideo(MP4), "video/mp4");
  assert.equal(sniffVideo(Buffer.concat([Buffer.alloc(4), Buffer.from("ftypisom"), Buffer.alloc(8)])), "video/mp4");
  assert.equal(sniffVideo(Buffer.concat([Buffer.alloc(4), Buffer.from("ftypqt  "), Buffer.alloc(8)])), "video/mp4");
  assert.equal(sniffVideo(WEBM), "video/webm");
  for (const brand of ["avif", "avis", "heic", "heix", "mif1"]) {
    assert.equal(sniffVideo(Buffer.concat([Buffer.alloc(4), Buffer.from(`ftyp${brand}`), Buffer.alloc(8)])), undefined, brand);
  }
  for (const bytes of ["", "ftyp", "<html></html>", "GIF89a", "\x89PNG\r\n\x1a\n"]) {
    assert.equal(sniffVideo(Buffer.from(bytes, "latin1")), undefined, JSON.stringify(bytes));
  }
  assert.equal(sniffImage(MP4), undefined, "a video is not a picture");
});
