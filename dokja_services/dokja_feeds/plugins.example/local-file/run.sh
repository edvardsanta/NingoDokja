#!/bin/sh
# Prints the items of a local JSON file named by EXAMPLE_FEED_FILE, so this example never
# touches the network. A real plugin prints the same shape on stdout, {"items": [...]}, and
# writes anything it wants to say to stderr, which only goes to the service log.
set -eu
: "${EXAMPLE_FEED_FILE:?set EXAMPLE_FEED_FILE to the path of a JSON file}"
exec cat -- "$EXAMPLE_FEED_FILE"
