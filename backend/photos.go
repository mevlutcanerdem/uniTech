package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"net/http"
)

const (
	maxPhotoBytes  = 8 << 20 // yüklenen dosya en fazla 8 MB
	maxPhotoPixels = 40_000_000
	photoMaxSide   = 1280 // Neon ücretsiz 1 GB: ~150-200 KB/fotoğraf
	photoQuality   = 78
)

var ErrPhotoFormat = errors.New("sadece JPEG veya PNG fotoğraf yüklenebilir")
var ErrPhotoTooBig = errors.New("fotoğraf çok büyük (en fazla 8 MB)")

// ProcessPhoto fotoğrafı doğrular, yönünü düzeltir, küçültür ve JPEG olarak yeniden kodlar.
// Yeniden kodlama, konum (GPS) dahil tüm EXIF bilgisini siler.
func ProcessPhoto(raw []byte) (data []byte, w, h int, err error) {
	if len(raw) > maxPhotoBytes {
		return nil, 0, 0, ErrPhotoTooBig
	}
	ct := http.DetectContentType(raw)
	if ct != "image/jpeg" && ct != "image/png" {
		return nil, 0, 0, ErrPhotoFormat
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 {
		return nil, 0, 0, ErrPhotoFormat
	}
	if cfg.Width*cfg.Height > maxPhotoPixels {
		return nil, 0, 0, ErrPhotoTooBig
	}
	var src image.Image
	if ct == "image/jpeg" {
		src, err = jpeg.Decode(bytes.NewReader(raw))
	} else {
		src, err = png.Decode(bytes.NewReader(raw))
	}
	if err != nil {
		return nil, 0, 0, ErrPhotoFormat
	}
	img := toRGBA(src)
	if ct == "image/jpeg" {
		img = orient(img, jpegOrientation(raw))
	}
	img = downscale(img, photoMaxSide)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: photoQuality}); err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	return buf.Bytes(), b.Dx(), b.Dy(), nil
}

func toRGBA(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	// Saydam PNG'ler JPEG'de siyah görünmesin diye beyaz zemin.
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}

// downscale uzun kenarı max piksele indirir (alan ortalaması, kaliteli küçültme).
func downscale(src *image.RGBA, max int) *image.RGBA {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if sw <= max && sh <= max {
		return src
	}
	dw, dh := max, sh*max/sw
	if sh > sw {
		dw, dh = sw*max/sh, max
	}
	dw, dh = maxInt(dw, 1), maxInt(dh, 1)
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		y0, y1 := y*sh/dh, maxInt((y+1)*sh/dh, y*sh/dh+1)
		for x := 0; x < dw; x++ {
			x0, x1 := x*sw/dw, maxInt((x+1)*sw/dw, x*sw/dw+1)
			var r, g, b, n uint32
			for sy := y0; sy < y1; sy++ {
				row := src.Pix[sy*src.Stride:]
				for sx := x0; sx < x1; sx++ {
					i := sx * 4
					r += uint32(row[i])
					g += uint32(row[i+1])
					b += uint32(row[i+2])
					n++
				}
			}
			o := y*dst.Stride + x*4
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = uint8(r/n), uint8(g/n), uint8(b/n), 255
		}
	}
	return dst
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// orient EXIF yön değerine (1-8) göre görüntüyü döndürür/çevirir.
func orient(src *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			copy(dst.Pix[dy*dst.Stride+dx*4:dy*dst.Stride+dx*4+4], src.Pix[y*src.Stride+x*4:y*src.Stride+x*4+4])
		}
	}
	return dst
}

// jpegOrientation JPEG içindeki EXIF yön etiketini (0x0112) okur; bulunamazsa 1.
func jpegOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		if marker == 0xD9 || marker == 0xDA { // görüntü verisi başladı
			return 1
		}
		size := int(binary.BigEndian.Uint16(b[i+2:]))
		if size < 2 || i+2+size > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return exifOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func exifOrientation(t []byte) int {
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
	off := int(bo.Uint32(t[4:]))
	if off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off:]))
	for k := 0; k < n; k++ {
		e := off + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			v := int(bo.Uint16(t[e+8:]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
