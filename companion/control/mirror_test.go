package main

import (
	"image"
	"image/color"
	"testing"
)

func TestMirrorTileRectsCoverCanvas(t *testing.T) {
	rects := mirrorTileRects()
	if len(rects) != 20 {
		t.Fatalf("expected 20 tiles, got %d", len(rects))
	}
	seen := 0
	for _, rect := range rects {
		if rect.Min.X < 0 || rect.Min.Y < 0 || rect.Max.X > mirrorWidth || rect.Max.Y > mirrorHeight {
			t.Fatalf("tile outside canvas: %v", rect)
		}
		seen += rect.Dx() * rect.Dy()
	}
	if seen != mirrorWidth*mirrorHeight {
		t.Fatalf("tiles cover %d pixels, expected %d", seen, mirrorWidth*mirrorHeight)
	}
}

func TestMirrorDeltaUnchangedFrameProducesNoTiles(t *testing.T) {
	canvas := image.NewRGBA(image.Rect(0, 0, mirrorWidth, mirrorHeight))
	for y := 0; y < mirrorHeight; y++ {
		for x := 0; x < mirrorWidth; x++ {
			canvas.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 32, A: 255})
		}
	}
	first, err := encodeMirrorJPEG(canvas)
	if err != nil {
		t.Fatal(err)
	}
	state := mirrorDeltaState{}
	if tiles, err := state.prepare(first, true); err != nil || len(tiles) != len(mirrorTileRects()) {
		t.Fatalf("first frame: tiles=%d err=%v", len(tiles), err)
	}
	second, err := encodeMirrorJPEG(canvas)
	if err != nil {
		t.Fatal(err)
	}
	tiles, err := state.prepare(second, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tiles) != 0 {
		t.Fatalf("unchanged frame: tiles=%d", len(tiles))
	}
}
