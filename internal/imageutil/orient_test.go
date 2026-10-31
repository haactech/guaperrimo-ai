package imageutil

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// sidewaysJPEG is 32x16: left half red, right half blue, with an EXIF
// orientation flag inserted after SOI, like an iPhone portrait photo.
func sidewaysJPEG(t *testing.T, orientation uint16, byteOrder binary.ByteOrder) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			c := color.RGBA{220, 20, 20, 255}
			if x >= 16 {
				c = color.RGBA{20, 20, 220, 255}
			}
			img.Set(x, y, c)
		}
	}
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}

	tiff := make([]byte, 26)
	if byteOrder == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	byteOrder.PutUint16(tiff[2:], 42)
	byteOrder.PutUint32(tiff[4:], 8)
	byteOrder.PutUint16(tiff[8:], 1)       // one entry
	byteOrder.PutUint16(tiff[10:], 0x0112) // orientation
	byteOrder.PutUint16(tiff[12:], 3)      // SHORT
	byteOrder.PutUint32(tiff[14:], 1)
	byteOrder.PutUint16(tiff[18:], orientation)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	seg = append(seg, payload...)

	b := enc.Bytes()
	out := append([]byte{}, b[:2]...)
	out = append(out, seg...)
	return append(out, b[2:]...)
}

func isRed(c color.Color) bool  { r, _, b, _ := c.RGBA(); return r > 2*b }
func isBlue(c color.Color) bool { r, _, b, _ := c.RGBA(); return b > 2*r }

func TestUprightJPEGRotatesPortraitPhotos(t *testing.T) {
	for _, bo := range []binary.ByteOrder{binary.BigEndian, binary.LittleEndian} {
		out, o, err := UprightJPEG(sidewaysJPEG(t, 6, bo))
		if err != nil {
			t.Fatal(err)
		}
		if o != 6 {
			t.Fatalf("orientation = %d, want 6", o)
		}
		img, err := jpeg.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != 16 || b.Dy() != 32 {
			t.Fatalf("size = %v, want 16x32", b.Size())
		}
		// Rotating 90° clockwise puts the left (red) half on top.
		if !isRed(img.At(8, 4)) || !isBlue(img.At(8, 28)) {
			t.Fatalf("top %v bottom %v: want red over blue", img.At(8, 4), img.At(8, 28))
		}
		if exifOrientation(out) != 1 {
			t.Fatal("output still carries an orientation flag")
		}
	}
}

func TestUprightJPEGOtherOrientations(t *testing.T) {
	cases := []struct {
		o           uint16
		w, h        int
		redAt, blue image.Point
	}{
		{1, 32, 16, image.Pt(4, 8), image.Pt(28, 8)},
		{3, 32, 16, image.Pt(28, 8), image.Pt(4, 8)}, // 180°: red moves right
		{8, 16, 32, image.Pt(8, 28), image.Pt(8, 4)}, // 90° counter-clockwise: red at the bottom
	}
	for _, c := range cases {
		out, _, err := UprightJPEG(sidewaysJPEG(t, c.o, binary.BigEndian))
		if err != nil {
			t.Fatal(err)
		}
		img, _ := jpeg.Decode(bytes.NewReader(out))
		if b := img.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
			t.Errorf("o=%d size %v", c.o, b.Size())
			continue
		}
		if !isRed(img.At(c.redAt.X, c.redAt.Y)) || !isBlue(img.At(c.blue.X, c.blue.Y)) {
			t.Errorf("o=%d wrong pixels", c.o)
		}
	}
}

func TestUprightJPEGRejectsGarbage(t *testing.T) {
	if _, _, err := UprightJPEG([]byte("not a jpeg")); err == nil {
		t.Fatal("expected an error")
	}
}
