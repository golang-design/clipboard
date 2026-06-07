# Test data

Fixtures used by the package tests.

## sample.* (custom-format fixtures)

Real-world files round-tripped through custom clipboard formats
(`clipboard.Register(mime)`) to verify raw, byte-exact passthrough with
realistic content across common MIME types.

| File | MIME | Provenance |
|------|------|------------|
| `sample.html` | `text/html` | Downloaded from <https://example.com/> (IANA's reserved example domain) on 2026-06-07; stored verbatim. The page states "This domain is for use in documentation examples without needing permission," so it is safe to vendor. |
| `sample.pdf` | `application/pdf` | Generated locally with `pandoc` from a one-line Markdown source. |
| `sample.docx` | `application/vnd.openxmlformats-officedocument.wordprocessingml.document` | Generated locally with `pandoc` from a one-line Markdown source. |
| `sample.xlsx` | `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet` | A minimal valid OOXML spreadsheet authored by hand and zipped. |

The generated files contain only placeholder content authored for this
repository, so they carry no third-party license.

## image24bit.bmp

A minimal 8×8 **24-bit** (BI_RGB) BMP with a known pixel pattern
(`R=x*32, G=y*32, B=128`), generated locally with a small stdlib-only program.
Used by the Windows test to put a 24-bit DIB on the clipboard (as `CF_DIB`) and
verify `Read(FmtImage)` decodes it.

## *.png

`clipboard.png` and the per-platform screenshots are PNG fixtures for the image
read/write tests; `clipboard.png`'s bytes are also reused as a real binary
payload for the custom-format round-trip (under `application/octet-stream`).
