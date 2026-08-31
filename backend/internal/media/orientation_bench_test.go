package media

import (
	"fmt"
	"image"
	"image/color"
	"testing"
	"time"
)

/*
向き補正にかかる時間を測る。

監査（2026-08-31、H2）で「上限いっぱいの画像だと Lambda の 60 秒 timeout を
超えうる」と推測のまま残していた。**実測して事実にする。**

Lambda は memory_size 1024MB / timeout 60 秒（infra/lambda.tf）。
上限は 5000 万画素（maxPixels）で、8660x5773 相当である。

applyOrientation は image.Image の At と draw 先の Set を画素ごとに呼ぶ。
どちらもインターフェース越しなので、画素数に比例して重くなる。

ベンチマークは `go test` の既定では走らない。CI を重くせずに、
必要なときだけ測れるようにしてある。

	go test -run XXX -bench BenchmarkApplyOrientation -benchtime 1x ./internal/media/
*/

// makeImage は指定した画素数の画像を作る。**内容は速度に影響しない**ので単色でよい。
func makeImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	return img
}

func BenchmarkApplyOrientation(b *testing.B) {
	// 上限（5000万画素）と、その手前の代表的な大きさ。
	sizes := []struct {
		name string
		w, h int
	}{
		{"12M画素_4000x3000_一般的なスマホ", 4000, 3000},
		{"24M画素_6000x4000_一眼", 6000, 4000},
		{"50M画素_8660x5773_上限いっぱい", 8660, 5773},
	}

	// 6（時計回りに90度）は転置を伴う。**最も重い側を測る。**
	for _, s := range sizes {
		b.Run(s.name, func(b *testing.B) {
			src := makeImage(s.w, s.h)
			b.ResetTimer()
			for b.Loop() {
				_ = applyOrientation(src, 6)
			}
		})
	}
}

/*
上限いっぱいの画像でも Lambda の timeout に収まること。

**ベンチマークは CI で走らないので、上限の確認はテストとして置く。**
ただし毎回 5000 万画素を回すと CI が重くなるため、
12M 画素（一般的なスマホの写真）で測り、画素数に比例するとして換算する。

比例するという前提は、実装が画素ごとの走査1回であることに基づく。
**実装が変わったらこの前提も崩れる**ので、そのときはここも見直すこと。
*/
func Test向き補正が制限時間に収まる(t *testing.T) {
	if testing.Short() {
		t.Skip("-short のため実行しない")
	}

	const (
		w, h        = 4000, 3000
		limitPixels = maxPixels
		// Lambda の timeout（60秒）のうち、向き補正に使ってよい割合。
		// 復号・変換物の生成・S3 の読み書きにも時間が要る。
		budget = 20 * time.Second
	)

	src := makeImage(w, h)
	start := time.Now()
	_ = applyOrientation(src, 6)
	elapsed := time.Since(start)

	pixels := float64(w * h)
	perPixel := float64(elapsed) / pixels
	atLimit := time.Duration(perPixel * float64(limitPixels))

	t.Logf("%dx%d（%.0f万画素）で %v / 1画素あたり %.1fns", w, h, pixels/10000, elapsed, perPixel)
	t.Logf("上限 %d 画素に換算すると %v（許容 %v）", limitPixels, atLimit.Round(time.Millisecond), budget)

	if atLimit > budget {
		t.Errorf("向き補正が遅すぎる。上限いっぱいの画像で %v かかる見込みで、"+
			"Lambda の timeout 60 秒に対して余裕が無い\n"+
			"%s", atLimit.Round(time.Millisecond), hint)
	}
}

const hint = `画素ごとに At と Set を呼んでいるのが原因である。
入力が *image.RGBA なら Pix を直接添字で読み書きできる（インターフェース越しの
呼び出しが消える）。それでも足りなければ、変換物だけ先に作って原本の
向き補正を諦めるか、Lambda の memory_size を上げて CPU を増やす
（Lambda の CPU は memory_size に比例する）。`

// opaque は *image.RGBA であることを隠す包み。**遅い経路を通すために使う。**
type opaque struct{ img image.Image }

func (o opaque) ColorModel() color.Model { return o.img.ColorModel() }
func (o opaque) Bounds() image.Rectangle { return o.img.Bounds() }
func (o opaque) At(x, y int) color.Color { return o.img.At(x, y) }

/*
速い経路と遅い経路が同じ結果を出すこと。

**2つの経路に分けた以上、片方だけ直る／壊れる余地ができた。**
向き 1〜8 のすべてで、1画素ずつ突き合わせる。
*/
func Test向き補正は経路によらず同じ結果になる(t *testing.T) {
	// 縦横を変えておく。**正方形だと転置の誤りが見えない。**
	src := makeImage(23, 17)

	for o := Orientation(1); o <= 8; o++ {
		t.Run(fmt.Sprintf("向き%d", o), func(t *testing.T) {
			fast := applyOrientation(src, o)
			slow := applyOrientation(opaque{src}, o)

			if fast.Bounds() != slow.Bounds() {
				t.Fatalf("寸法が違う: 速い %v / 遅い %v", fast.Bounds(), slow.Bounds())
			}
			b := fast.Bounds()
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					if fast.At(x, y) != slow.At(x, y) {
						t.Fatalf("(%d,%d) が違う: 速い %v / 遅い %v",
							x, y, fast.At(x, y), slow.At(x, y))
					}
				}
			}
		})
	}
}
