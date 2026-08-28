<script lang="ts">
	import { resolve } from '$app/paths';
	import { listPosts, type Post } from '$lib/api/posts';
	import { session } from '$lib/auth/session.svelte';
	import PostCard from '$lib/components/PostCard.svelte';

	type State =
		| { kind: 'loading' }
		| { kind: 'ready'; posts: Post[]; nextCursor: string | null }
		| { kind: 'error'; message: string };

	let view = $state<State>({ kind: 'loading' });
	let loadingMore = $state(false);

	// 未ログインでは投稿を取得できない（フィードは認証を要する）。
	// 復元が終わるまで待ってから判断する。
	$effect(() => {
		if (session.restored && session.isAuthenticated && view.kind === 'loading') {
			void load();
		}
	});

	async function load() {
		try {
			const feed = await listPosts();
			view = { kind: 'ready', posts: feed.posts, nextCursor: feed.nextCursor ?? null };
		} catch {
			view = { kind: 'error', message: 'フィードを取得できませんでした' };
		}
	}

	async function loadMore() {
		if (view.kind !== 'ready' || !view.nextCursor || loadingMore) return;
		loadingMore = true;
		try {
			const feed = await listPosts(view.nextCursor);
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
		<h1>新着</h1>
		<a class="new-post" href={resolve('/posts/new')}>投稿する</a>
	</div>

	{#if view.kind === 'loading'}
		<p>読み込んでいます…</p>
	{:else if view.kind === 'error'}
		<p class="error" role="alert"><span aria-hidden="true">✕</span> {view.message}</p>
	{:else if view.posts.length === 0}
		<!-- 何も出ない画面を作らない。次に何をすればよいかを示す。 -->
		<div class="empty">
			<p>まだ投稿がありません。</p>
			<p><a href={resolve('/posts/new')}>最初の投稿をしてみましょう。</a></p>
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
