// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build darwin && !ios

// Interact with NSPasteboard using Objective-C
// https://developer.apple.com/documentation/appkit/nspasteboard?language=objc

#import <Foundation/Foundation.h>
#import <Cocoa/Cocoa.h>

unsigned int clipboard_read_string(void **out) {
	NSPasteboard * pasteboard = [NSPasteboard generalPasteboard];
	NSData *data = [pasteboard dataForType:NSPasteboardTypeString];
	if (data == nil) {
		return 0;
	}
	NSUInteger siz = [data length];
	*out = malloc(siz);
	[data getBytes: *out length: siz];
	return siz;
}

unsigned int clipboard_read_image(void **out) {
	NSPasteboard * pasteboard = [NSPasteboard generalPasteboard];
	NSData *data = [pasteboard dataForType:NSPasteboardTypePNG];
	if (data == nil) {
		return 0;
	}
	NSUInteger siz = [data length];
	*out = malloc(siz);
	[data getBytes: *out length: siz];
	return siz;
}

int clipboard_write_string(const void *bytes, NSInteger n) {
	NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
	NSData *data = [NSData dataWithBytes: bytes length: n];
	[pasteboard clearContents];
	BOOL ok = [pasteboard setData: data forType:NSPasteboardTypeString];
	if (!ok) {
		return -1;
	}
	return 0;
}
int clipboard_write_image(const void *bytes, NSInteger n) {
	NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
	NSData *data = [NSData dataWithBytes: bytes length: n];
	[pasteboard clearContents];
	BOOL ok = [pasteboard setData: data forType:NSPasteboardTypePNG];
	if (!ok) {
		return -1;
	}
	return 0;
}

NSInteger clipboard_change_count() {
	return [[NSPasteboard generalPasteboard] changeCount];
}
