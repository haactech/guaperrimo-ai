// Package imageutil prepares user photos before they are stored.
package imageutil

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
)

// UprightJPEG decodes a JPEG, applies its EXIF orientation to the pixels and
// re-encodes it without metadata. Phones save photos sideways with an
// orientation flag; some try-on models ignore the flag and get a lying-down
// person. Dropping EXIF also removes GPS and device data from the stored
// photo. orientation is the flag that was applied (1 = already upright).
func UprightJPEG(data []byte) (out []byte, orientation int, err error) {
	orientation = exifOrientation(data)
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, orientation, fmt.Errorf("decoding jpeg: %w", err)
	}
	img = orient(img, orientation)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92}); err != nil {
		return nil, orientation, fmt.Errorf("encoding jpeg: %w", err)
	}
	return buf.Bytes(), orientation, nil
}

// exifOrientation reads tag 0x0112 from the JPEG's EXIF block; 1 when absent
// or unreadable.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xDA || marker == 0xD9 { // image data starts: no EXIF before it
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if size < 2 || i+2+size > len(data) {
			return 1
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	ifd := int(bo.Uint32(t[4:8]))
	if ifd+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[ifd : ifd+2]))
	for e := 0; e < n; e++ {
		off := ifd + 2 + e*12
		if off+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[off:off+2]) == 0x0112 {
			if v := int(bo.Uint16(t[off+8 : off+10])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// orient returns img as it should be displayed for the EXIF orientation o.
func orient(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	b := img.Bounds()
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 { // 5-8 swap width and height
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for dy := 0; dy < dh; dy++ {
		for dx := 0; dx < dw; dx++ {
			var sx, sy int
			switch o {
			case 2: // mirrored
				sx, sy = w-1-dx, dy
			case 3: // rotated 180
				sx, sy = w-1-dx, h-1-dy
			case 4: // mirrored vertically
				sx, sy = dx, h-1-dy
			case 5: // transposed
				sx, sy = dy, dx
			case 6: // rotate 90 clockwise to display (iPhone portrait)
				sx, sy = dy, h-1-dx
			case 7: // transversed
				sx, sy = w-1-dy, h-1-dx
			case 8: // rotate 90 counter-clockwise to display
				sx, sy = w-1-dy, dx
			}
			si, di := src.PixOffset(sx, sy), dst.PixOffset(dx, dy)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}
