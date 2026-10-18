package icons

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32 = windows.NewLazySystemDLL("shell32.dll")
	user32  = windows.NewLazySystemDLL("user32.dll")
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")

	procExtractIconExW = shell32.NewProc("ExtractIconExW")
	procGetIconInfo    = user32.NewProc("GetIconInfo")
	procDestroyIcon    = user32.NewProc("DestroyIcon")
	procGetDC          = user32.NewProc("GetDC")
	procReleaseDC      = user32.NewProc("ReleaseDC")
	procGetObjectW     = gdi32.NewProc("GetObjectW")
	procGetDIBits      = gdi32.NewProc("GetDIBits")
	procDeleteObject   = gdi32.NewProc("DeleteObject")
)

// iconInfo mirrors ICONINFO (winuser.h). Layout is checked by TestIconInfoLayout.
type iconInfo struct {
	IsIcon   int32
	XHotspot uint32
	YHotspot uint32
	Mask     uintptr // HBITMAP
	Color    uintptr // HBITMAP
}

// bitmap mirrors BITMAP (wingdi.h).
type bitmap struct {
	Type       int32
	Width      int32
	Height     int32
	WidthBytes int32
	Planes     uint16
	BitsPixel  uint16
	Bits       uintptr
}

// bitmapInfoHeader mirrors BITMAPINFOHEADER (wingdi.h); with no colour table
// it is all GetDIBits needs for 32 bits per pixel.
type bitmapInfoHeader struct {
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
}

// extractPNG renders the file's icon at index (a negative index is a resource
// id) as a PNG, using the large system icon size.
func extractPNG(path string, index int) ([]byte, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var large, small uintptr
	n, _, _ := procExtractIconExW.Call(uintptr(unsafe.Pointer(p)), uintptr(int32(index)),
		uintptr(unsafe.Pointer(&large)), uintptr(unsafe.Pointer(&small)), 1)
	if small != 0 {
		defer procDestroyIcon.Call(small)
	}
	if large != 0 {
		defer procDestroyIcon.Call(large)
	}
	if n == 0 || large == 0 {
		return nil, errors.New("no icon found")
	}

	var info iconInfo
	if r, _, _ := procGetIconInfo.Call(large, uintptr(unsafe.Pointer(&info))); r == 0 {
		return nil, errors.New("could not read the icon")
	}
	if info.Mask != 0 {
		defer procDeleteObject.Call(info.Mask)
	}
	if info.Color != 0 {
		defer procDeleteObject.Call(info.Color)
	}
	if info.Color == 0 {
		return nil, errors.New("monochrome icons are not supported")
	}

	var bm bitmap
	if r, _, _ := procGetObjectW.Call(info.Color, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); r == 0 {
		return nil, errors.New("could not measure the icon")
	}
	w, h := int(bm.Width), int(bm.Height)
	if w <= 0 || h <= 0 || w > 256 || h > 256 {
		return nil, errors.New("unexpected icon size")
	}

	dc, _, _ := procGetDC.Call(0)
	if dc == 0 {
		return nil, errors.New("no device context")
	}
	defer procReleaseDC.Call(0, dc)

	color, err := dibBits(dc, info.Color, w, h)
	if err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	hasAlpha := false
	for i := 0; i < w*h; i++ {
		if color[i*4+3] != 0 {
			hasAlpha = true
			break
		}
	}
	// Icons without an alpha channel carry their transparency in the mask
	// (a set bit means transparent).
	var mask []byte
	if !hasAlpha && info.Mask != 0 {
		mask, _ = dibBits(dc, info.Mask, w, h)
	}
	for i := 0; i < w*h; i++ {
		b, g, r, a := color[i*4], color[i*4+1], color[i*4+2], color[i*4+3]
		if !hasAlpha {
			a = 255
			if len(mask) == len(color) && mask[i*4] != 0 {
				a = 0
			}
		}
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = r, g, b, a
	}

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// dibBits reads a bitmap as top-down 32-bit BGRA.
func dibBits(dc, hbm uintptr, w, h int) ([]byte, error) {
	hdr := bitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(w), Height: -int32(h),
		Planes: 1, BitCount: 32,
	}
	buf := make([]byte, w*h*4)
	lines, _, _ := procGetDIBits.Call(dc, hbm, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&hdr)), 0) // DIB_RGB_COLORS
	if int(lines) != h {
		return nil, errors.New("could not read the icon's pixels")
	}
	return buf, nil
}
