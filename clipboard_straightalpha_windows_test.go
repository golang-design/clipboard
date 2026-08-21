// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build windows

package clipboard_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.design/x/clipboard"
)

// TestWindowsImageStraightAlpha verifies, via a real Windows consumer, that a
// semi-transparent image written to the clipboard is stored with straight
// (non-premultiplied) alpha — the contract a BITMAPV5HEADER's AlphaMask implies
// (#105). It writes NRGBA{200,100,50,128} and asks WPF
// (System.Windows.Clipboard.GetImage, which exposes CF_DIBV5 as straight
// Bgra32) what it reads back. Premultiplied storage would yield ~{r:100,g:50,
// b:25}; straight yields the original. Skips if the WPF oracle is unavailable.
func TestWindowsImageStraightAlpha(t *testing.T) {
	if _, err := exec.LookPath("powershell"); err != nil {
		t.Skip("powershell not found")
	}

	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	clipboard.Write(context.TODO(), clipboard.FmtImage, buf.Bytes())
	time.Sleep(200 * time.Millisecond)

	const ps = `
Add-Type -AssemblyName PresentationCore,WindowsBase
try { $bs = [System.Windows.Clipboard]::GetImage() } catch { Write-Output ("ERR=" + $_.Exception.Message); exit }
if ($bs -eq $null) { Write-Output "NULL"; exit }
$stride = $bs.PixelWidth * 4
$arr = New-Object byte[] ($stride * $bs.PixelHeight)
try { $bs.CopyPixels($arr, $stride, 0) } catch { Write-Output ("COPYERR=" + $_.Exception.Message); exit }
Write-Output ("FORMAT=" + $bs.Format.ToString())
Write-Output ("BGRA=" + $arr[0] + "," + $arr[1] + "," + $arr[2] + "," + $arr[3])
`
	out, err := exec.Command("powershell", "-NoProfile", "-STA", "-Command", ps).CombinedOutput()
	t.Logf("WPF probe (err=%v):\n%s", err, out)

	var b, g, r, a int
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "BGRA=") {
			if _, e := fmt.Sscanf(strings.TrimSpace(line), "BGRA=%d,%d,%d,%d", &b, &g, &r, &a); e == nil {
				found = true
			}
		}
	}
	if !found {
		t.Skipf("WPF clipboard oracle unavailable: %s", out)
	}

	near := func(got, want int) bool { d := got - want; return d >= -2 && d <= 2 }
	if !near(b, 50) || !near(g, 100) || !near(r, 200) || !near(a, 128) {
		t.Fatalf("WPF read BGRA=(%d,%d,%d,%d) for NRGBA{200,100,50,128}; want straight ~(50,100,200,128). Premultiplied storage would give ~(25,50,100,128).", b, g, r, a)
	}
}
