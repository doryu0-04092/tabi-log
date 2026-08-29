<script lang="ts">
	import { resolve } from '$app/paths';
	import type { PrefectureCount } from '$lib/api/users';
	import {
		PREFECTURE_TILES,
		REGION_HUES,
		TILE_COLUMNS,
		TILE_ROWS
	} from '$lib/data/prefecture-tiles';

	let { prefectures }: { prefectures: PrefectureCount[] } = $props();

	let byCode = $derived(new Map(prefectures.map((p) => [p.code, p])));

	/** マスの並び。配置データの順にそのまま描く。 */
	let tiles = $derived(
		PREFECTURE_TILES.map((t) => ({ ...t, prefecture: byCode.get(t.code) })).filter(
			(t) => t.prefecture !== undefined
		)
	);

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
	-->
	<ul
		class="grid"
		style="--rows:{TILE_ROWS}; --columns:{TILE_COLUMNS};"
		aria-label="都道府県ごとの投稿"
	>
		{#each tiles as tile (tile.code)}
			{@const p = tile.prefecture!}
			{@const isVisited = p.postCount > 0}
			<li style="grid-row:{tile.row}; grid-column:{tile.column};">
				<a
					class="tile"
					class:visited={isVisited}
					style="--hue:{REGION_HUES[p.region] ?? 210};"
					href={resolve('/prefectures/[code]', { code: p.code })}
					aria-label="{p.name} {p.postCount}件{isVisited ? '（訪問済み）' : '（未訪問）'}"
				>
					<!--
						**色の違いだけで訪問済みを示さない。** 訪問済みには
						県名の頭文字を出し、未訪問には出さない。塗りの濃さ・
						文字の有無・読み上げのラベルの3つで伝える。
					-->
					<span aria-hidden="true" class="mark">{isVisited ? p.name.slice(0, 1) : ''}</span>
				</a>
			</li>
		{/each}
	</ul>

	<button type="button" onclick={() => (showTable = !showTable)} aria-expanded={showTable}>
		{showTable ? '表を閉じる' : '同じ内容を表で見る'}
	</button>

	{#if showTable}
		<table>
			<caption>都道府県ごとの投稿数</caption>
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
					<tr>
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

	.grid {
		display: grid;
		grid-template-rows: repeat(var(--rows), 1fr);
		grid-template-columns: repeat(var(--columns), 1fr);
		gap: 2px;
		list-style: none;
		/* **狭い画面でも崩れない。** マスの大きさは列数から決まるため、
		   横に溢れず、正方形の比率だけを保つ。 */
		width: 100%;
		max-width: 30rem;
		aspect-ratio: var(--columns) / var(--rows);
		margin: var(--space-4) 0 0;
		padding: 0;
	}

	.tile {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 100%;
		height: 100%;
		/* 未訪問は薄く、枠線だけで存在を示す。 */
		background: hsl(var(--hue) 20% 92%);
		border: 1px solid var(--color-border);
		border-radius: 2px;
		font-size: 0.625rem;
		font-weight: 700;
		color: var(--color-text);
		text-decoration: none;
	}

	/* **白文字とのコントラストを確保するため明度を下げている。**
	   色相によっては 55% だと 4.5:1 を下回る。 */
	.tile.visited {
		background: hsl(var(--hue) 60% 34%);
		border-color: hsl(var(--hue) 60% 26%);
		color: #fff;
	}

	.mark {
		line-height: 1;
	}

	button {
		min-height: 2.75rem;
		margin-top: var(--space-4);
		padding: var(--space-2) var(--space-4);
		font: inherit;
		color: var(--color-text);
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		cursor: pointer;
	}

	table {
		width: 100%;
		margin-top: var(--space-4);
		border-collapse: collapse;
		font-size: 0.875rem;
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
		border-bottom: 1px solid var(--color-border);
	}

	thead th {
		color: var(--color-text-muted);
		font-weight: 600;
	}

	tbody th {
		font-weight: 600;
	}
</style>
