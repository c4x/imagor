package vipsprocessor

import (
	"context"
	"fmt"
	"github.com/cshum/imagor"
	"github.com/cshum/imagor/imagorpath"
	"github.com/davidbyttow/govips/v2/vips"
	"math"
	"net/url"
	"strconv"
	"strings"
)

func (v *VipsProcessor) fill(ctx context.Context, img *vips.ImageRef, w, h, hPad, vPad int, upscale bool, colour string) (err error) {
	c := getColor(img, colour)
	if colour != "blur" || (colour == "blur" && v.DisableBlur) {
		// fill color
		if img.HasAlpha() {
			if err = img.Flatten(getColor(img, colour)); err != nil {
				return
			}
		}
		left := (w - img.Width()) / 2
		top := (h - img.Height()) / 2
		if isBlack(c) {
			if err = img.Embed(left, top, w, h, vips.ExtendBlack); err != nil {
				return
			}
		} else if isWhite(c) {
			if err = img.Embed(left, top, w, h, vips.ExtendWhite); err != nil {
				return
			}
		} else {
			if err = img.EmbedBackground(left, top, w, h, c); err != nil {
				return
			}
		}
	} else {
		// fill blur
		var cp *vips.ImageRef
		if cp, err = img.Copy(); err != nil {
			return
		}
		AddImageRef(ctx, cp)
		if upscale || w-hPad*2 < img.Width() || h-vPad*2 < img.Height() {
			if err = cp.Thumbnail(w-hPad*2, h-vPad*2, vips.InterestingNone); err != nil {
				return
			}
		}
		if err = img.ThumbnailWithSize(
			w, h, vips.InterestingNone, vips.SizeForce,
		); err != nil {
			return
		}
		if err = img.GaussianBlur(50); err != nil {
			return
		}
		if err = img.Composite(
			cp, vips.BlendModeOver, (w-cp.Width())/2, (h-cp.Height())/2); err != nil {
			return
		}
	}
	return
}

func (v *VipsProcessor) watermark(ctx context.Context, img *vips.ImageRef, load imagor.LoadFunc, args ...string) (err error) {
	ln := len(args)
	if ln < 1 {
		return
	}
	image := args[0]
	if unescape, e := url.QueryUnescape(args[0]); e == nil {
		image = unescape
	}
	var blob *imagor.Blob
	if blob, err = load(image); err != nil {
		return
	}
	var x, y, w, h int
	var repeatX = 1
	var repeatY = 1
	var overlay *vips.ImageRef

	// w_ratio h_ratio
	if ln >= 6 {
		w = img.Width()
		h = img.Height()
		if args[4] != "none" {
			w, _ = strconv.Atoi(args[4])
			w = img.Width() * w / 100
		}
		if args[5] != "none" {
			h, _ = strconv.Atoi(args[5])
			h = img.Height() * h / 100
		}
		if overlay, err = v.newThumbnail(
			blob, w, h, vips.InterestingNone, vips.SizeDown,
		); err != nil {
			return
		}
	} else {
		if overlay, err = v.newThumbnail(
			blob, v.MaxWidth, v.MaxHeight, vips.InterestingNone, vips.SizeDown,
		); err != nil {
			return
		}
	}
	AddImageRef(ctx, overlay)
	w = overlay.Width()
	h = overlay.Height()
	// alpha
	if ln >= 4 {
		alpha, _ := strconv.ParseFloat(args[3], 64)
		alpha = 1 - alpha/100
		if err = overlay.AddAlpha(); err != nil {
			return
		}
		if err = overlay.Linear([]float64{1, 1, 1, alpha}, []float64{0, 0, 0, 0}); err != nil {
			return
		}
	}
	// x y
	if ln >= 3 {
		if args[1] == "center" {
			x = (img.Width() - overlay.Width()) / 2
		} else if args[1] == imagorpath.HAlignLeft {
			x = 0
		} else if args[1] == imagorpath.HAlignRight {
			x = img.Width() - overlay.Width()
		} else if args[1] == "repeat" {
			x = 0
			repeatX = img.Width()/overlay.Width() + 1
		} else if strings.HasSuffix(args[1], "p") {
			x, _ = strconv.Atoi(strings.TrimSuffix(args[1], "p"))
			x = x * img.Width() / 100
		} else {
			x, _ = strconv.Atoi(args[1])
		}
		if args[2] == "center" {
			y = (img.Height() - overlay.Height()) / 2
		} else if args[2] == imagorpath.VAlignTop {
			y = 0
		} else if args[2] == imagorpath.VAlignBottom {
			y = img.Height() - overlay.Height()
		} else if args[2] == "repeat" {
			y = 0
			repeatY = img.Height()/overlay.Height() + 1
		} else if strings.HasSuffix(args[2], "p") {
			y, _ = strconv.Atoi(strings.TrimSuffix(args[2], "p"))
			y = y * img.Height() / 100
		} else {
			y, _ = strconv.Atoi(args[2])
		}
		if x < 0 {
			x += img.Width() - overlay.Width()
		}
		if y < 0 {
			y += img.Height() - overlay.Height()
		}
	}
	if repeatX*repeatY > 1 {
		if err = overlay.Replicate(repeatX, repeatY); err != nil {
			return
		}
	}
	if err = img.Composite(overlay, vips.BlendModeOver, x, y); err != nil {
		return
	}
	return
}

func roundCorner(ctx context.Context, img *vips.ImageRef, _ imagor.LoadFunc, args ...string) (err error) {
	var rx, ry int
	var c *vips.Color
	if len(args) == 0 {
		return
	}
	if a, e := url.QueryUnescape(args[0]); e == nil {
		args[0] = a
	}
	if len(args) == 3 {
		// rx,ry,color
		c = getColor(img, args[2])
		args = args[:2]
	}
	rx, _ = strconv.Atoi(args[0])
	ry = rx
	if len(args) > 1 {
		ry, _ = strconv.Atoi(args[1])
	}

	var rounded *vips.ImageRef
	var w = img.Width()
	var h = img.Height()
	if rounded, err = vips.NewThumbnailFromBuffer([]byte(fmt.Sprintf(`
		<svg viewBox="0 0 %d %d">
			<rect rx="%d" ry="%d" 
			 x="0" y="0" width="%d" height="%d" 
			 fill="#fff"/>
		</svg>
	`, w, h, rx, ry, w, h)), w, h, vips.InterestingNone); err != nil {
		return
	}
	AddImageRef(ctx, rounded)
	if err = img.Composite(rounded, vips.BlendModeDestIn, 0, 0); err != nil {
		return
	}
	if c != nil {
		if err = img.Flatten(c); err != nil {
			return
		}
	}
	return nil
}

func backgroundColor(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, args ...string) (err error) {
	if len(args) == 0 {
		return
	}
	if !img.HasAlpha() {
		return
	}
	return img.Flatten(getColor(img, args[0]))
}

func rotate(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, args ...string) (err error) {
	if len(args) == 0 {
		return
	}
	if angle, _ := strconv.Atoi(args[0]); angle > 0 {
		vAngle := vips.Angle0
		switch angle {
		case 90:
			vAngle = vips.Angle270
		case 180:
			vAngle = vips.Angle180
		case 270:
			vAngle = vips.Angle90
		}
		if err = img.Rotate(vAngle); err != nil {
			return err
		}
	}
	return
}

func grayscale(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, _ ...string) (err error) {
	return img.Modulate(1, 0, 0)
}

func brightness(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, args ...string) (err error) {
	if len(args) == 0 {
		return
	}
	b, _ := strconv.ParseFloat(args[0], 64)
	b = b * 255 / 100
	return linearRGB(img, []float64{1, 1, 1}, []float64{b, b, b})
}

func contrast(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, args ...string) (err error) {
	if len(args) == 0 {
		return
	}
	a, _ := strconv.ParseFloat(args[0], 64)
	a = a * 255 / 100
	a = math.Min(math.Max(a, -255), 255)
	a = (259 * (a + 255)) / (255 * (259 - a))
	b := 128 - a*128
	return linearRGB(img, []float64{a, a, a}, []float64{b, b, b})
}

func hue(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, args ...string) (err error) {
	if len(args) == 0 {
		return
	}
	h, _ := strconv.ParseFloat(args[0], 64)
	return img.Modulate(1, 1, h)
}

func saturation(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, args ...string) (err error) {
	if len(args) == 0 {
		return
	}
	s, _ := strconv.ParseFloat(args[0], 64)
	s = 1 + s/100
	return img.Modulate(1, s, 0)
}

func rgb(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, args ...string) (err error) {
	if len(args) != 3 {
		return
	}
	r, _ := strconv.ParseFloat(args[0], 64)
	g, _ := strconv.ParseFloat(args[1], 64)
	b, _ := strconv.ParseFloat(args[2], 64)
	r = r * 255 / 100
	g = g * 255 / 100
	b = b * 255 / 100
	return linearRGB(img, []float64{1, 1, 1}, []float64{r, g, b})
}

func modulate(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, args ...string) (err error) {
	if len(args) != 3 {
		return
	}
	b, _ := strconv.ParseFloat(args[0], 64)
	s, _ := strconv.ParseFloat(args[1], 64)
	h, _ := strconv.ParseFloat(args[2], 64)
	b = 1 + b/100
	s = 1 + s/100
	return img.Modulate(b, s, h)
}

func blur(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, args ...string) (err error) {
	var sigma float64
	switch len(args) {
	case 2:
		sigma, _ = strconv.ParseFloat(args[1], 64)
		break
	case 1:
		sigma, _ = strconv.ParseFloat(args[0], 64)
		break
	}
	sigma /= 2
	if sigma > 0 {
		return img.GaussianBlur(sigma)
	}
	return
}

func sharpen(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, args ...string) (err error) {
	var sigma float64
	switch len(args) {
	case 1:
		sigma, _ = strconv.ParseFloat(args[0], 64)
		break
	case 2, 3:
		sigma, _ = strconv.ParseFloat(args[1], 64)
		break
	}
	sigma = 1 + sigma*2
	return img.Sharpen(sigma, 1, 2)
}

func stripIcc(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, _ ...string) (err error) {
	return img.RemoveICCProfile()
}

func stripExif(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, _ ...string) (err error) {
	return img.RemoveICCProfile()
}

func trimFilter(_ context.Context, img *vips.ImageRef, _ imagor.LoadFunc, args ...string) error {
	var (
		ln        = len(args)
		pos       string
		tolerance int
	)
	if ln > 0 {
		tolerance, _ = strconv.Atoi(args[0])
	}
	if ln > 1 {
		pos = args[1]
	}
	return trim(img, pos, tolerance)
}

func linearRGB(img *vips.ImageRef, a, b []float64) error {
	if img.HasAlpha() {
		a = append(a, 1)
		b = append(b, 0)
	}
	return img.Linear(a, b)
}
