# Host-local post reader relay

This optional mechanism plugin relays `social.resolve.v1` requests to an owner-local
worker over its Unix socket. Site extraction belongs in that worker. The worker
must expose authenticated `POST /resolve` metadata and cookie-free `GET /media/`
files with opaque 48-hex filenames, as used by the local post-preview contract.

Copy this directory into the ignored plugins directory, copy `settings.example.json`
to `settings.json`, configure the worker socket, exact media origin and private
worker token, then enable the manifest. Keep tokens and actual settings out of Git.
If the original reader already provides both actions, use it directly instead of
enabling a second reader.

- `read {url}` returns the worker's public metadata and media references.
- `media {url}` reads a reference from that worker's own cache and returns
  `{content_type, data_b64}`. It accepts JPEG, PNG, WebP and MP4, up to 16 MiB;
  a larger file returns `{"refused": "media_too_large"}`.

The socket path is private operator configuration. Caller-supplied media URLs cannot
select another origin, route, filesystem path or network endpoint. The plugin never
fetches those URLs directly. Its 23 MiB output budget accommodates base64 media.

Run the offline tests with `python3 -m unittest -v` in this directory.
