package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework Cocoa
#import <Foundation/Foundation.h>
#import <Cocoa/Cocoa.h>
*/
import "C"
import (
	"os"
	"unsafe"

	"golang.design/x/clipboard"
)

var f = unsafe.Pointer(C.NSPasteboardTypePDF)

type audioHandler struct{}

func (ah *audioHandler) Format() interface{} { return f }

func main() {
	err := clipboard.Init()
	if err != nil {
		panic(err)
	}
	clipboard.Register(&audioHandler{})

	content, err := os.ReadFile("~/test.pdf")
	if err != nil {
		panic(err)
	}

	clipboard.Write(f, content)
	b := clipboard.Read(clipboard.FmtText)
	os.WriteFile("x.txt", b, os.ModePerm)
}
