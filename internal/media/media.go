// Package media keeps pictures on disk, named by the hash of their bytes, shrunk once when saved.
// The same picture fetched on ten days is stored once, and an old edition never loses one.
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	_ "image/gif"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"tisak/internal/fetch"
	"tisak/internal/store"
)

type Store struct {
	Dir    string // root folder; files go in Dir/ab/abcdef….jpg
	DB     *store.Store
	Client *fetch.Client
}

// Path is where a stored file lives.
func (s *Store) Path(hash, ext string) string {
	return filepath.Join(s.Dir, hash[:2], hash+"."+ext)
}

// SaveURL downloads a picture, shrinks it to maxWidth and stores it. It gives the hash.
func (s *Store) SaveURL(ctx context.Context, url string, maxWidth int) (string, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("picture: not a web address: %q", url)
	}
	body, err := s.Client.Get(ctx, url)
	if err != nil {
		return "", err
	}
	return s.SaveBytes(ctx, body, url, maxWidth)
}

// SaveFile stores a local file, such as a source logo. SVG is kept as it is; other pictures are shrunk.
func (s *Store) SaveFile(ctx context.Context, path string, maxWidth int) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return s.SaveBytes(ctx, body, "file:"+filepath.Base(path), maxWidth)
}

func (s *Store) SaveBytes(ctx context.Context, body []byte, from string, maxWidth int) (string, error) {
	out, ext, w, h, err := Shrink(body, maxWidth)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(out)
	hash := hex.EncodeToString(sum[:])
	path := s.Path(hash, ext)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, out, 0o644); err != nil {
			return "", err
		}
		if err := os.Rename(tmp, path); err != nil {
			return "", err
		}
	}
	err = s.DB.SaveMedia(ctx, store.Media{Hash: hash, Ext: ext, Width: w, Height: h, Bytes: len(out), SourceURL: from})
	return hash, err
}

// Shrink decodes a picture and re-encodes it no wider than maxWidth: JPEG for photos,
// PNG when the picture has transparency. SVG passes through untouched.
func Shrink(body []byte, maxWidth int) (out []byte, ext string, w, h int, err error) {
	if looksSVG(body) {
		return body, "svg", 0, 0, nil
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, "", 0, 0, fmt.Errorf("picture: %w", err)
	}
	b := img.Bounds()
	if b.Dx() < 16 || b.Dy() < 16 {
		return nil, "", 0, 0, errors.New("picture: too small, probably a tracking pixel")
	}
	if maxWidth > 0 && b.Dx() > maxWidth {
		nh := b.Dy() * maxWidth / b.Dx()
		dst := image.NewRGBA(image.Rect(0, 0, maxWidth, nh))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}
	b = img.Bounds()
	var buf bytes.Buffer
	if transparent(img) {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img)
		return buf.Bytes(), "png", b.Dx(), b.Dy(), err
	}
	// JPEG has no alpha: flatten onto white so nothing turns black
	flat := image.NewRGBA(b)
	draw.Draw(flat, b, &image.Uniform{color.White}, image.Point{}, draw.Src)
	draw.Draw(flat, b, img, b.Min, draw.Over)
	err = jpeg.Encode(&buf, flat, &jpeg.Options{Quality: 78})
	return buf.Bytes(), "jpg", b.Dx(), b.Dy(), err
}

func looksSVG(b []byte) bool {
	head := strings.ToLower(string(b[:min(len(b), 512)]))
	return strings.Contains(head, "<svg")
}

// transparent tells whether any pixel is see-through, checking a grid so big pictures stay quick.
func transparent(img image.Image) bool {
	switch img.(type) {
	case *image.YCbCr, *image.Gray, *image.CMYK:
		return false
	}
	b := img.Bounds()
	step := max(1, min(b.Dx(), b.Dy())/64)
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			if _, _, _, a := img.At(x, y).RGBA(); a < 0xff00 {
				return true
			}
		}
	}
	return false
}
