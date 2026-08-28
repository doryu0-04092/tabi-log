<script lang="ts">
	import favicon from '$lib/assets/favicon.svg';
	import '$lib/styles/tokens.css';

	let { children } = $props();
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

<!--
	スキップリンク。キーボード利用者がヘッダーを読み飛ばして
	本文へ直接移動できるようにする。普段は視覚的に隠れ、
	フォーカスが当たったときだけ現れる。
-->
<a class="skip-link" href="#main">本文へスキップ</a>

<header>
	<p class="brand">tabi-log</p>
</header>

<main id="main" tabindex="-1">
	{@render children()}
</main>

<style>
	.skip-link {
		position: absolute;
		left: var(--space-2);
		top: -3rem;
		z-index: 10;
		padding: var(--space-2) var(--space-4);
		background: var(--color-accent);
		color: var(--color-accent-text);
		border-radius: var(--radius);
		transition: top 0.15s ease;
	}

	.skip-link:focus {
		top: var(--space-2);
	}

	header {
		border-bottom: 1px solid var(--color-border);
		padding: var(--space-4);
	}

	.brand {
		margin: 0;
		font-weight: 700;
		letter-spacing: 0.02em;
	}

	main {
		max-width: var(--measure);
		margin: 0 auto;
		padding: var(--space-8) var(--space-4);
	}

	/* プログラム的にフォーカスするための tabindex="-1" が
	   輪郭を描かないようにする。スキップリンク経由の移動先であり、
	   利用者が Tab で辿り着く要素ではないため。 */
	main:focus {
		outline: none;
	}
</style>
