# Test data

Fixtures used by the package tests.

## sample.html

A real `text/html` document used to round-trip a custom clipboard format
(`clipboard.Register("text/html")`) with realistic, non-trivial content.

- **Source:** <https://example.com/> (IANA's reserved example domain).
- **Retrieved:** 2026-06-07.
- **License/usage:** the page states "This domain is for use in documentation
  examples without needing permission," so it is safe to vendor as a fixture.
  Stored verbatim as downloaded.

## *.png

`clipboard.png` and the per-platform screenshots are PNG fixtures for the image
read/write tests; `clipboard.png`'s bytes are also reused as a real binary
payload for the custom-format round-trip (under `application/octet-stream`).
