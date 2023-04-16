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

unsigned int clipboard_read(void **out, void* t) {
    NSString *cs = *((__unsafe_unretained NSString **)(t));
	NSPasteboardType tt = (NSPasteboardType)cs;

	NSPasteboard * pasteboard = [NSPasteboard generalPasteboard];
	NSData *data = [pasteboard dataForType:tt];
	if (data == nil) {
		return 0;
	}
	NSUInteger siz = [data length];
	*out = malloc(siz);
	[data getBytes: *out length: siz];
	return siz;
}

int clipboard_write(const void *bytes, NSInteger n, void* t) {
    NSString *cs = *((__unsafe_unretained NSString **)(t));
	NSPasteboardType tt = (NSPasteboardType)cs;
	NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
	NSData *data = [NSData dataWithBytes: bytes length: n];
	[pasteboard clearContents];
	BOOL ok = [pasteboard setData: data forType:tt];
	if (!ok) {
		return -1;
	}
	return 0;
}

NSInteger clipboard_change_count() {
	return [[NSPasteboard generalPasteboard] changeCount];
}
