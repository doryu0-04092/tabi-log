// 都道府県制覇マップのマス配置。
//
// **見た目の調整をこのファイルだけで行えるように切り出している。**
// 行と列を動かしたくなるのは配置が気に入らないときであり、
// そのたびにコンポーネントを触るのは筋が悪い。
//
// **地理的に正確な地図ではなく、同じ大きさのマスを並べる。** 理由は3つ
// （features.md 8章）。
//
//   1. ライセンスの問題が構造的に消える。そのまま使える日本地図 SVG は
//      コピーレフトのものしか見つからず、地理データからの変換工程を
//      維持するのも避けたい
//   2. 実際の地図では香川・大阪・東京のタップ領域が数ピクセルしかなく、
//      北海道との差が極端になる。**均一なマスなら全県が同じ操作しやすさになる**
//   3. 見たいのは「どこを制覇したか」であり、県境の正確な形ではない
//
// 代償として、「日本地図が塗られていく」という見た目の訴求は実地図より弱い。
//
// 座標は1始まり。row が南へ、column が東へ増える。

export type PrefectureTile = {
	/** JIS X 0401 の都道府県コード。 */
	code: string;
	row: number;
	column: number;
};

/** グリッドの大きさ。CSS の grid-template に渡す。 */
export const TILE_ROWS = 12;
export const TILE_COLUMNS = 11;

/**
 * 47件のマス配置。
 *
 * おおよその位置関係が分かればよく、形は再現しない。
 * 隣り合う県が隣り合うマスになることを優先している。
 */
export const PREFECTURE_TILES: PrefectureTile[] = [
	// 北海道・東北
	{ code: '01', row: 1, column: 10 }, // 北海道
	{ code: '02', row: 2, column: 9 }, // 青森
	{ code: '05', row: 3, column: 8 }, // 秋田
	{ code: '03', row: 3, column: 9 }, // 岩手
	{ code: '06', row: 4, column: 8 }, // 山形
	{ code: '04', row: 4, column: 9 }, // 宮城

	// 北陸・甲信越・関東
	{ code: '17', row: 5, column: 6 }, // 石川
	{ code: '16', row: 5, column: 7 }, // 富山
	{ code: '15', row: 5, column: 8 }, // 新潟
	{ code: '07', row: 5, column: 9 }, // 福島
	{ code: '18', row: 6, column: 6 }, // 福井
	{ code: '21', row: 6, column: 7 }, // 岐阜
	{ code: '20', row: 6, column: 8 }, // 長野
	{ code: '10', row: 6, column: 9 }, // 群馬
	{ code: '09', row: 6, column: 10 }, // 栃木
	{ code: '08', row: 6, column: 11 }, // 茨城

	// 中国・近畿・東海・関東南部
	{ code: '32', row: 7, column: 1 }, // 島根
	{ code: '31', row: 7, column: 2 }, // 鳥取
	{ code: '28', row: 7, column: 3 }, // 兵庫
	{ code: '26', row: 7, column: 4 }, // 京都
	{ code: '25', row: 7, column: 5 }, // 滋賀
	{ code: '24', row: 7, column: 6 }, // 三重
	{ code: '23', row: 7, column: 7 }, // 愛知
	{ code: '19', row: 7, column: 8 }, // 山梨
	{ code: '11', row: 7, column: 9 }, // 埼玉
	{ code: '13', row: 7, column: 10 }, // 東京
	{ code: '12', row: 7, column: 11 }, // 千葉

	{ code: '35', row: 8, column: 1 }, // 山口
	{ code: '34', row: 8, column: 2 }, // 広島
	{ code: '33', row: 8, column: 3 }, // 岡山
	{ code: '27', row: 8, column: 4 }, // 大阪
	{ code: '29', row: 8, column: 5 }, // 奈良
	{ code: '22', row: 8, column: 8 }, // 静岡
	{ code: '14', row: 8, column: 10 }, // 神奈川

	// 九州・四国
	{ code: '40', row: 9, column: 1 }, // 福岡
	{ code: '44', row: 9, column: 2 }, // 大分
	{ code: '38', row: 9, column: 3 }, // 愛媛
	{ code: '37', row: 9, column: 4 }, // 香川
	{ code: '30', row: 9, column: 5 }, // 和歌山

	{ code: '41', row: 10, column: 1 }, // 佐賀
	{ code: '43', row: 10, column: 2 }, // 熊本
	{ code: '39', row: 10, column: 3 }, // 高知
	{ code: '36', row: 10, column: 4 }, // 徳島

	{ code: '42', row: 11, column: 1 }, // 長崎
	{ code: '45', row: 11, column: 2 }, // 宮崎

	{ code: '47', row: 12, column: 1 }, // 沖縄
	{ code: '46', row: 12, column: 2 } // 鹿児島
];

/** 地方ごとの色相。訪問済み／未訪問は明度で分ける。 */
export const REGION_HUES: Record<string, number> = {
	北海道: 200,
	東北: 220,
	関東: 260,
	中部: 160,
	近畿: 30,
	中国: 350,
	四国: 120,
	九州沖縄: 300
};
