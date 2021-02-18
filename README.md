# clipboard [![PkgGoDev](https://pkg.go.dev/badge/golang.design/x/clipboard)](https://pkg.go.dev/golang.design/x/clipboard) ![](https://changkun.de/urlstat?mode=github&repo=golang-design/clipboard) ![clipboard](https://github.com/golang-design/clipboard/workflows/clipboard/badge.svg?branch=main)

cross platform clipboard package in Go

```go
import "golang.design/x/clipboard"
```

## Dependency

- Linux users: require X: `apt install -y libx11-dev` or `xorg-dev`
- macOS users: require Cgo, no dependency
- Windows users: unsupported yet

## API Usage

Quick start:

```go
// write/read text format data of the clipboard
clipboard.Write(clipboard.FmtText, []byte("text data"))
clipboard.Read(clipboard.FmtText)

// write/read image format data of the clipboard, assume
// image bytes are png encoded.
clipboard.Write(clipboard.FmtImage, []byte("image data"))
clipboard.Read(clipboard.FmtImage)
```

In addition, the `clipboard.Write` API returns a channel that
can receive an empty struct as a signal that indicates the
corresponding write call to the clipboard is outdated, meaning
the clipboard has been overwritten by others and the previously
written data is lost. For instance:

```go
changed := clipboard.Write(clipboard.FmtText, []byte("text data"))

select {
case <-changed:
      println(`"text data" is no longer available from clipboard.`)
}
```

You can ignore the reutrning channel if you don't need this type of
notification. Furthermore, when you need more than just knowing whether
clipboard data is changed, use the watcher API:

```go
ch := clipboard.Watch(context.TODO(), clipboard.FmtText)
for data := range ch {
      // print out clipboard data whenever it is changed
      println(string(data))
}
```

## Command Usage

`gclip` command offers the ability to interact with the system clipboard
from the shell. To install:

```bash
$ go install golang.design/x/clipboard/cmd/gclip@latest
```

```bash
$ gclip
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

If `-copy` is used, the command will exit when the data is no longer
available from the clipboard. You can always send the command to the
background using a shell `&` operator, for example:

```bash
$ cat x.txt | gclip -copy &
```

## Additional Notes

In general, to put image data to system clipboard, there are system level shortcuts:

- On macOS, using shortcut `Ctrl+Shift+Cmd+4`
- On Linux/Ubuntu, using `Ctrl+Shift+PrintScreen`

The package supports read/write plain text or PNG encoded image data.
The other types of data are not supported yet, i.e. undefined behavior.

## License

MIT | &copy; 2021 The golang.design Initiative Authors, written by [Changkun Ou](https://changkun.de).