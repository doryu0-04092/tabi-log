package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// makeJPEG は指定した寸法の JPEG を作る。左上だけ色を変え、
// 回転したかどうかを判別できるようにする。
func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 20, G: 20, B: 20, A: 255})
		}
	}
	// 左上の目印。
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("JPEG の作成に失敗した: %v", err)
	}
	return buf.Bytes()
}

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("PNG の作成に失敗した: %v", err)
	}
	return buf.Bytes()
}

// withEXIFOrientation は JPEG に Orientation だけを持つ EXIF(APP1) を挿入する。
//
// 実機の写真を用意せずに向きの扱いを検証するため、
// 最小限の TIFF 構造を組み立てる。
func withEXIFOrientation(t *testing.T, jpegData []byte, orientation uint16) []byte {
	t.Helper()
	if len(jpegData) < 2 || jpegData[0] != 0xFF || jpegData[1] != 0xD8 {
		t.Fatal("SOI で始まっていない")
	}

	// TIFF ヘッダー（ビッグエンディアン）＋ IFD0（エントリ1件）。
	var tiff bytes.Buffer
	tiff.WriteString("MM")                                    // バイト順
	_ = binary.Write(&tiff, binary.BigEndian, uint16(42))     // 識別子
	_ = binary.Write(&tiff, binary.BigEndian, uint32(8))      // IFD0 へのオフセット
	_ = binary.Write(&tiff, binary.BigEndian, uint16(1))      // エントリ数
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0x0112)) // タグ: Orientation
	_ = binary.Write(&tiff, binary.BigEndian, uint16(3))      // 型: SHORT
	_ = binary.Write(&tiff, binary.BigEndian, uint32(1))      // 個数
	_ = binary.Write(&tiff, binary.BigEndian, orientation)    // 値
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0))      // 値領域の余り
	_ = binary.Write(&tiff, binary.BigEndian, uint32(0))      // 次の IFD なし

	body := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	segLen := len(body) + 2

	out := make([]byte, 0, len(jpegData)+segLen+2)
	out = append(out, 0xFF, 0xD8) // SOI
	out = append(out, 0xFF, 0xE1) // APP1
	out = binary.BigEndian.AppendUint16(out, uint16(segLen))
	out = append(out, body...)
	out = append(out, jpegData[2:]...) // 元の SOI 以降
	return out
}

// ---------------------------------------------------------------------------
// 形式の判定
// ---------------------------------------------------------------------------

func TestProcess_対応形式を受け入れる(t *testing.T) {
	for _, tt := range []struct {
		name string
		data []byte
		mime string
	}{
		{"JPEG", makeJPEG(t, 100, 80), "image/jpeg"},
		{"PNG", makePNG(t, 100, 80), "image/png"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Process(tt.data)
			if err != nil {
				t.Fatalf("処理に失敗した: %v", err)
			}
			if got.Mime != tt.mime {
				t.Errorf("Mime: 期待 %q, 実際 %q", tt.mime, got.Mime)
			}
		})
	}
}

// **拡張子や申告された Content-Type ではなく、中身で判定すること。**
// 実行可能ファイルに .jpg と名前を付けて image/jpeg と申告するのは誰にでもできる。
func TestProcess_画像でないものを拒否する(t *testing.T) {
	cases := map[string][]byte{
		"実行可能ファイル":    {0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0x00, 0x00}, // MZ ヘッダー
		"ELF":         {0x7F, 'E', 'L', 'F', 2, 1, 1, 0},
		"ZIP":         {'P', 'K', 0x03, 0x04, 0, 0, 0, 0},
		"ただのテキスト":     []byte("これは画像ではありません。ただの文章です。"),
		"JPEGのふりをした空": {0xFF, 0xD8},
	}

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Process(data)
			if err == nil {
				t.Fatal("画像でないものが受け入れられた")
			}
			if !errors.Is(err, ErrUnsupportedFormat) && !errors.Is(err, ErrDecode) {
				t.Errorf("想定外のエラー: %v", err)
			}
		})
	}
}

func TestProcess_中身が空なら拒否する(t *testing.T) {
	if _, err := Process(nil); !errors.Is(err, ErrDecode) {
		t.Fatalf("期待 ErrDecode, 実際 %v", err)
	}
}

// ---------------------------------------------------------------------------
// EXIF の除去 — このプロジェクトで最も重要な検証
// ---------------------------------------------------------------------------

// **変換物に EXIF が残っていないこと。**
//
// 位置情報を都道府県までに絞る設計をしていても、画像の EXIF に
// GPS 座標が残っていれば自宅や訪問先が特定されうる。
// 旅行の記録は行動履歴そのものであり、ここが漏れると配慮が無意味になる。
func TestProcess_変換物にEXIFが残らない(t *testing.T) {
	withExif := withEXIFOrientation(t, makeJPEG(t, 200, 100), 1)

	// 前提の確認: 入力には EXIF がある。
	if _, ok := findEXIFSegment(withExif); !ok {
		t.Fatal("テストの前提が壊れている: 入力に EXIF が無い")
	}

	got, err := Process(withExif)
	if err != nil {
		t.Fatalf("処理に失敗した: %v", err)
	}

	for _, v := range got.Variants {
		if _, ok := findEXIFSegment(v.Data); ok {
			t.Errorf("%s に EXIF が残っている", v.Kind)
		}
		// APP1 セグメント自体が存在しないことも確認する。
		if bytes.Contains(v.Data, []byte("Exif\x00\x00")) {
			t.Errorf("%s に Exif マーカーが残っている", v.Kind)
		}
	}
}

// EXIF を消すと向きの情報も消えるため、消す前に画素を回転させておく必要がある。
// これをしないと、スマートフォンで撮った縦向きの写真が横倒しで表示される。
func TestProcess_向きに従って画素を回転する(t *testing.T) {
	// 横長（200x100）の画像に「時計回りに90度」を指示する。
	// 回転後は縦長（100x200）になるはず。
	rotated := withEXIFOrientation(t, makeJPEG(t, 200, 100), 6)

	got, err := Process(rotated)
	if err != nil {
		t.Fatalf("処理に失敗した: %v", err)
	}

	if got.Width != 100 || got.Height != 200 {
		t.Errorf("回転後の寸法: 期待 100x200, 実際 %dx%d", got.Width, got.Height)
	}
}

func TestProcess_向きが1なら回転しない(t *testing.T) {
	normal := withEXIFOrientation(t, makeJPEG(t, 200, 100), 1)

	got, err := Process(normal)
	if err != nil {
		t.Fatalf("処理に失敗した: %v", err)
	}
	if got.Width != 200 || got.Height != 100 {
		t.Errorf("寸法: 期待 200x100, 実際 %dx%d", got.Width, got.Height)
	}
}

func TestReadJPEGOrientation(t *testing.T) {
	base := makeJPEG(t, 10, 10)

	for _, o := range []uint16{1, 2, 3, 4, 5, 6, 7, 8} {
		if got := readJPEGOrientation(withEXIFOrientation(t, base, o)); got != Orientation(o) {
			t.Errorf("Orientation %d: 実際 %d", o, got)
		}
	}

	// EXIF が無い場合と壊れている場合は、既定（回転しない）に倒す。
	if got := readJPEGOrientation(base); got != OrientationNormal {
		t.Errorf("EXIF 無し: 期待 1, 実際 %d", got)
	}
	if got := readJPEGOrientation([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00}); got != OrientationNormal {
		t.Errorf("壊れた EXIF: 期待 1, 実際 %d", got)
	}
	// 範囲外の値も既定に倒す。
	if got := readJPEGOrientation(withEXIFOrientation(t, base, 99)); got != OrientationNormal {
		t.Errorf("範囲外の値: 期待 1, 実際 %d", got)
	}
}

// ---------------------------------------------------------------------------
// 変換物
// ---------------------------------------------------------------------------

func TestProcess_長辺を上限まで縮小する(t *testing.T) {
	got, err := Process(makeJPEG(t, 2000, 1000))
	if err != nil {
		t.Fatalf("処理に失敗した: %v", err)
	}

	byKind := map[string]Variant{}
	for _, v := range got.Variants {
		byKind[v.Kind] = v
	}

	if v := byKind["thumb"]; v.Width != ThumbMaxEdge || v.Height != ThumbMaxEdge/2 {
		t.Errorf("thumb: 期待 %dx%d, 実際 %dx%d", ThumbMaxEdge, ThumbMaxEdge/2, v.Width, v.Height)
	}
	if v := byKind["medium"]; v.Width != MediumMaxEdge || v.Height != MediumMaxEdge/2 {
		t.Errorf("medium: 期待 %dx%d, 実際 %dx%d", MediumMaxEdge, MediumMaxEdge/2, v.Width, v.Height)
	}
}

// 小さい画像を引き伸ばしても情報は増えず、ファイルだけが大きくなる。
func TestProcess_元より大きくは拡大しない(t *testing.T) {
	got, err := Process(makeJPEG(t, 120, 90))
	if err != nil {
		t.Fatalf("処理に失敗した: %v", err)
	}

	for _, v := range got.Variants {
		if v.Width > 120 || v.Height > 90 {
			t.Errorf("%s が元より大きい: %dx%d", v.Kind, v.Width, v.Height)
		}
	}
}

func TestProcess_変換物が読み取れるJPEGである(t *testing.T) {
	got, err := Process(makePNG(t, 400, 300))
	if err != nil {
		t.Fatalf("処理に失敗した: %v", err)
	}

	for _, v := range got.Variants {
		// PNG を入力しても変換物は JPEG になる（webp の書き出しが標準に無いため統一）。
		cfg, format, err := image.DecodeConfig(bytes.NewReader(v.Data))
		if err != nil {
			t.Fatalf("%s を読み取れない: %v", v.Kind, err)
		}
		if format != "jpeg" {
			t.Errorf("%s の形式: 期待 jpeg, 実際 %s", v.Kind, format)
		}
		if cfg.Width != v.Width || cfg.Height != v.Height {
			t.Errorf("%s の寸法が報告と食い違う: 報告 %dx%d, 実際 %dx%d",
				v.Kind, v.Width, v.Height, cfg.Width, cfg.Height)
		}
	}
}

func TestFitWithin(t *testing.T) {
	tests := []struct {
		name          string
		w, h, maxEdge int
		wantW, wantH  int
	}{
		{"上限以下はそのまま", 100, 50, 320, 100, 50},
		{"横長を縮小", 2000, 1000, 320, 320, 160},
		{"縦長を縮小", 1000, 2000, 320, 160, 320},
		{"正方形", 1000, 1000, 320, 320, 320},
		{"極端な縦横比でも0にしない", 10000, 5, 320, 320, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := fitWithin(tt.w, tt.h, tt.maxEdge)
			if w != tt.wantW || h != tt.wantH {
				t.Errorf("期待 %dx%d, 実際 %dx%d", tt.wantW, tt.wantH, w, h)
			}
		})
	}
}
