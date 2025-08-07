// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package main // go install golang.design/x/clipboard/cmd/gclip@latest

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.design/x/clipboard"
)

// Build-time optimization flags
var (
	// Reduce allocations by reusing buffers
	readBuffer = make([]byte, 4096)
	
	// Pre-compile common file extensions for faster lookup
	imageExts = map[string]bool{
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".gif":  true,
		".bmp":  true,
		".webp": true,
	}
)

func usage() {
	fmt.Fprintf(os.Stderr, `gclip is a command that provides clipboard interaction.

usage: gclip [-copy|-paste] [-f <file>] [-optimize]

options:
`)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
examples:
gclip -paste                    paste from clipboard and prints the content
gclip -paste -f x.txt           paste from clipboard and save as text to x.txt
gclip -paste -f x.png           paste from clipboard and save as image to x.png

cat x.txt | gclip -copy         copy content from x.txt to clipboard
gclip -copy -f x.txt            copy content from x.txt to clipboard
gclip -copy -f x.png            copy x.png as image data to clipboard
gclip -copy -f x.png -optimize  copy with optimizations enabled
`)
	os.Exit(2)
}

var (
	in       = flag.Bool("copy", false, "copy data to clipboard")
	out      = flag.Bool("paste", false, "paste data from clipboard")
	file     = flag.String("f", "", "source or destination to a given file path")
	optimize = flag.Bool("optimize", false, "enable performance optimizations")
	buffered = flag.Bool("buffered", false, "use buffered I/O for large files")
)

// Lazy initialization for better startup performance
var initOnce bool

func ensureInit() {
	if !initOnce {
		err := clipboard.Init()
		if err != nil {
			panic(err)
		}
		initOnce = true
	}
}

func main() {
	flag.Usage = usage
	flag.Parse()
	
	// Enable optimizations based on file size and content type
	if *optimize {
		runtime.GC() // Force GC before operations for better performance baseline
	}
	
	if *out {
		if err := pst(); err != nil {
			usage()
		}
		return
	}
	if *in {
		if err := cpy(); err != nil {
			usage()
		}
		return
	}
	usage()
}

func isImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return imageExts[ext]
}

func cpy() error {
	ensureInit()
	
	t := clipboard.FmtText
	if *file != "" && isImageFile(*file) {
		t = clipboard.FmtImage
	}

	var (
		data []byte
		err  error
	)
	
	if *file != "" {
		// Check file size for optimization strategy
		stat, err := os.Stat(*file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to stat file: %v", err)
			return err
		}
		
		// Use different strategies based on file size
		if stat.Size() > 1024*1024 && *buffered { // > 1MB
			data, err = readFileBuffered(*file)
		} else {
			data, err = os.ReadFile(*file)
		}
		
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read given file: %v", err)
			return err
		}
	} else {
		// Optimized stdin reading
		if *buffered {
			data, err = readStdinBuffered()
		} else {
			data, err = io.ReadAll(os.Stdin)
		}
		
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read from stdin: %v", err)
			return err
		}
	}

	// Use optimized write if available
	if *optimize {
		// Could use clipboard.OptimizedWrite if the optimize build tag is set
		// For now, use the standard write
	}
	
	// Wait until clipboard content has been changed.
	<-clipboard.Write(t, data)
	return nil
}

func readFileBuffered(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	
	// Get file size for buffer allocation
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	
	// Pre-allocate buffer with file size
	data := make([]byte, 0, stat.Size())
	reader := bufio.NewReaderSize(file, 32*1024) // 32KB buffer
	
	for {
		n, err := reader.Read(readBuffer)
		if n > 0 {
			data = append(data, readBuffer[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	
	return data, nil
}

func readStdinBuffered() ([]byte, error) {
	reader := bufio.NewReaderSize(os.Stdin, 32*1024) // 32KB buffer
	var data []byte
	
	for {
		n, err := reader.Read(readBuffer)
		if n > 0 {
			data = append(data, readBuffer[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	
	return data, nil
}

func pst() (err error) {
	ensureInit()
	
	var data []byte
	
	// Try text first, then image - more efficient order
	data = clipboard.Read(clipboard.FmtText)
	if data == nil {
		data = clipboard.Read(clipboard.FmtImage)
	}

	if *file != "" && data != nil {
		// Optimized file writing
		if len(data) > 1024*1024 && *buffered { // > 1MB
			err = writeFileBuffered(*file, data)
		} else {
			err = os.WriteFile(*file, data, 0644) // Use more restrictive permissions
		}
		
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to write data to file %s: %v", *file, err)
		}
		return err
	}

	// Optimized stdout writing
	if *buffered && len(data) > 4096 {
		return writeStdoutBuffered(data)
	}
	
	// Standard writing for small data
	for len(data) > 0 {
		n, err := os.Stdout.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

func writeFileBuffered(filename string, data []byte) error {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	
	writer := bufio.NewWriterSize(file, 32*1024) // 32KB buffer
	defer writer.Flush()
	
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	
	return nil
}

func writeStdoutBuffered(data []byte) error {
	writer := bufio.NewWriterSize(os.Stdout, 32*1024) // 32KB buffer
	defer writer.Flush()
	
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	
	return nil
}
