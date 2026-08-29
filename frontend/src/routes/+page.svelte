<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { listFollowingFeed, listPosts, type Post } from '$lib/api/posts';
	import { session } from '$lib/auth/session.svelte';
	import PostCard from '$lib/components/PostCard.svelte';

	type State =
		| { kind: 'loading' }
		| { kind: 'ready'; posts: Post[]; nextCursor: string | null }
		| { kind: 'error'; message: string };

	type Tab = 'following' | 'latest';

	let view = $state<State>({ kind: 'loading' });
	let loadingMore = $state(false);

	/**
	 * どちらのフィードを見ているか。
	 *
	 * **URL に持たせる。** 状態を画面の中だけに持つと、リロードや共有で
	 * 「新着」に戻ってしまい、開いていた場所へ戻れない。
	 */
	let tab = $derived<Tab>(
		page.url.searchParams.get('tab') === 'following' ? 'following' : 'latest'
	);

	// 未ログインでは投稿を取得できない（フィードは認証を要する）。
	// 復元が終わるまで待ってから判断する。
	$effect(() => {
		if (session.restored && session.isAuthenticated) {
			void load(tab);
		}
	});

	async function load(which: Tab) {
		view = { kind: 'loading' };
		try {
			const feed = await (which === 'following' ? listFollowingFeed() : listPosts());
			view = { kind: 'ready', posts: feed.posts, nextCursor: feed.nextCursor ?? null };
		} catch {
			view = { kind: 'error', message: 'フィードを取得できませんでした' };
		}
	}

	async function loadMore() {
		if (view.kind !== 'ready' || !view.nextCursor || loadingMore) return;
		loadingMore = true;
		try {
			const feed = await (tab === 'following'
				? listFollowingFeed(view.nextCursor)
				: listPosts(view.nextCursor));
			view = {
				kind: 'ready',
				posts: [...view.posts, ...feed.posts],
				nextCursor: feed.nextCursor ?? null
			};
		} catch {
			// 追加読み込みの失敗で、既に表示している分まで消さない。
			view = { ...view };
		} finally {
			loadingMore = false;
		}
	}
</script>

<svelte:head>
	<title>tabi-log</title>
</svelte:head>

{#if !session.restored}
	<p>読み込んでいます…</p>
{:else if !session.isAuthenticated}
	<h1>tabi-log</h1>
	<p>旅行先の写真と記録を共有する SNS です。</p>
	<p>
		<a href={resolve('/login')}>ログイン</a> または
		<a href={resolve('/signup')}>新規登録</a> をしてください。
	</p>
{:else}
	<div class="head">
		<h1>フィード</h1>
		<a class="new-post" href={resolve('/posts/new')}>投稿する</a>
	</div>

	<!--
		タブはリンクにする。ボタンだと戻る操作で前のタブへ戻れず、
		URL を共有しても相手には別のタブが開く。
		現在地は aria-current で示し、下線だけに頼らない。
	-->
	<nav class="tabs" aria-label="フィードの種類">
		<a href="{resolve('/')}?tab=following" aria-current={tab === 'following' ? 'page' : undefined}>
			フォロー中
		</a>
		<a href={resolve('/')} aria-current={tab === 'latest' ? 'page' : undefined}>新着</a>
	</nav>

	{#if view.kind === 'loading'}
		<p>読み込んでいます…</p>
	{:else if view.kind === 'error'}
		<p class="error" role="alert"><span aria-hidden="true">✕</span> {view.message}</p>
	{:else if view.posts.length === 0}
		<!-- 何も出ない画面を作らない。次に何をすればよいかを示す。 -->
		<div class="empty">
			{#if tab === 'following'}
				<p>フォロー中の人の投稿はまだありません。</p>
				<!--
					**自分の投稿はここに出ない。** 自分自身はフォローできないため。
					誤解しやすいので、行き先とあわせて明示する。
				-->
				<p>
					<a href={resolve('/')}>新着</a> から気になる人を見つけてフォローすると、ここに投稿が並びます。
				</p>
			{:else}
				<p>まだ投稿がありません。</p>
				<p><a href={resolve('/posts/new')}>最初の投稿をしてみましょう。</a></p>
			{/if}
		</div>
	{:else}
		<ul class="feed">
			{#each view.posts as post (post.id)}
				<li><PostCard {post} /></li>
			{/each}
		</ul>

		{#if view.nextCursor}
			<!--
				無限スクロールにはしない。キーボードと読み上げソフトでは
				「いつ終わるか分からない」ものになり、末尾へ到達できなくなる。
			-->
			<button type="button" onclick={loadMore} disabled={loadingMore}>
				{loadingMore ? '読み込んでいます…' : 'さらに読み込む'}
			</button>
		{/if}
	{/if}
{/if}

<style>
	.head {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: var(--space-4);
	}

	h1 {
		margin-top: 0;
	}

	.new-post {
		padding: var(--space-2) var(--space-4);
		font-weight: 600;
		color: var(--color-accent-text);
		background: var(--color-accent);
		border-radius: var(--radius);
		text-decoration: none;
	}

	.tabs {
		display: flex;
		gap: var(--space-2);
		margin-bottom: var(--space-6);
		border-bottom: 1px solid var(--color-border);
	}

	.tabs a {
		/* 指で押す領域を確保する。 */
		min-height: 2.75rem;
		display: flex;
		align-items: center;
		padding: 0 var(--space-4);
		color: var(--color-text-muted);
		text-decoration: none;
		border-bottom: 3px solid transparent;
	}

	/* 現在地は太字と下線の両方で示す。色の違いだけに頼らない。 */
	.tabs a[aria-current='page'] {
		color: var(--color-text);
		font-weight: 700;
		border-bottom-color: var(--color-accent);
	}

	.feed {
		display: flex;
		flex-direction: column;
		gap: var(--space-6);
		list-style: none;
		margin: 0;
		padding: 0;
	}

	.empty {
		padding: var(--space-8) var(--space-4);
		text-align: center;
		border: 1px dashed var(--color-border);
		border-radius: var(--radius);
	}

	.error {
		color: var(--color-danger);
	}

	button {
		display: block;
		width: 100%;
		min-height: 2.75rem;
		margin-top: var(--space-6);
		padding: var(--space-3);
		font: inherit;
		color: var(--color-text);
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		cursor: pointer;
	}

	button:disabled {
		cursor: progress;
	}
</style>
