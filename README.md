# clipboard [![PkgGoDev](https://pkg.go.dev/badge/golang.design/x/clipboard)](https://pkg.go.dev/golang.design/x/clipboard) ![](https://changkun.de/urlstat?mode=github&repo=golang-design/clipboard) ![clipboard](https://github.com/golang-design/clipboard/workflows/clipboard/badge.svg?branch=main)

cross platform clipboard access in Go

```go
import "golang.design/x/clipboard"
```

## Dependency

- Linux users: `apt install -y libx11-dev`
- macOS users: no dependency

## Usage

```go
// write texts to the clipboard
clipboard.Write(clipboard.MIMEText, []byte("text data"))

// read texts from the clipboard
clipboard.Read(clipboard.MIMEText)

// write image to the clipboard, assume image bytes are png encoded.
clipboard.Write(clipboard.MIMEImage, []byte("image data"))

// read image from the clipboard
clipboard.Read(clipboard.MIMEImage)
```

## Notes

To put image data to system clipboard, you could:

- On macOS, using shortcut Ctrl+Shift+Cmd+4
- On Linux/Ubuntu, using Ctrl+Shift+PrintScreen

The package supports read/write plain text or PNG encoded image data.
The other types of data are not supported yet, i.e. undefined behavior.

## License

GNU GPL-3 Copyright &copy; 2021 The golang.design Initiative Authors, written by [Changkun Ou](https://changkun.de).