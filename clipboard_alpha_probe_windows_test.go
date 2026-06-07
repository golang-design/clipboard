// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

//go:build windows

package clipboard_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.design/x/clipboard"
)

// TestAlphaProbe is an INVESTIGATION (re: #105/#108), not an assertion. It writes
// a known semi-transparent pixel and asks WPF (System.Windows.Clipboard) what it
// decodes the CF_DIBV5 back to, to learn whether WPF is a faithful straight-alpha
// oracle for CI. The source pixel is NRGBA{200,100,50,128}; premultiplied storage
// would darken RGB to ~{100,50,25}.
func TestAlphaProbe(t *testing.T) {
	if _, err := exec.LookPath("powershell"); err != nil {
		t.Skip("powershell not found")
	}

	// 4x4 solid, half-transparent, mid-range channels (avoid 0/255 saturation).
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
	clipboard.Write(clipboard.FmtImage, buf.Bytes())
	time.Sleep(200 * time.Millisecond)

	ps := `
Add-Type -AssemblyName PresentationCore,WindowsBase
try {
  $bs = [System.Windows.Clipboard]::GetImage()
} catch { Write-Output ("ERR=" + $_.Exception.Message); exit }
if ($bs -eq $null) { Write-Output "NULL"; exit }
Write-Output ("FORMAT=" + $bs.Format.ToString() + " " + $bs.PixelWidth + "x" + $bs.PixelHeight)
$stride = $bs.PixelWidth * 4
$arr = New-Object byte[] ($stride * $bs.PixelHeight)
try { $bs.CopyPixels($arr, $stride, 0) } catch { Write-Output ("COPYERR=" + $_.Exception.Message); exit }
Write-Output ("BGRA=" + $arr[0] + "," + $arr[1] + "," + $arr[2] + "," + $arr[3])
`
	out, err := exec.Command("powershell", "-NoProfile", "-STA", "-Command", ps).CombinedOutput()
	t.Logf("WPF clipboard probe (err=%v):\n%s", err, out)
}
