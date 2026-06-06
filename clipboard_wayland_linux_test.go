// Copyright 2026 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build linux && !android

package clipboard

import (
	"os"
	"testing"
)

// TestWaylandDiscoverGlobals verifies the wire core can connect to a Wayland
// compositor and discover the globals the data-control backend needs: a seat
// and a data-control manager. It runs under the headless sway compositor in CI
// (hack/test-wayland.sh / the wayland_test job) and skips when not in a Wayland
// session.
func TestWaylandDiscoverGlobals(t *testing.T) {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("not a Wayland session (WAYLAND_DISPLAY unset)")
	}

	globals, err := wlListGlobals()
	if err != nil {
		t.Fatalf("wlListGlobals: %v", err)
	}
	if len(globals) == 0 {
		t.Fatal("no globals advertised by the compositor")
	}

	if _, ok := globals["wl_seat"]; !ok {
		t.Errorf("compositor did not advertise wl_seat (got %d globals)", len(globals))
	}

	iface, g, ok := dataControlManager(globals)
	if !ok {
		t.Fatalf("compositor advertises no data-control manager; need one of %v", dataControlManagers)
	}
	t.Logf("data-control manager: %s (name=%d, version=%d); %d globals total",
		iface, g.name, g.version, len(globals))
}
