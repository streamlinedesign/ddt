package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func encode(t *testing.T, img image.Image, asPNG bool) []byte {
	var b bytes.Buffer
	var err error
	if asPNG {
		err = png.Encode(&b, img)
	} else {
		err = jpeg.Encode(&b, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestShrinkPhoto(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2000, 1000))
	for x := 0; x < 2000; x++ {
		img.Set(x, x/2, color.RGBA{200, 30, 30, 255})
	}
	out, ext, w, h, err := Shrink(encode(t, img, false), 400)
	if err != nil || ext != "jpg" || w != 400 || h != 200 {
		t.Fatalf("got %s %dx%d, %v", ext, w, h, err)
	}
	if len(out) > 40_000 {
		t.Errorf("400 px JPEG is %d bytes", len(out))
	}
}

func TestShrinkKeepsTransparency(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 100, 40)) // all transparent: a logo's background
	img.Set(10, 10, color.NRGBA{0, 0, 0, 255})
	_, ext, w, _, err := Shrink(encode(t, img, true), 400)
	if err != nil || ext != "png" || w != 100 {
		t.Fatalf("got %s width %d, %v; a transparent logo must stay PNG and not grow", ext, w, err)
	}
}

func TestShrinkSVGAndJunk(t *testing.T) {
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`)
	if out, ext, _, _, err := Shrink(svg, 400); err != nil || ext != "svg" || !bytes.Equal(out, svg) {
		t.Errorf("svg: %s %v", ext, err)
	}
	if _, _, _, _, err := Shrink([]byte("<html>not a picture</html>"), 400); err == nil {
		t.Error("an HTML error page was accepted as a picture")
	}
	pixel := encode(t, image.NewRGBA(image.Rect(0, 0, 1, 1)), true)
	if _, _, _, _, err := Shrink(pixel, 400); err == nil {
		t.Error("a tracking pixel was accepted")
	}
}
