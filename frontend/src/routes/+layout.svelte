<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import favicon from '$lib/assets/favicon.svg';
	import { logout, restoreSession, session } from '$lib/auth/session.svelte';
	import '$lib/styles/tokens.css';

	let { children } = $props();

	// アクセストークンはメモリにしか無いため、リロードで消える。
	// Cookie のリフレッシュトークンから取り直す。
	$effect(() => {
		void restoreSession();
	});

	async function handleLogout() {
		await logout();
		await goto(resolve('/login'));
	}
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
	<a class="brand" href={resolve("/")}>tabi-log</a>

	<nav aria-label="アカウント">
		<!--
			復元が終わるまで何も出さない。ここで「ログイン」を先に描くと、
			リロードのたびに一瞬ログアウトしたように見える。
		-->
		{#if session.restored}
			{#if session.isAuthenticated}
				<span class="who">{session.user?.displayName}</span>
				<button type="button" onclick={handleLogout}>ログアウト</button>
			{:else if page.url.pathname !== '/login' && page.url.pathname !== '/signup'}
				<a href={resolve("/login")}>ログイン</a>
			{/if}
		{/if}
	</nav>
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
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-4);
		border-bottom: 1px solid var(--color-border);
		padding: var(--space-4);
	}

	.brand {
		font-weight: 700;
		letter-spacing: 0.02em;
		color: inherit;
		text-decoration: none;
	}

	nav {
		display: flex;
		align-items: center;
		gap: var(--space-4);
		min-height: 2.25rem;
	}

	.who {
		color: var(--color-text-muted);
	}

	nav button {
		padding: var(--space-1) var(--space-3);
		font: inherit;
		color: var(--color-text);
		background: transparent;
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		cursor: pointer;
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
