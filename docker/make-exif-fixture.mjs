// 検証用に、GPS 座標入りの JPEG を作る。
//
// **画像処理が EXIF を落としていることを確かめるために使う。**
// 落ちたことを言うには、落ちる前に確かに入っていた必要がある。
//
// exiftool や PIL が無くても作れるよう、EXIF の APP1 セグメントを自前で組み立てる。
// **中身を自分で決めておくと、「消えた」ことを確実に判定できる。**
//
// 使い方:
//   node docker/make-exif-fixture.mjs <出力先ディレクトリ>
//
// 出力:
//   <出力先>/gps.jpg   GPS 入り（アップロードに使う）
//
// 前提: ffmpeg（土台の JPEG を作るのに使う）
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

const outDir = process.argv[2] || '.';
fs.mkdirSync(outDir, { recursive: true });

const base = path.join(outDir, 'base.jpg');
const out = path.join(outDir, 'gps.jpg');

// 土台。中身は何でもよいが、**変換で縮むことを確かめたいので大きめにする。**
execFileSync('ffmpeg', [
  '-y', '-f', 'lavfi', '-i', 'color=c=steelblue:s=1600x1200',
  '-frames:v', '1', '-q:v', '3', base,
], { stdio: 'ignore' });

const src = fs.readFileSync(base);

// --- EXIF（APP1）を組み立てる ------------------------------------------------
//
// TIFF ヘッダ → IFD0（GPS IFD へのポインタ1件）→ GPS IFD（4件）→ 実データ。
// オフセットはすべて TIFF ヘッダの先頭からの相対位置である。
const tiff = Buffer.alloc(128);
tiff.write('II', 0); // リトルエンディアン
tiff.writeUInt16LE(0x002a, 2);
tiff.writeUInt32LE(8, 4); // IFD0 の位置

tiff.writeUInt16LE(1, 8); // IFD0 は1件だけ
tiff.writeUInt16LE(0x8825, 10); // GPSInfoIFDPointer
tiff.writeUInt16LE(4, 12); // LONG
tiff.writeUInt32LE(1, 14);
tiff.writeUInt32LE(26, 18); // GPS IFD の位置
tiff.writeUInt32LE(0, 22); // 次の IFD は無い

tiff.writeUInt16LE(4, 26); // GPS IFD は4件

const entry = (off, tag, type, count, value, inline) => {
  tiff.writeUInt16LE(tag, off);
  tiff.writeUInt16LE(type, off + 2);
  tiff.writeUInt32LE(count, off + 4);
  if (inline) tiff.write(value, off + 8, 'ascii');
  else tiff.writeUInt32LE(value, off + 8);
};
entry(28, 0x0001, 2, 2, 'N\0', true); // 緯度の南北
entry(40, 0x0002, 5, 3, 80, false); // 緯度（RATIONAL×3）
entry(52, 0x0003, 2, 2, 'E\0', true); // 経度の東西
entry(64, 0x0004, 5, 3, 104, false); // 経度（RATIONAL×3）
tiff.writeUInt32LE(0, 76);

const rational = (off, num, den) => {
  tiff.writeUInt32LE(num, off);
  tiff.writeUInt32LE(den, off + 4);
};
// 東京タワー付近。**実在する座標にしておく。** ゼロだと「除去された」のか
// 「もともと無かった」のか区別しにくい。
rational(80, 35, 1);
rational(88, 39, 1);
rational(96, 2229, 100);
rational(104, 139, 1);
rational(112, 44, 1);
rational(120, 5157, 100);

const body = Buffer.concat([Buffer.from('Exif\0\0', 'ascii'), tiff]);
const app1 = Buffer.alloc(4 + body.length);
app1.writeUInt16BE(0xffe1, 0);
app1.writeUInt16BE(body.length + 2, 2);
body.copy(app1, 4);

// SOI（FFD8）の直後に差し込む。
fs.writeFileSync(out, Buffer.concat([src.subarray(0, 2), app1, src.subarray(2)]));
fs.unlinkSync(base);

const made = fs.readFileSync(out);
const hasExif = made.includes(Buffer.from('Exif\0\0', 'ascii'));
const hasGps = made.includes(Buffer.from([0x25, 0x88, 0x04, 0x00]));

console.log('できました: ' + out + ' (' + made.length + ' bytes, 1600x1200)');
console.log('  Exif マーカー : ' + (hasExif ? 'あり' : 'なし'));
console.log('  GPS タグ      : ' + (hasGps ? 'あり' : 'なし'));

if (!hasExif || !hasGps) {
  console.error('**入っているはずのものが入っていない。** これでは検証にならない。');
  process.exit(1);
}

console.log('');
console.log('配信された画像に対して同じ判定をして、両方 "なし" になれば除去できている。');
