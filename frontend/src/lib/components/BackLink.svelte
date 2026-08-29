<script lang="ts">
	import { resolve } from '$app/paths';
	import { navHistory } from '$lib/navigation/history.svelte';

	let {
		/** 履歴が無いときの行き先の表示名。 */
		label = 'ホーム',
		/** 履歴が無いときの行き先。 */
		href = resolve('/')
	}: { label?: string; href?: string } = $props();
</script>

<!--
	**まず「一つ前の画面」へ戻す。**
	行き先を固定にすると、プロフィールの制覇マップから都道府県別の一覧へ
	飛んだあと、ホームへ飛ばされて元の画面に戻れない。
	辿ってきた道を戻れることのほうが、決まった場所へ行けることより大事である。

	直接リンクで開いた場合は履歴が無いので、そのときだけ決まった行き先を出す。
-->
<p class="back">
	{#if navHistory.canGoBack}
		<button type="button" onclick={() => history.back()}>
			<span aria-hidden="true">←</span>
			前の画面へ戻る
		</button>
	{:else}
		<!--
			行き先は呼び出し側が resolve() で解決して渡す。
			eslint の no-navigation-without-resolve は href が resolve() の
			呼び出しそのものであることを求めるが、部品として受け取る以上
			ここでは判定できない。意図して外す。
		-->
		<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -->
		<a {href}>
			<span aria-hidden="true">←</span>
			{label}へ戻る
		</a>
	{/if}
</p>

<style>
	.back {
		margin: var(--space-6) 0 0;
		padding-top: var(--space-4);
		border-top: var(--line);
	}

	/* 押す領域を確保する。文字リンクのままだと指では狙いにくい。
	   リンクとボタンで見た目を揃え、どちらが出ても同じ操作に見えるようにする。 */
	a,
	button {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 2.75rem;
		padding: 0 var(--space-3);
		font: inherit;
		color: var(--color-text);
		background: transparent;
		text-decoration: none;
		border: var(--line);
		border-radius: var(--radius);
		cursor: pointer;
	}

	a:hover,
	button:hover {
		background: var(--color-surface);
	}
</style>
