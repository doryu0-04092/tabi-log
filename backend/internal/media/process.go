// Package media はアップロードされた画像を検証し、表示用に変換する。
//
// この処理は S3 のイベントで起動する Lambda から呼ばれる。バックエンドは
// 画像を経由させない設計（ブラウザから S3 へ直接送る）のため、
// **中身を見る唯一の場所がここになる**。
//
// 中心にあるのは3つの責務である。
//   - 申告された種類ではなく、実際のバイト列で形式を判定する
//   - **EXIF（GPS座標・撮影日時・端末情報）を除去する**
//   - 表示用の変換物を作り、原本を配らないようにする
//
// 画像の入出力しか行わず、S3 にもデータベースにも依存しない。
// 変換の正しさをテストするのに何も立ち上げずに済む。
package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"

	xdraw "golang.org/x/image/draw"

	// デコーダを登録する。webp は読み取りのみ対応（書き出しは標準に無い）。
	_ "golang.org/x/image/webp"
	_ "image/png"
)

// 受け付ける画像形式。**申告値ではなくバイト列から判定した結果**を照合する。
var allowedFormats = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

// 変換物の長辺の画素数。
const (
	ThumbMaxEdge  = 320
	MediumMaxEdge = 1080
)

// maxPixels は展開後の画素数の上限。
//
// **圧縮率の高い画像は、ファイルが小さくても展開すると巨大になる**
// （いわゆる圧縮爆弾）。ファイルサイズの上限だけでは防げないため、
// 寸法でも上限を設ける。5000万画素は 8660x5773 相当で、
// 通常の写真には十分な余裕がある。
const maxPixels = 50_000_000

// jpegQuality は変換物の画質。
const jpegQuality = 82

// 処理が失敗する理由。呼び出し側は errors.Is で判別する。
var (
	ErrUnsupportedFormat = errors.New("対応していない画像形式である")
	ErrTooLarge          = errors.New("画像の寸法が大きすぎる")
	ErrDecode            = errors.New("画像を読み取れない")
)

// Variant は変換後の1つの画像。
type Variant struct {
	Kind   string // thumb / medium
	Data   []byte
	Width  int
	Height int
}

// Result は処理の結果。
type Result struct {
	// Mime は**バイト列から判定した**実際の形式。申告値ではない。
	Mime string
	// Width / Height は向きを補正したあとの寸法。
	Width  int
	Height int
	// Variants は表示用の変換物。いずれも JPEG で、EXIF を持たない。
	Variants []Variant
}

// Process は画像を検証し、表示用の変換物を作る。
func Process(data []byte) (*Result, error) {
	mime, err := detectFormat(data)
	if err != nil {
		return nil, err
	}

	// 向きは**デコードして再エンコードする前に**読む。
	// 再エンコードすると EXIF ごと消えるためである。
	orientation := OrientationNormal
	if mime == "image/jpeg" {
		orientation = readJPEGOrientation(data)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecode, err)
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, fmt.Errorf("%w: %d x %d", ErrTooLarge, cfg.Width, cfg.Height)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecode, err)
	}

	// ここで画素を実際に回転させる。以降、向きの情報は不要になる。
	img = applyOrientation(img, orientation)
	bounds := img.Bounds()

	variants := make([]Variant, 0, 2)
	for _, spec := range []struct {
		kind    string
		maxEdge int
	}{
		{"thumb", ThumbMaxEdge},
		{"medium", MediumMaxEdge},
	} {
		v, err := makeVariant(img, spec.kind, spec.maxEdge)
		if err != nil {
			return nil, err
		}
		variants = append(variants, v)
	}

	return &Result{
		Mime:     mime,
		Width:    bounds.Dx(),
		Height:   bounds.Dy(),
		Variants: variants,
	}, nil
}

// detectFormat はバイト列の先頭から実際の形式を判定する。
//
// **拡張子も、クライアントが申告した Content-Type も信用しない。**
// 実行可能ファイルに .jpg という名前を付けて image/jpeg と申告することは
// 誰にでもできる。中身を見る以外に確かめる方法はない。
func detectFormat(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("%w: 中身が空である", ErrDecode)
	}

	// http.DetectContentType は先頭512バイトの並びから種類を判定する。
	// 判定規則は標準ライブラリが持っているため、自前で書かない。
	mime := http.DetectContentType(data)
	if _, ok := allowedFormats[mime]; !ok {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedFormat, mime)
	}
	return mime, nil
}

// makeVariant は長辺が maxEdge を超えないように縮小した JPEG を作る。
//
// 元より大きくは拡大しない。小さい画像を引き伸ばしても情報は増えず、
// ファイルだけが大きくなる。
func makeVariant(src image.Image, kind string, maxEdge int) (Variant, error) {
	w, h := fitWithin(src.Bounds().Dx(), src.Bounds().Dy(), maxEdge)

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// CatmullRom は縮小時の品質が良い。速度より仕上がりを優先する。
	// 1枚あたりの処理は Lambda で完結し、利用者を待たせない。
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return Variant{}, fmt.Errorf("%s の書き出しに失敗した: %w", kind, err)
	}

	return Variant{Kind: kind, Data: buf.Bytes(), Width: w, Height: h}, nil
}

// fitWithin は縦横比を保ったまま長辺を maxEdge 以内に収める寸法を返す。
func fitWithin(w, h, maxEdge int) (int, int) {
	if w <= maxEdge && h <= maxEdge {
		return w, h
	}
	if w >= h {
		return maxEdge, max(1, h*maxEdge/w)
	}
	return max(1, w*maxEdge/h), maxEdge
}

// applyOrientation は EXIF の向きに従って画素を並べ替える。
//
// 5〜8 は縦横が入れ替わる（転置を伴う）ため、出力の寸法も変わる。
func applyOrientation(src image.Image, o Orientation) image.Image {
	if o <= OrientationNormal || o > 8 {
		return src
	}

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	// 転置を伴う向きでは縦横が入れ替わる。
	outW, outH := w, h
	if o >= 5 {
		outW, outH = h, w
	}

	dst := image.NewRGBA(image.Rect(0, 0, outW, outH))
	for y := range h {
		for x := range w {
			var nx, ny int
			switch o {
			case 2: // 左右反転
				nx, ny = w-1-x, y
			case 3: // 180度回転
				nx, ny = w-1-x, h-1-y
			case 4: // 上下反転
				nx, ny = x, h-1-y
			case 5: // 転置
				nx, ny = y, x
			case 6: // 時計回りに90度
				nx, ny = h-1-y, x
			case 7: // 反転転置
				nx, ny = h-1-y, w-1-x
			case 8: // 反時計回りに90度
				nx, ny = y, w-1-x
			default:
				nx, ny = x, y
			}
			dst.Set(nx, ny, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
