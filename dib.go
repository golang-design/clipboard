// Copyright 2021 The golang.design Initiative Authors.
// All rights reserved. Use of this source code is governed
// by a MIT license that can be found in the LICENSE file.
//
// Written by Changkun Ou <changkun.de>

package clipboard

import (
	"image"
	"image/color"
	"unsafe"
)

// BITMAPV5Header structure, see:
// https://docs.microsoft.com/en-us/windows/win32/api/wingdi/ns-wingdi-bitmapv5header
//
// This lives in an untagged file (rather than clipboard_windows.go) so the
// pure pixel-packing logic in imageToDIB can be unit tested on any platform.
type bitmapV5Header struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
	RedMask       uint32
	GreenMask     uint32
	BlueMask      uint32
	AlphaMask     uint32
	CSType        uint32
	Endpoints     struct {
		CiexyzRed, CiexyzGreen, CiexyzBlue struct {
			CiexyzX, CiexyzY, CiexyzZ int32 // FXPT2DOT30
		}
	}
	GammaRed    uint32
	GammaGreen  uint32
	GammaBlue   uint32
	Intent      uint32
	ProfileData uint32
	ProfileSize uint32
	Reserved    uint32
}

// imageToDIB converts an image into a CF_DIBV5 memory block: a
// BITMAPV5HEADER followed by 32bpp, bottom-up, BGRA pixel data, matching
// what Windows expects from SetClipboardData(CF_DIBV5, ...).
func imageToDIB(img image.Image) []byte {
	offset := unsafe.Sizeof(bitmapV5Header{})
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	imageSize := 4 * width * height

	data := make([]byte, int(offset)+imageSize)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := int(offset) + 4*(y*width+x)
			// DIB scanlines are stored bottom-up.
			//
			// The BITMAPV5HEADER declares straight (non-premultiplied) alpha
			// via AlphaMask, so store straight 8-bit BGRA. color.Color.RGBA()
			// returns alpha-premultiplied 16-bit channels; converting through
			// NRGBA un-premultiplies and narrows to 8-bit in one step. For
			// opaque pixels this equals the previous uint8(r>>8) (so #48/#102's
			// 16-bit/opaque fix is preserved); for semi-transparent pixels it
			// avoids the darkened, premultiplied RGB consumers misread (#105).
			c := color.NRGBAModel.Convert(img.At(x, height-1-y)).(color.NRGBA)
			data[idx+2] = c.R
			data[idx+1] = c.G
			data[idx+0] = c.B
			data[idx+3] = c.A
		}
	}

	info := bitmapV5Header{}
	info.Size = uint32(offset)
	info.Width = int32(width)
	info.Height = int32(height)
	info.Planes = 1
	info.Compression = 0 // BI_RGB
	info.SizeImage = uint32(4 * info.Width * info.Height)
	info.RedMask = 0xff0000 // default mask
	info.GreenMask = 0xff00
	info.BlueMask = 0xff
	info.AlphaMask = 0xff000000
	info.BitCount = 32 // we only deal with 32 bpp at the moment.
	// Use calibrated RGB values as Go's image/png assumes linear color space.
	// Other options:
	// - LCS_CALIBRATED_RGB = 0x00000000
	// - LCS_sRGB = 0x73524742
	// - LCS_WINDOWS_COLOR_SPACE = 0x57696E20
	// https://docs.microsoft.com/en-us/openspecs/windows_protocols/ms-wmf/eb4bbd50-b3ce-4917-895c-be31f214797f
	info.CSType = 0x73524742
	// Use GL_IMAGES for GamutMappingIntent
	// Other options:
	// - LCS_GM_ABS_COLORIMETRIC = 0x00000008
	// - LCS_GM_BUSINESS = 0x00000001
	// - LCS_GM_GRAPHICS = 0x00000002
	// - LCS_GM_IMAGES = 0x00000004
	// https://docs.microsoft.com/en-us/openspecs/windows_protocols/ms-wmf/9fec0834-607d-427d-abd5-ab240fb0db38
	info.Intent = 4 // LCS_GM_IMAGES

	infob := make([]byte, int(unsafe.Sizeof(info)))
	for i, v := range *(*[unsafe.Sizeof(info)]byte)(unsafe.Pointer(&info)) {
		infob[i] = v
	}
	copy(data[:], infob[:])

	return data
}
