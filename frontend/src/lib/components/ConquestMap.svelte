<script lang="ts">
	import { resolve } from '$app/paths';
	import type { PrefectureCount } from '$lib/api/users';
	import mapImage from '$lib/assets/japan-map.png';
	import { MAP_HEIGHT, MAP_WIDTH, PREFECTURE_HITS, REGION_COLORS } from '$lib/data/prefecture-hits';

	let { prefectures }: { prefectures: PrefectureCount[] } = $props();

	let byCode = $derived(new Map(prefectures.map((p) => [p.code, p])));

	/**
	 * 押せる場所。**県名の文字の上に重ねる。**
	 *
	 * データベースに無い県コードは出さない。マスタは 47 件固定だが、
	 * 取得に失敗した状態で当たり判定だけ出すと、押しても何も起きない。
	 */
	let hits = $derived(
		PREFECTURE_HITS.flatMap((h) => {
			const prefecture = byCode.get(h.code);
			if (prefecture === undefined) return [];
			return [{ ...h, prefecture, visited: prefecture.postCount > 0 }];
		})
	);

	let visited = $derived(prefectures.filter((p) => p.postCount > 0));
	let rate = $derived(
		prefectures.length === 0 ? 0 : Math.round((visited.length / prefectures.length) * 100)
	);

	/** 地方の色を CSS へ渡す。**明度は CSS 側で決める。** */
	function tint(region: string): string {
		const c = REGION_COLORS[region];
		return c === undefined ? '--hue:210; --sat:20%;' : `--hue:${c.hue}; --sat:${c.sat}%;`;
	}

	/**
	 * 同じ内容を表でも見せるかどうか。
	 *
	 * **マップは補助であって、唯一の伝え方にはしない。**
	 * 県名は絵に焼き込まれており、読み上げには渡らない。
	 * 位置関係を頼りにできない利用者には、表が唯一の経路になる。
	 */
	let showTable = $state(false);
</script>

<section aria-labelledby="map-heading">
	<div class="head">
		<h2 id="map-heading">都道府県制覇マップ</h2>
		<!-- 率は色ではなく数と語で示す。マップを見なくても達成が分かる。 -->
		<p class="rate">{visited.length} / {prefectures.length} 県（{rate}%）</p>
	</div>

	<!--
		**絵は 1 枚の画像で、その上に県名ぶんのリンクを重ねる。**

		県ごとの領域を持たないため、訪問済みを県の形で塗ることはできない。
		代わりに県名の枠を塗り、印を添える。制覇の全体像は率と表で伝える。

		画像そのものは読み上げから外す。県名は絵に焼き込まれており、
		画像を1つの説明でまとめても中身は伝わらない。
		伝える役はリンクの aria-label と表が担う。
	-->
	<div class="viewport">
		<svg viewBox="0 0 {MAP_WIDTH} {MAP_HEIGHT}" role="group" aria-label="都道府県ごとの投稿">
			<image href={mapImage} x="0" y="0" width={MAP_WIDTH} height={MAP_HEIGHT} aria-hidden="true" />
			{#each hits as hit (hit.code)}
				{@const p = hit.prefecture}
				<a
					class="pref"
					class:visited={hit.visited}
					href={resolve('/prefectures/[code]', { code: p.code })}
					aria-label="{p.name} {p.postCount}件{hit.visited ? '（訪問済み）' : '（未訪問）'}"
				>
					<rect x={hit.x} y={hit.y} width={hit.w} height={hit.h} rx="6" />
					<!--
							**色の違いだけで訪問済みを示さない。** 塗りに加えて
							右上に印を打ち、読み上げのラベルにも語で入れる。
						-->
					{#if hit.visited}
						<circle class="dot" cx={hit.x + hit.w - 5} cy={hit.y + 5} r="4" />
					{/if}
				</a>
			{/each}
		</svg>
	</div>

	<button type="button" onclick={() => (showTable = !showTable)} aria-expanded={showTable}>
		{showTable ? '表を閉じる' : '同じ内容を表で見る'}
	</button>

	{#if showTable}
		<table>
			<caption>都道府県ごとの投稿数（行の色は地方）</caption>
			<thead>
				<tr>
					<th scope="col">都道府県</th>
					<th scope="col">地方</th>
					<th scope="col">投稿</th>
					<th scope="col">状態</th>
				</tr>
			</thead>
			<tbody>
				{#each prefectures as p (p.code)}
					<!--
						**行にも地図と同じ地方の色を薄く敷く。** 並べたときに
						どの行がどの地方かが地図と対応する。色は情報を運ばない
						（地方は「地方」の列に語で出ている）。
					-->
					<tr style={tint(p.region)}>
						<th scope="row">
							<a href={resolve('/prefectures/[code]', { code: p.code })}>{p.name}</a>
						</th>
						<td>{p.region}</td>
						<td>{p.postCount}件</td>
						<td>{p.postCount > 0 ? '訪問済み' : '未訪問'}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	{/if}
</section>

<style>
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		justify-content: space-between;
		gap: var(--space-3);
	}

	h2 {
		margin: 0;
		font-size: 1.125rem;
	}

	.rate {
		margin: 0;
		font-weight: 700;
	}

	/*
	**本文の幅から出して、画面いっぱいに見せる。**

	main は読みやすさのために `--measure`（40rem）に絞ってある。
	そのままだと地図は 608px しかもらえず、1920px の画面でも
	横スクロールになっていた。**本文の幅は文章のためのもので、
	図にまで効かせる理由が無い。**

	上限 80rem（1280px）で、県名は約 16px、当たり判定は約 38x23px。
	画面がそれより狭ければ画面幅まで縮む。**横スクロールは出ない。**

	100vw から引く 4rem は、左右のページ余白（--space-4 ×2）と
	縦スクロールバーのぶんを合わせた見込みである。
	**足りないと、ページ全体が横に溢れる。**
	*/
	.viewport {
		--map-width: min(80rem, 100vw - 4rem);
		width: var(--map-width);
		margin-top: var(--space-4);
		/* 本文の中央を基準に、左右へはみ出させる。 */
		margin-inline: calc((100% - var(--map-width)) / 2);
	}

	/*
	**地の色を敷かない。** 絵の外側は透明にしてあるので、
	ページの地色がそのまま透ける。白い箱が浮いて見えなくなる。
	*/
	svg {
		display: block;
		width: 100%;
		height: auto;
	}

	/*
	**地の色を敷かない。** 絵の外側は透明にしてあるので、
	ページの地色がそのまま透ける。白い箱が浮いて見えなくなる。

	アプリの配色は明るい側に固定してある（`tokens.css`）ので、
	テーマによる出し分けは要らない。

	この絵は県名が黒で描かれており、暗い地の上では読めない。
	暗い配色用に作り直す案は試したうえで見送った。

	  - 全体を `invert(1)`           → 緑が紫になるなど色相が壊れ、表の色と食い違う
	  - `invert(1) hue-rotate(180deg)` → 色相は保てるが塗りまで暗くなり、かえって見づらい
	  - 黒い画素だけを白にする         → 凡例の文字が色見本のすぐ横にあり、
	                                    地図上の県名と機械的に区別できなかった

	**配色を固定したのはこれが理由である。** 経緯は tokens.css にも書いた。
	*/

	/* 触れていないときは絵をそのまま見せる。 */
	.pref rect {
		fill: transparent;
		stroke: transparent;
	}

	/*
	訪問済み。**県の形ではなく県名の枠に印を付ける。**
	絵は 1 枚の画像で県ごとの領域を持たないため、これが限界である。

	**濃く塗り潰さない。** 絵の県名は黒で描かれており、上に濃い色を重ねると
	名前が読めなくなる（実際にそうなった）。薄い塗りと太い輪郭で示す。
	*/
	.visited rect {
		fill: rgb(31 107 82 / 20%);
		stroke: #1f6b52;
		stroke-width: 3;
	}

	/* **色だけに頼らないための印。** 白い縁を付けて、
	   下がどの地方の色でも見えるようにする。 */
	.dot {
		fill: #1f6b52;
		stroke: #fff;
		stroke-width: 1.5;
		pointer-events: none;
	}

	.pref:hover rect {
		fill: rgb(173 59 28 / 22%);
		stroke: #ad3b1c;
		stroke-width: 3;
	}

	/* **フォーカスが見えること。** SVG では outline の描かれ方が
	   ブラウザで揃わないため、輪郭線そのものを太くする。 */
	.pref:focus {
		outline: none;
	}

	.pref:focus-visible rect {
		fill: rgb(173 59 28 / 30%);
		stroke: var(--color-text);
		stroke-width: 4;
	}

	button {
		min-height: 2.75rem;
		margin-top: var(--space-4);
		padding: var(--space-2) var(--space-4);
		font: inherit;
		color: var(--color-text);
		background: var(--color-surface);
		border: var(--line);
		border-radius: var(--radius);
		cursor: pointer;
	}

	table {
		width: 100%;
		margin-top: var(--space-4);
		/* **collapse ではなく separate にする。** collapse では隣り合う枠線が
		   1本に畳まれる際に行の背景の上へ重なり、色の境目が濁る。
		   行間は 0 のままにするので、見た目の詰まり方は変わらない。 */
		border-collapse: separate;
		border-spacing: 0;
		font-size: 0.875rem;
	}

	/*
	行の背景。**色相と彩度は地図の凡例から来た値、明度はここで決める。**

	明度 88% は、ページの地色（#f6ecd8）との差が十分あり
	（最も近い北海道でも 35）、地方どうしも見分けられ（最小 34）、
	本文の色に対して 9.7:1 ある。95% では近畿と九州沖縄が地色に埋もれた。
	*/
	tbody tr {
		background: hsl(var(--hue) var(--sat) 88%);
	}

	caption {
		margin-bottom: var(--space-2);
		text-align: left;
		color: var(--color-text-muted);
	}

	th,
	td {
		padding: var(--space-2);
		text-align: left;
		border-bottom: var(--line);
	}

	/* separate では枠線が畳まれないため、表の下端が二重に見える。
	   最終行だけ止める。 */
	tbody tr:last-child th,
	tbody tr:last-child td {
		border-bottom: none;
	}

	thead th {
		color: var(--color-text-muted);
		font-weight: 600;
	}

	tbody th {
		font-weight: 600;
	}

	/*
	表の中のリンクは、色ではなく下線で示す。

	**主色は行の色に対してコントラストが足りない。** 実測で
	明るいテーマの近畿の行が 4.08:1、暗いテーマの中部の行が 3.35:1 で、
	どちらも 4.5:1 を下回っていた。行に色を敷いた副作用である。

	本文と同じ色にすれば 9.7:1 / 7.1:1 になる。リンクであることは
	下線で分かるので、**色を失っても何も伝わらなくならない。**
	*/
	tbody th a {
		color: inherit;
		text-decoration: underline;
	}
</style>
