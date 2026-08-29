<script lang="ts">
	import { afterNavigate, goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import favicon from '$lib/assets/favicon.svg';
	import { getUnreadCount } from '$lib/api/notifications';
	import { logout, onUnreadCleared, restoreSession, session } from '$lib/auth/session.svelte';
	import { recordNavigation } from '$lib/navigation/history.svelte';
	import '$lib/styles/tokens.css';

	let { children } = $props();

	// 「一つ前の画面へ戻る」を出してよいかの判断に使う。
	afterNavigate((nav) => recordNavigation(nav.type));

	// アクセストークンはメモリにしか無いため、リロードで消える。
	// Cookie のリフレッシュトークンから取り直す。
	$effect(() => {
		void restoreSession();
	});

	/**
	 * 未読の件数。見出しに数を出すためだけに引く。
	 *
	 * **画面を移るたびに取り直す。** リアルタイムの更新はしない
	 * （常時つなぐ仕組みを持ち込むほどの機能ではない）。
	 * 数だけの軽い問い合わせなので、遷移ごとでも負担が小さい。
	 */
	let unreadCount = $state(0);

	// 通知の一覧で既読にしたら、その場で鈴の印を消す。
	// 画面を移るまで待つと、押したのに印が残ったままになる。
	onUnreadCleared(() => {
		unreadCount = 0;
	});

	$effect(() => {
		// page.url を読むことで、画面を移るたびに取り直す。
		void page.url.pathname;
		if (!session.restored || !session.isAuthenticated) {
			unreadCount = 0;
			return;
		}
		void refreshUnread();
	});

	/** 現在地の判定。前方一致だと「/」が全画面に当たるため厳密に見る。 */
	function isCurrent(path: string): boolean {
		return page.url.pathname === path;
	}

	async function refreshUnread() {
		try {
			const { unreadCount: n } = await getUnreadCount();
			unreadCount = n;
		} catch {
			// 数が取れなくても画面は使える。見出しの数を出さないだけにする。
			unreadCount = 0;
		}
	}

	/**
	 * ヘッダーの簡易検索。
	 *
	 * **語を入れて探すだけなら、ここで完結させる。** 絞り込みを組み合わせる
	 * ときだけ詳細検索（/explore）へ行けばよい。探すたびに専用の画面へ
	 * 移動させると、見ていた場所を毎回失う。
	 */
	let keyword = $state('');

	async function submitSearch(event: SubmitEvent) {
		event.preventDefault();
		const q = keyword.trim();
		// 1文字では全文検索の索引（ngram、トークン長2）に当たらない。
		if (q.length < 2) return;
		// eslint-disable-next-line svelte/no-navigation-without-resolve
		await goto(`${resolve('/explore')}?q=${encodeURIComponent(q)}`);
	}

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
	<a class="brand" href={resolve('/')}>tabi-log</a>

	{#if session.restored && session.isAuthenticated}
		<!--
			**押せると分かる見た目にする。** 文字だけのリンクだと
			設定の項目のように見え、押してよいのか分かりづらい。
			現在地は aria-current と塗りで示す。
		-->
		<nav aria-label="主要">
			<a class="navlink" href={resolve('/')} aria-current={isCurrent('/') ? 'page' : undefined}>
				ホーム
			</a>
			<a
				class="navlink"
				href={resolve('/explore')}
				aria-current={isCurrent('/explore') ? 'page' : undefined}
			>
				検索
			</a>
			<!--
				**鈴の記号と赤い丸で示す。** Discord や LINE と同じ見せ方に合わせ、
				一目で「何か来ている」と分かるようにする。

				ただし**記号と色だけに頼らない。** 記号は aria-hidden で読み上げから
				外し、代わりに読み上げ用の文言（「通知 3件の未読」）を必ず持たせる。
				色が見えない環境でも件数が伝わる。
			-->
			<a
				class="navlink bell"
				href={resolve('/notifications')}
				aria-current={isCurrent('/notifications') ? 'page' : undefined}
			>
				<span class="bell-icon" aria-hidden="true">🔔</span>
				{#if unreadCount > 0}
					<span class="badge" aria-hidden="true">{unreadCount > 99 ? '99+' : unreadCount}</span>
				{/if}
				<span class="visually-hidden">
					通知{#if unreadCount > 0}&nbsp;{unreadCount}件の未読{/if}
				</span>
			</a>
		</nav>

		<form class="search" role="search" onsubmit={submitSearch}>
			<label class="visually-hidden" for="header-search">投稿を検索</label>
			<input
				id="header-search"
				type="search"
				bind:value={keyword}
				placeholder="投稿を検索"
				autocomplete="off"
			/>
			<button type="submit">探す</button>
		</form>
	{/if}

	<nav aria-label="アカウント">
		<!--
			復元が終わるまで何も出さない。ここで「ログイン」を先に描くと、
			リロードのたびに一瞬ログアウトしたように見える。
		-->
		{#if session.restored}
			{#if session.isAuthenticated}
				<!--
					**自分のプロフィールへの導線がここにしか無い。**
					付けないと、自分の投稿カードを探して名前を押すしか手段がなくなる。
				-->
				<a class="who" href={resolve('/users/[handle]', { handle: session.user?.handle ?? '' })}>
					{session.user?.displayName}
				</a>
				<button type="button" onclick={handleLogout}>ログアウト</button>
			{:else if page.url.pathname !== '/login' && page.url.pathname !== '/signup'}
				<a href={resolve('/login')}>ログイン</a>
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
		/* 紙より少し濃い面にして、下を太い線で締める。 */
		background: var(--color-surface);
		border-bottom: var(--line-strong);
		padding: var(--space-4);
	}

	/* ロゴだけ表示用の書体。**ここが「ポスター」の入口になる。** */
	.brand {
		font-family: var(--font-display);
		font-weight: 400;
		font-size: 1.15rem;
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
		display: inline-flex;
		align-items: center;
		min-height: 2.5rem;
		padding: 0 var(--space-2);
		color: var(--color-text-muted);
		text-decoration: none;
	}

	.who:hover {
		color: var(--color-text);
		text-decoration: underline;
	}

	/* 主要な移動先はボタン然とした見た目にする。 */
	.navlink {
		display: inline-flex;
		align-items: center;
		min-height: 2.5rem;
		padding: 0 var(--space-4);
		font-weight: 700;
		color: var(--color-text);
		background: var(--color-bg);
		border: var(--line);
		border-radius: var(--radius);
		text-decoration: none;
	}

	.navlink:hover {
		background: var(--color-surface);
	}

	/* 現在地は塗りと文字色で示す。下線だけに頼らない。 */
	.navlink[aria-current='page'] {
		color: var(--color-accent-text);
		background: var(--color-accent);
	}

	.search {
		display: flex;
		gap: var(--space-2);
	}

	.search input {
		min-width: 10rem;
		min-height: 2.5rem;
		padding: 0 var(--space-3);
		font: inherit;
		color: var(--color-text);
		background: var(--color-bg);
		border: var(--line);
		border-radius: var(--radius);
	}

	.search button {
		min-height: 2.5rem;
		padding: 0 var(--space-4);
		font: inherit;
		font-weight: 600;
		color: var(--color-text);
		background: var(--color-surface);
		border: var(--line);
		border-radius: var(--radius);
		cursor: pointer;
	}

	/* 鈴は正方形に近い形にして、他のボタンと押し心地を揃える。 */
	.bell {
		position: relative;
		padding: 0 var(--space-3);
	}

	.bell-icon {
		font-size: 1.125rem;
		line-height: 1;
	}

	/* 赤い丸。件数が入るので幅は内容に合わせる。 */
	.badge {
		position: absolute;
		top: -0.25rem;
		right: -0.25rem;
		min-width: 1.125rem;
		height: 1.125rem;
		padding: 0 0.25rem;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		font-size: 0.6875rem;
		font-weight: 700;
		line-height: 1;
		color: #fff;
		background: var(--color-danger);
		border: 2px solid var(--color-surface);
		/* バッジだけは円のまま。**「未読がある」の記号であり、
		   ボタンの仲間ではない。** 形で役割を分けている。 */
		border-radius: 999px;
	}

	nav button {
		padding: var(--space-1) var(--space-3);
		font: inherit;
		color: var(--color-text);
		background: transparent;
		border: var(--line);
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
