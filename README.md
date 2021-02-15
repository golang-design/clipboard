# clipboard [![PkgGoDev](https://pkg.go.dev/badge/golang.design/x/clipboard)](https://pkg.go.dev/golang.design/x/clipboard) ![](https://changkun.de/urlstat?mode=github&repo=golang-design/clipboard) ![clipboard](https://github.com/golang-design/clipboard/workflows/clipboard/badge.svg?branch=main)

cross platform clipboard access in Go

```go
import "golang.design/x/clipboard"
```

## Dependency

- Linux users: require X: `apt install -y libx11-dev` or `xorg-dev`
- macOS users: require Cgo, no dependency
- Windows users: unsupported yet

## API Usage

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

## Command Usage

```sh
go install golang.design/x/clipboard/cmd/gclip@latest
```

```
gclip is a command that provides clipboard interaction.

usage: gclip [-copy|-paste] [-f <file>]

options:
  -copy
        copy data to clipboard
  -f string
        source or destination to a given file path
  -paste
        paste data from clipboard

examples:
gclip -paste                    paste from clipboard and prints the content
gclip -paste -f x.txt           paste from clipboard and save as text to x.txt
gclip -paste -f x.png           paste from clipboard and save as image to x.png

cat x.txt | gclip -copy         copy content from x.txt to clipboard
gclip -copy -f x.txt            copy content from x.txt to clipboard
gclip -copy -f x.png            copy x.png as image data to clipboard
```

## Notes

To put image data to system clipboard, you could:

- On macOS, using shortcut Ctrl+Shift+Cmd+4
- On Linux/Ubuntu, using Ctrl+Shift+PrintScreen

The package supports read/write plain text or PNG encoded image data.
The other types of data are not supported yet, i.e. undefined behavior.

## License

GNU GPL-3 Copyright &copy; 2021 The golang.design Initiative Authors, written by [Changkun Ou](https://changkun.de).