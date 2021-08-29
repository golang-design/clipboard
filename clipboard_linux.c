// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build linux && !android

#include <stdlib.h>
#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include <X11/Xlib.h>
#include <X11/Xatom.h>

// syncStatus is a function from the Go side.
extern void syncStatus(uintptr_t handle, int status);

int clipboard_test() {
    Display *d = XOpenDisplay(0);
    if (d == NULL) {
        return -1;
    }
    XCloseDisplay(d);
    return 0;
}

// clipboard_write writes the given buf of size n as type typ.
// if start is provided, the value of start will be changed to 1 to indicate
// if the write is availiable for reading.
int clipboard_write(char *typ, unsigned char *buf, size_t n, uintptr_t handle) {
    Display* d = NULL;
    for (int i = 0; i < 42; i++) {
        d = XOpenDisplay(0);
        if (d == NULL) {
            continue;
        }
        break;
    }
    if (d == NULL) {
        syncStatus(handle, -1);
        return -1;
    }

    Window w = XCreateSimpleWindow(d, DefaultRootWindow(d), 0, 0, 1, 1, 0, 0, 0);

    // Use False because these may not available for the first time.
    Atom sel         = XInternAtom(d, "CLIPBOARD", False);
    Atom atomString  = XInternAtom(d, "UTF8_STRING", False);
    Atom atomImage   = XInternAtom(d, "image/png", False);
    Atom targetsAtom = XInternAtom(d, "TARGETS", False);

    // Use True to makesure the requested type is a valid type.
    Atom target = XInternAtom(d, typ, True);
    if (target == None) {
        XCloseDisplay(d);
        syncStatus(handle, -2);
        return -2;
    }

    XSetSelectionOwner(d, sel, w, CurrentTime);
    if (XGetSelectionOwner(d, sel) != w) {
        XCloseDisplay(d);
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

        XNextEvent(d, &event);
        switch (event.type) {
        case SelectionClear:
            // For debugging:
            // printf("x11write: lost ownership of clipboard selection.\n");
            // fflush(stdout);
            XCloseDisplay(d);
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
                R = XChangeProperty(ev.display, ev.requestor, ev.property, 
                    atomString, 8, PropModeReplace, buf, n);
            } else if (ev.target == atomImage && ev.target == target) {
                R = XChangeProperty(ev.display, ev.requestor, ev.property,
                    atomImage, 8, PropModeReplace, buf, n);
            } else if (ev.target == targetsAtom) {
                // Reply atoms for supported targets, other clients should
                // request the clipboard again and obtain the data if their
                // implementation is correct.
                Atom targets[] = { atomString, atomImage };
                R = XChangeProperty(ev.display, ev.requestor, ev.property,
                    XA_ATOM, 32, PropModeReplace,
                    (unsigned char *)&targets, sizeof(targets)/sizeof(Atom));
            } else {
                ev.property = None;
            }

            if ((R & 2) == 0) XSendEvent(d, ev.requestor, 0, 0, (XEvent *)&ev);
            break;
        }
    }
}

// read_data reads the property of a selection if the target atom matches
// the actual atom.
unsigned long read_data(XSelectionEvent *sev, Atom sel, Atom prop, Atom target, char **buf) {
    unsigned char *data;
    Atom actual;
    int format;
    unsigned long n    = 0;
    unsigned long size = 0;
    if (sev->property == None || sev->selection != sel || sev->property != prop) {
        return 0;
    }

    int ret = XGetWindowProperty(sev->display, sev->requestor, sev->property,
        0L, (~0L), 0, AnyPropertyType, &actual, &format, &size, &n, &data);
    if (ret != Success) {
        return 0;
    }

    if (actual == target && buf != NULL) {
        *buf = (char *)malloc(size * sizeof(char));
        memcpy(*buf, data, size*sizeof(char));
    }
    XFree(data);
    XDeleteProperty(sev->display, sev->requestor, sev->property);
    return size * sizeof(char);
}

// clipboard_read reads the clipboard selection in given format typ.
// the readed bytes is written into buf and returns the size of the buffer.
//
// The caller of this function should responsible for the free of the buf.
unsigned long clipboard_read(char* typ, char **buf) {
    Display* d = NULL;
    for (int i = 0; i < 42; i++) {
        d = XOpenDisplay(0);
        if (d == NULL) {
            continue;
        }
        break;
    }
    if (d == NULL) {
        return -1;
    }

    Window w = XCreateSimpleWindow(d, DefaultRootWindow(d), 0, 0, 1, 1, 0, 0, 0);

    // Use False because these may not available for the first time.
    Atom sel  = XInternAtom(d, "CLIPBOARD", False);
    Atom prop = XInternAtom(d, "GOLANG_DESIGN_DATA", False);

    // Use True to makesure the requested type is a valid type.
    Atom target = XInternAtom(d, typ, True);
    if (target == None) {
        XCloseDisplay(d);
        return -2;
    }

    XConvertSelection(d, sel, target, prop, w, CurrentTime);
    XEvent event;
    for (;;) {
        XNextEvent(d, &event);
        if (event.type != SelectionNotify) continue;
        break;
    }
    unsigned long n = read_data((XSelectionEvent *)&event.xselection, sel, prop, target, buf);
    XCloseDisplay(d);
    return n;
}