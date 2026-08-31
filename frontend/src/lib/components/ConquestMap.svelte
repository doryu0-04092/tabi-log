<script lang="ts">
	import { resolve } from '$app/paths';
	import type { PrefectureCount } from '$lib/api/users';
	import {
		PREFECTURE_TILES,
		REGION_HUES,
		TILE_GAP,
		TILE_UNIT_X,
		TILE_UNIT_Y
	} from '$lib/data/prefecture-tiles';

	let { prefectures }: { prefectures: PrefectureCount[] } = $props();

	let byCode = $derived(new Map(prefectures.map((p) => [p.code, p])));

	/** 角丸の半径。参考図に近い丸みにしている。 */
	const RADIUS = 8;

	/** 図の余白。突起と輪郭線が切れないだけの幅を取る。 */
	const PAD = 10;

	/**
	 * マスに書く名前。**「都」「府」「県」を落とす。**
	 *
	 * 枠の幅は2文字を基準にしている。接尾辞を付けると全県が3文字以上になり、
	 * 文字が枠に合わせて縮んで読めなくなる。読み上げ用の aria-label には
	 * 正式名称をそのまま使うので、情報は落ちない。
	 *
	 * 北海道は「道」で終わるが3文字で1つの名前なので落とさない。
	 */
	function shortName(name: string): string {
		return name.length > 2 ? name.replace(/[都府県]$/, '') : name;
	}

	/**
	 * 描画用のマス。
	 *
	 * 配置データの「マス単位」を px に直し、文字の大きさまでここで決める。
	 * **テンプレート側に計算を置かない** — 47件ぶん式が並ぶと読めなくなる。
	 */
	let tiles = $derived(
		PREFECTURE_TILES.flatMap((t) => {
			const prefecture = byCode.get(t.code);
			if (prefecture === undefined) return [];

			const w = (t.width ?? 1) * TILE_UNIT_X - TILE_GAP;
			const h = (t.height ?? 1) * TILE_UNIT_Y - TILE_GAP;
			const label = shortName(prefecture.name);

			return [
				{
					code: t.code,
					prefecture,
					visited: prefecture.postCount > 0,
					hue: REGION_HUES[prefecture.region] ?? 210,
					x: t.col * TILE_UNIT_X,
					y: t.row * TILE_UNIT_Y,
					w,
					h,
					// **枠に収まる大きさにする。** 高さから決めた値と、
					// 文字数で割った幅の小さいほうを取る。3文字の県が潰れない。
					label,
					fontSize: Math.min(h * 0.52, (w / label.length) * 1.05),
					tail: t.tail === true
				}
			];
		})
	);

	/** 図の表示範囲。マスの位置から求めるので、配置を変えても追従する。 */
	let viewBox = $derived.by(() => {
		if (tiles.length === 0) return '0 0 1 1';
		const minX = Math.min(...tiles.map((t) => t.x));
		const minY = Math.min(...tiles.map((t) => t.y));
		const maxX = Math.max(...tiles.map((t) => t.x + t.w));
		// 北海道の突起は枠の下へ出るぶんを足す。
		const maxY = Math.max(...tiles.map((t) => t.y + t.h + (t.tail ? TILE_UNIT_Y * 0.42 : 0)));
		return `${minX - PAD} ${minY - PAD} ${maxX - minX + PAD * 2} ${maxY - minY + PAD * 2}`;
	});

	let visited = $derived(prefectures.filter((p) => p.postCount > 0));
	let rate = $derived(
		prefectures.length === 0 ? 0 : Math.round((visited.length / prefectures.length) * 100)
	);

	/**
	 * 同じ内容を表でも見せるかどうか。
	 *
	 * **マップは補助であって、唯一の伝え方にはしない。** 位置関係を
	 * 頼りにできない利用者にも、県名と件数の一覧で同じ情報が届く必要がある。
	 */
	let showTable = $state(false);
</script>

<section aria-labelledby="map-heading">
	<div class="head">
		<h2 id="map-heading">都道府県制覇マップ</h2>
		<!--
			率は色ではなく数と語で示す。マップを見なくても達成が分かる。
		-->
		<p class="rate">{visited.length} / {prefectures.length} 県（{rate}%）</p>
	</div>

	<!--
		**マスはリンクにする。** その県の投稿一覧へ行けることが目的であり、
		キーボードでも順に辿れる必要がある。
		aria-label に県名と件数を入れ、読み上げだけで内容が分かるようにする。

		地理的に正確な形ではなく角丸の枠に県名を書く。理由は
		prefecture-tiles.ts の冒頭に書いた。
	-->
	<div class="map">
		<svg {viewBox} role="group" aria-label="都道府県ごとの投稿">
			{#each tiles as tile (tile.code)}
				{@const p = tile.prefecture}
				<a
					class="tile"
					class:visited={tile.visited}
					style="--hue:{tile.hue};"
					href={resolve('/prefectures/[code]', { code: p.code })}
					aria-label="{p.name} {p.postCount}件{tile.visited ? '（訪問済み）' : '（未訪問）'}"
				>
					{#if tile.tail}
						<!-- 北海道の左下の突起。形の手がかりとして付ける。 -->
						<rect
							class="tail"
							x={tile.x + tile.w * 0.06}
							y={tile.y + tile.h - 2}
							width={tile.w * 0.2}
							height={TILE_UNIT_Y * 0.42}
							rx={RADIUS * 0.6}
						/>
					{/if}
					<rect class="box" x={tile.x} y={tile.y} width={tile.w} height={tile.h} rx={RADIUS} />
					<!--
						**色の違いだけで訪問済みを示さない。** 訪問済みには
						右上に印を打つ。塗りの濃さ・印の有無・読み上げのラベルの
						3つで伝える。
					-->
					{#if tile.visited}
						<circle class="dot" cx={tile.x + tile.w - 7} cy={tile.y + 7} r="2.6" />
					{/if}
					<text
						class="name"
						x={tile.x + tile.w / 2}
						y={tile.y + tile.h / 2}
						text-anchor="middle"
						dominant-baseline="central"
						font-size={tile.fontSize.toFixed(1)}
					>
						{tile.label}
					</text>
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
						**行にも地方の色を薄く敷く。** マップと表で同じ地方が同じ色になり、
						見比べたときに対応が取れる。薄くするのは、色が情報を運ぶのではなく
						地方のまとまりを示すだけだからである。地方そのものは「地方」の列に
						語で出ており、色が見えなくても情報は落ちない。
					-->
					<tr style="--hue:{REGION_HUES[p.region] ?? 210};">
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

	.map {
		/* **狭い画面でも横に溢れない。** viewBox の比率のまま縮む。 */
		width: 100%;
		max-width: 34rem;
		margin-top: var(--space-4);
	}

	svg {
		display: block;
		width: 100%;
		height: auto;
		overflow: visible;
	}

	.box,
	.tail {
		/* 未訪問。地方の色を薄く敷き、輪郭は墨で締める。
		   **未訪問でも地方の色を薄く付ける。** 塗られていない状態でも
		   「どの地方の県か」が見え、まとまりとして地図が読める。 */
		fill: hsl(var(--hue) 38% 84%);
		stroke: var(--color-border);
		stroke-width: 1.4;
		transition: fill 0.15s;
	}

	/* **白文字とのコントラストを確保するため明度を下げている。**
	   色相によっては 55% だと 4.5:1 を下回る。 */
	.visited .box,
	.visited .tail {
		fill: hsl(var(--hue) 52% 30%);
	}

	.name {
		font-weight: 700;
		fill: var(--color-text);
		/* 文字の上でも押せる。枠と別に当たり判定を作らない。 */
		pointer-events: none;
	}

	.visited .name {
		fill: #fff;
	}

	.dot {
		fill: #fff;
		pointer-events: none;
	}

	.tile:hover .box,
	.tile:hover .tail {
		fill: hsl(var(--hue) 52% 44%);
	}

	.tile:hover .name {
		fill: #fff;
	}

	/* **フォーカスが見えること。** SVG では outline の描かれ方が
	   ブラウザで揃わないため、輪郭線そのものを太くする。 */
	.tile:focus {
		outline: none;
	}

	.tile:focus-visible .box {
		stroke: var(--color-text);
		stroke-width: 3.5;
	}

	@media (prefers-reduced-motion: reduce) {
		.box,
		.tail {
			transition: none;
		}
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
	 * 行の背景。**色相だけを行から受け取り、明度と彩度はここで決める。**
	 *
	 * 行ごとに出来上がった色を渡すと、暗いテーマに切り替えたときに
	 * 追従できない。色相はデータ（地方）で決まり、明るさは見た目の都合で
	 * 決まるので、決める場所を分ける。
	 *
	 * 明度 95% は本文の色（#1c2b2d）に対してどの色相でも 15:1 以上あり、
	 * 4.5:1 の基準を大きく超える。
	 */
	tbody tr {
		background: hsl(var(--hue) 55% 95%);
	}

	/* **暗いテーマでは明度を反転する。** 同じ 95% を敷くと、
	   明るい地に明るい文字（#f6ecd8）が乗って読めなくなる。
	   18% はどの色相でも 9:1 以上ある。 */
	@media (prefers-color-scheme: dark) {
		tbody tr {
			background: hsl(var(--hue) 30% 18%);
		}
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
</style>
