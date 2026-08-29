<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { listPrefectures, searchPosts, type Post, type Prefecture } from '$lib/api/posts';
	import { session } from '$lib/auth/session.svelte';
	import BackLink from '$lib/components/BackLink.svelte';
	import PostCard from '$lib/components/PostCard.svelte';

	type State =
		| { kind: 'loading' }
		| { kind: 'ready'; posts: Post[]; nextCursor: string | null }
		| { kind: 'error'; message: string };

	let view = $state<State>({ kind: 'loading' });
	let loadingMore = $state(false);
	let prefecture = $state<Prefecture | null>(null);

	let code = $derived(page.params.code ?? '');

	$effect(() => {
		if (session.restored && session.isAuthenticated) {
			void load(code);
		}
	});

	async function load(c: string) {
		view = { kind: 'loading' };
		prefecture = null;

		// **県名は都道府県マスタから引く。** 画面に名前を持たせると、
		// マスタと食い違ったときに気づけない。
		try {
			prefecture = (await listPrefectures()).find((p) => p.code === c) ?? null;
		} catch {
			// 名前が出せなくても投稿は出す。見出しをコードのままにする。
		}

		try {
			const feed = await searchPosts({ prefectureCode: c });
			view = { kind: 'ready', posts: feed.posts, nextCursor: feed.nextCursor ?? null };
		} catch {
			view = { kind: 'error', message: '投稿を取得できませんでした' };
		}
	}

	async function loadMore() {
		if (view.kind !== 'ready' || !view.nextCursor || loadingMore) return;
		loadingMore = true;
		try {
			const feed = await searchPosts({ prefectureCode: code, cursor: view.nextCursor });
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

	let title = $derived(prefecture ? prefecture.name : `都道府県コード ${code}`);
</script>

<svelte:head>
	<title>{title} — tabi-log</title>
</svelte:head>

{#if !session.restored}
	<p>読み込んでいます…</p>
{:else if !session.isAuthenticated}
	<p>この一覧を見るには <a href={resolve('/login')}>ログイン</a> が必要です。</p>
{:else}
	<nav aria-label="パンくず">
		<a href={resolve('/')}>ホーム</a> ／ {title}
	</nav>

	<h1>{title}の投稿</h1>

	{#if view.kind === 'loading'}
		<p>読み込んでいます…</p>
	{:else if view.kind === 'error'}
		<p class="error" role="alert"><span aria-hidden="true">✕</span> {view.message}</p>
	{:else if view.posts.length === 0}
		<!-- 0件はエラーではない。次に何をすればよいかを示す。 -->
		<div class="empty">
			<p>{title}の投稿はまだありません。</p>
			<p><a href={resolve('/posts/new')}>最初の投稿をしてみましょう。</a></p>
		</div>
	{:else}
		<ul class="feed">
			{#each view.posts as post (post.id)}
				<li><PostCard {post} /></li>
			{/each}
		</ul>

		{#if view.nextCursor}
			<button type="button" onclick={loadMore} disabled={loadingMore}>
				{loadingMore ? '読み込んでいます…' : 'さらに読み込む'}
			</button>
		{/if}
	{/if}

	<BackLink href={resolve('/')} />
{/if}

<style>
	nav {
		margin-bottom: var(--space-4);
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	h1 {
		margin-top: 0;
	}

	.empty {
		padding: var(--space-8) var(--space-4);
		text-align: center;
		border: 1px dashed var(--color-border);
		border-radius: var(--radius);
	}

	.feed {
		display: flex;
		flex-direction: column;
		gap: var(--space-6);
		list-style: none;
		margin: 0;
		padding: 0;
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
		border: var(--line);
		border-radius: var(--radius);
		cursor: pointer;
	}

	button:disabled {
		cursor: progress;
	}

	.error {
		color: var(--color-danger);
	}
</style>
