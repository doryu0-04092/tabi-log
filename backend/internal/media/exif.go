package media

import "encoding/binary"

// Orientation は EXIF の向き情報（1〜8）。1 が「そのまま」。
type Orientation int

const OrientationNormal Orientation = 1

// exifOrientationTag は EXIF の Orientation タグ番号。
const exifOrientationTag = 0x0112

// readJPEGOrientation は JPEG の EXIF から向きを読む。
//
// **なぜ必要か**: 画像を再エンコードすると EXIF ごと消える。これは
// GPS 座標を落とすという目的にはかなっているが、**向きの情報も一緒に消える**。
// スマートフォンの写真は「センサーのまま保存し、向きは EXIF で伝える」ことが
// 多いため、そのまま再エンコードすると縦向きの写真が横倒しで表示される。
//
// そこで除去する前に向きだけ読み取り、画素を実際に回転させてから
// 再エンコードする。結果として EXIF が無くても正しい向きで表示される。
//
// 読み取れない場合は OrientationNormal を返す。向きが分からないことは
// エラーではなく、「回転しない」で十分に妥当な既定である。
func readJPEGOrientation(data []byte) Orientation {
	app1, ok := findEXIFSegment(data)
	if !ok {
		return OrientationNormal
	}
	return orientationFromTIFF(app1)
}

// findEXIFSegment は JPEG から EXIF（APP1）セグメントの中身を探す。
//
// JPEG は SOI(FFD8) の後にセグメントが並ぶ構造で、各セグメントは
// FF <marker> <長さ2バイト> <データ> になっている。
func findEXIFSegment(data []byte) ([]byte, bool) {
	// SOI が無ければ JPEG ではない。
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil, false
	}

	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return nil, false // セグメント境界が壊れている
		}
		marker := data[i+1]

		// SOS(FFDA) 以降は圧縮データ本体。ここより後に EXIF は無い。
		if marker == 0xDA || marker == 0xD9 {
			return nil, false
		}
		// パディングの 0xFF は読み飛ばす。
		if marker == 0xFF {
			i++
			continue
		}

		// 長さはマーカー直後の2バイト（長さ自身を含む）。
		if i+4 > len(data) {
			return nil, false
		}
		segLen := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if segLen < 2 || i+2+segLen > len(data) {
			return nil, false
		}
		body := data[i+4 : i+2+segLen]

		// APP1(FFE1) で、中身が "Exif\0\0" で始まるものが EXIF。
		if marker == 0xE1 && len(body) >= 6 && string(body[:4]) == "Exif" {
			return body[6:], true
		}

		i += 2 + segLen
	}
	return nil, false
}

// orientationFromTIFF は EXIF 本体（TIFF 構造）から Orientation タグを読む。
//
// TIFF は先頭2バイトでバイト順を示す（"II"=リトル / "MM"=ビッグ）。
// **オフセットはすべて TIFF ヘッダーの先頭からの相対値**である。
func orientationFromTIFF(tiff []byte) Orientation {
	if len(tiff) < 8 {
		return OrientationNormal
	}

	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return OrientationNormal
	}

	// 続く2バイトは常に 42。これが違えば TIFF ではない。
	if order.Uint16(tiff[2:4]) != 42 {
		return OrientationNormal
	}

	ifdOffset := int(order.Uint32(tiff[4:8]))
	if ifdOffset < 8 || ifdOffset+2 > len(tiff) {
		return OrientationNormal
	}

	entryCount := int(order.Uint16(tiff[ifdOffset : ifdOffset+2]))
	entries := tiff[ifdOffset+2:]

	const entrySize = 12
	for n := range entryCount {
		off := n * entrySize
		if off+entrySize > len(entries) {
			return OrientationNormal
		}
		e := entries[off : off+entrySize]

		if order.Uint16(e[0:2]) != exifOrientationTag {
			continue
		}
		// 型 3（SHORT）以外は想定しない。値は値領域の先頭2バイトに入る。
		if order.Uint16(e[2:4]) != 3 {
			return OrientationNormal
		}
		v := Orientation(order.Uint16(e[8:10]))
		if v < 1 || v > 8 {
			return OrientationNormal
		}
		return v
	}
	return OrientationNormal
}
