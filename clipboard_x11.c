// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build ignore

// This is the cgo X11 clipboard implementation used only by the BSD backend,
// which #includes it textually from clipboard_bsd.c. Linux no longer compiles
// any cgo (it uses the pure-Go backend in clipboard_x11_linux.go), so this file
// carries a build constraint that never matches: it is never built as a
// standalone translation unit, only pulled in by clipboard_bsd.c.

#include <stdlib.h>
#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include <dlfcn.h>
#include <X11/Xlib.h>
#include <X11/Xatom.h>

// syncStatus is a function from the Go side.
extern void syncStatus(uintptr_t handle, int status);

void *libX11;

Display* (*P_XOpenDisplay)(int);
void (*P_XCloseDisplay)(Display*);
Window (*P_XDefaultRootWindow)(Display*);
Window (*P_XCreateSimpleWindow)(Display*, Window, int, int, int, int, int, int, int);
Atom (*P_XInternAtom)(Display*, char*, int);
void (*P_XSetSelectionOwner)(Display*, Atom, Window, unsigned long);
Window (*P_XGetSelectionOwner)(Display*, Atom);
void (*P_XNextEvent)(Display*, XEvent*);
int (*P_XChangeProperty)(Display*, Window, Atom, Atom, int, int, unsigned char*, int);
void (*P_XSendEvent)(Display*, Window, int, long , XEvent*);
int (*P_XGetWindowProperty) (Display*, Window, Atom, long, long, Bool, Atom, Atom*, int*, unsigned long *, unsigned long *, unsigned char **);
void (*P_XFree) (void*);
void (*P_XDeleteProperty) (Display*, Window, Atom);
void (*P_XConvertSelection)(Display*, Atom, Atom, Atom, Window, Time);
int (*P_XSetErrorHandler)(int (*)(Display*, XErrorEvent*));
int (*P_XSync)(Display*, int);

// ignoreXError swallows asynchronous X11 protocol errors instead of letting
// Xlib's default handler terminate the whole process. On a pure Wayland
// session the X selection can be served against an invalid requestor window,
// producing a BadWindow error on X_ChangeProperty; without this handler Xlib
// would print the error and call exit(1), crashing the host program (see #61).
int ignoreXError(Display* d, XErrorEvent* e) {
	(void)d;
	(void)e;
	return 0; // the return value is ignored by Xlib
}

int initX11() {
	if (libX11) {
		return 1;
	}
	libX11 = dlopen("libX11.so", RTLD_LAZY);
    if (!libX11)
    {
        libX11 = dlopen("libX11.so.6", RTLD_LAZY);
        if (!libX11)
        {
            return 0;
        }
    }
    P_XOpenDisplay = (Display* (*)(int)) dlsym(libX11, "XOpenDisplay");
	P_XCloseDisplay = (void (*)(Display*)) dlsym(libX11, "XCloseDisplay");
	P_XDefaultRootWindow = (Window (*)(Display*)) dlsym(libX11, "XDefaultRootWindow");
	P_XCreateSimpleWindow = (Window (*)(Display*, Window, int, int, int, int, int, int, int)) dlsym(libX11, "XCreateSimpleWindow");
	P_XInternAtom = (Atom (*)(Display*, char*, int)) dlsym(libX11, "XInternAtom");
	P_XSetSelectionOwner = (void (*)(Display*, Atom, Window, unsigned long)) dlsym(libX11, "XSetSelectionOwner");
	P_XGetSelectionOwner = (Window (*)(Display*, Atom)) dlsym(libX11, "XGetSelectionOwner");
	P_XNextEvent = (void (*)(Display*, XEvent*)) dlsym(libX11, "XNextEvent");
	P_XChangeProperty = (int (*)(Display*, Window, Atom, Atom, int, int, unsigned char*, int)) dlsym(libX11, "XChangeProperty");
	P_XSendEvent = (void (*)(Display*, Window, int, long , XEvent*)) dlsym(libX11, "XSendEvent");
	P_XGetWindowProperty = (int (*)(Display*, Window, Atom, long, long, Bool, Atom, Atom*, int*, unsigned long *, unsigned long *, unsigned char **)) dlsym(libX11, "XGetWindowProperty");
	P_XFree = (void (*)(void*)) dlsym(libX11, "XFree");
	P_XDeleteProperty = (void (*)(Display*, Window, Atom)) dlsym(libX11, "XDeleteProperty");
	P_XConvertSelection = (void (*)(Display*, Atom, Atom, Atom, Window, Time)) dlsym(libX11, "XConvertSelection");
	P_XSetErrorHandler = (int (*)(int (*)(Display*, XErrorEvent*))) dlsym(libX11, "XSetErrorHandler");
	P_XSync = (int (*)(Display*, int)) dlsym(libX11, "XSync");

	// Install a handler so an asynchronous protocol error can never abort the
	// host process (see #61). This is process-global, as Xlib offers no
	// per-display protocol error handler.
	if (P_XSetErrorHandler != NULL) {
		(*P_XSetErrorHandler)(ignoreXError);
	}
	return 1;
}

// displayUnavailable remembers that opening the X11 display has already
// failed, so later calls can fail fast instead of retrying the open on
// every Write/Read/Watch. Without this, once Init fails (e.g. no DISPLAY
// or no X server) each subsequent call repeats the open attempts and
// blocks before returning the same error (see #85).
static int displayUnavailable = 0;

// openDisplay opens the X11 display, retrying a few times to tolerate a
// transient race during startup. A persistent failure is cached so that
// subsequent calls return immediately rather than retrying.
//
// Note: the fail-fast caching is not covered by a unit test. Its only
// observable effect is latency, and that only matters when XOpenDisplay
// itself is slow (e.g. a TCP timeout to an unreachable X server); with no
// DISPLAY or a missing socket it fails instantly, so there is nothing
// deterministic to assert without a timing heuristic.
Display* openDisplay() {
	if (displayUnavailable) {
		return NULL;
	}
	for (int i = 0; i < 42; i++) {
		Display* d = (*P_XOpenDisplay)(0);
		if (d != NULL) {
			return d;
		}
	}
	displayUnavailable = 1;
	return NULL;
}

int clipboard_test() {
	if (!initX11()) {
		return -1;
	}

	Display* d = openDisplay();
	if (d == NULL) {
		return -1;
	}
	(*P_XCloseDisplay)(d);
	return 0;
}

// clipboard_trigger_protocol_error deliberately issues an invalid X request to
// verify that the error handler installed by initX11 keeps the process alive.
// It returns 0 if the process survived the resulting protocol error, or a
// negative value if the X display is unavailable. Without the handler, Xlib's
// default handler would call exit(1). Exposed only for the #61 regression test.
int clipboard_trigger_protocol_error() {
	if (!initX11()) {
		return -1;
	}
	Display* d = openDisplay();
	if (d == NULL) {
		return -1;
	}
	Atom prop = (*P_XInternAtom)(d, "GOLANG_DESIGN_ERRTEST", 0);
	unsigned char val = 1;
	// 0xffffffff is not a valid window, so this yields a BadWindow protocol
	// error on X_ChangeProperty (the same opcode that crashed in #61).
	(*P_XChangeProperty)(d, (Window)0xffffffff, prop, XA_ATOM, 8, PropModeReplace, &val, 1);
	if (P_XSync != NULL) {
		(*P_XSync)(d, 0); // flush so the error is delivered to our handler
	}
	(*P_XCloseDisplay)(d);
	return 0;
}

// clipboard_write writes the given buf of size n as type typ.
// if start is provided, the value of start will be changed to 1 to indicate
// if the write is availiable for reading.
int clipboard_write(char *typ, unsigned char *buf, size_t n, uintptr_t handle) {
	if (!initX11()) {
        syncStatus(handle, -1);
		return -1;
	}

    Display* d = openDisplay();
    if (d == NULL) {
        syncStatus(handle, -1);
        return -1;
    }
    Window w = (*P_XCreateSimpleWindow)(d, (*P_XDefaultRootWindow)(d), 0, 0, 1, 1, 0, 0, 0);

    // Use False because these may not available for the first time.
    Atom sel         = (*P_XInternAtom)(d, "CLIPBOARD", 0);
    Atom atomString  = (*P_XInternAtom)(d, "UTF8_STRING", 0);
    Atom atomImage   = (*P_XInternAtom)(d, "image/png", 0);
    Atom targetsAtom = (*P_XInternAtom)(d, "TARGETS", 0);

    // Use True to makesure the requested type is a valid type.
    Atom target = (*P_XInternAtom)(d, typ, 1);
    if (target == None) {
        (*P_XCloseDisplay)(d);
        syncStatus(handle, -2);
        return -2;
    }

    (*P_XSetSelectionOwner)(d, sel, w, CurrentTime);
    if ((*P_XGetSelectionOwner)(d, sel) != w) {
        (*P_XCloseDisplay)(d);
        syncStatus(handle, -3);
        return -3;
    }

    XEvent event;
    XSelectionRequestEvent* xsr;
    int notified = 0;
    for (;;) {
        if (notified == 0) {
            syncStatus(handle, 1); // notify Go side
            notified = 1;
        }

        (*P_XNextEvent)(d, &event);
        switch (event.type) {
        case SelectionClear:
            // For debugging:
            // printf("x11write: lost ownership of clipboard selection.\n");
            // fflush(stdout);
            (*P_XCloseDisplay)(d);
            return 0;
        case SelectionNotify:
            // For debugging:
            // printf("x11write: notify.\n");
            // fflush(stdout);
            break;
        case SelectionRequest:
            if (event.xselectionrequest.selection != sel) {
                break;
            }

            XSelectionRequestEvent * xsr = &event.xselectionrequest;
            XSelectionEvent ev = {0};
            int R = 0;

            ev.type      = SelectionNotify;
            ev.display   = xsr->display;
            ev.requestor = xsr->requestor;
            ev.selection = xsr->selection;
            ev.time      = xsr->time;
            ev.target    = xsr->target;
            ev.property  = xsr->property;

            if (ev.target == atomString && ev.target == target) {
                R = (*P_XChangeProperty)(ev.display, ev.requestor, ev.property,
                    atomString, 8, PropModeReplace, buf, n);
            } else if (ev.target == atomImage && ev.target == target) {
                R = (*P_XChangeProperty)(ev.display, ev.requestor, ev.property,
                    atomImage, 8, PropModeReplace, buf, n);
            } else if (ev.target == targetsAtom) {
                // Reply atoms for supported targets, other clients should
                // request the clipboard again and obtain the data if their
                // implementation is correct.
                Atom targets[] = { targetsAtom, target };
                R = (*P_XChangeProperty)(ev.display, ev.requestor, ev.property,
                    XA_ATOM, 32, PropModeReplace,
                    (unsigned char *)&targets, sizeof(targets)/sizeof(Atom));
            } else {
                ev.property = None;
            }

            if ((R & 2) == 0) (*P_XSendEvent)(d, ev.requestor, 0, 0, (XEvent *)&ev);
            break;
        }
    }
}

// read_data reads the property of a selection if the target atom matches
// the actual atom.
unsigned long read_data(XSelectionEvent *sev, Atom sel, Atom prop, Atom target, char **buf) {
	if (!initX11()) {
		return -1;
	}

    unsigned char *data;
    Atom actual;
    int format;
    unsigned long n    = 0;
    unsigned long size = 0;
    if (sev->property == None || sev->selection != sel || sev->property != prop) {
        return 0;
    }

    int ret = (*P_XGetWindowProperty)(sev->display, sev->requestor, sev->property,
        0L, (~0L), 0, AnyPropertyType, &actual, &format, &size, &n, &data);
    if (ret != Success) {
        return 0;
    }

    if (actual == target && buf != NULL) {
        *buf = (char *)malloc(size * sizeof(char));
        memcpy(*buf, data, size*sizeof(char));
    }
    (*P_XFree)(data);
    (*P_XDeleteProperty)(sev->display, sev->requestor, sev->property);
    return size * sizeof(char);
}

// clipboard_read reads the clipboard selection in given format typ.
// the read bytes are written into buf and returns the size of the buffer.
//
// The caller of this function should responsible for the free of the buf.
unsigned long clipboard_read(char* typ, char **buf) {
	if (!initX11()) {
		return -1;
	}

    Display* d = openDisplay();
    if (d == NULL) {
        return -1;
    }

    Window w = (*P_XCreateSimpleWindow)(d, (*P_XDefaultRootWindow)(d), 0, 0, 1, 1, 0, 0, 0);

    // Use False because these may not available for the first time.
    Atom sel  = (*P_XInternAtom)(d, "CLIPBOARD", False);
    Atom prop = (*P_XInternAtom)(d, "GOLANG_DESIGN_DATA", False);

    // Use True to makesure the requested type is a valid type.
    Atom target = (*P_XInternAtom)(d, typ, True);
    if (target == None) {
        (*P_XCloseDisplay)(d);
        return -2;
    }

    (*P_XConvertSelection)(d, sel, target, prop, w, CurrentTime);
    XEvent event;
    for (;;) {
        (*P_XNextEvent)(d, &event);
        if (event.type != SelectionNotify) continue;
        break;
    }
    unsigned long n = read_data((XSelectionEvent *)&event.xselection, sel, prop, target, buf);
    (*P_XCloseDisplay)(d);
    return n;
}
