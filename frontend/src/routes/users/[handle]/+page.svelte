<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { ApiError } from '$lib/api/client';
	import type { Post } from '$lib/api/posts';
	import {
		getUserProfile,
		listUserPosts,
		PREFECTURE_TOTAL,
		type UserProfile
	} from '$lib/api/users';
	import { session } from '$lib/auth/session.svelte';
	import FollowButton from '$lib/components/FollowButton.svelte';
	import PostCard from '$lib/components/PostCard.svelte';

	type State =
		| { kind: 'loading' }
		| { kind: 'ready'; profile: UserProfile }
		| { kind: 'error'; message: string };

	let view = $state<State>({ kind: 'loading' });
	let posts = $state<Post[]>([]);
	let nextCursor = $state<string | null>(null);
	let postsLoaded = $state(false);
	let loadingMore = $state(false);

	// フォローの切り替えでフォロワー数が動く。ボタンだけ変わって数字が
	// 据え置きだと、押した結果が反映されていないように見える。
	let followerDelta = $state(0);

	let handle = $derived(page.params.handle ?? '');

	$effect(() => {
		if (session.restored && session.isAuthenticated) {
			void load(handle);
		}
	});

	async function load(h: string) {
		if (!h) return;
		view = { kind: 'loading' };
		posts = [];
		nextCursor = null;
		postsLoaded = false;
		followerDelta = 0;

		try {
			const profile = await getUserProfile(h);
			view = { kind: 'ready', profile };
		} catch (e) {
			view = {
				kind: 'error',
				message:
					e instanceof ApiError && e.status === 404
						? '利用者が見つかりません'
						: 'プロフィールを取得できませんでした'
			};
			return;
		}

		try {
			const feed = await listUserPosts(h);
			posts = feed.posts;
			nextCursor = feed.nextCursor ?? null;
		} catch {
			// **投稿が取れなくてもプロフィールは出す。** 片方の失敗で
			// 画面全体をエラーにすると、見られるはずの情報まで見られなくなる。
		} finally {
			postsLoaded = true;
		}
	}

	async function loadMore() {
		if (!nextCursor || loadingMore) return;
		loadingMore = true;
		try {
			const feed = await listUserPosts(handle, nextCursor);
			posts = [...posts, ...feed.posts];
			nextCursor = feed.nextCursor ?? null;
		} catch {
			// 追加読み込みの失敗で、既に表示している分まで消さない。
		} finally {
			loadingMore = false;
		}
	}

	let followerCount = $derived(
		view.kind === 'ready' ? view.profile.followerCount + followerDelta : 0
	);

	/** 制覇率。小数は出さない。1%未満の差を見せても意味が無い。 */
	let conquestRate = $derived(
		view.kind === 'ready'
			? Math.round((view.profile.visitedPrefectureCount / PREFECTURE_TOTAL) * 100)
			: 0
	);
</script>

<svelte:head>
	<title>
		{view.kind === 'ready' ? `${view.profile.displayName} — tabi-log` : 'tabi-log'}
	</title>
</svelte:head>

{#if !session.restored}
	<p>読み込んでいます…</p>
{:else if !session.isAuthenticated}
	<p>プロフィールを見るには <a href={resolve('/login')}>ログイン</a> が必要です。</p>
{:else if view.kind === 'loading'}
	<p>読み込んでいます…</p>
{:else if view.kind === 'error'}
	<p class="error" role="alert"><span aria-hidden="true">✕</span> {view.message}</p>
	<p><a href={resolve('/')}>新着へ戻る</a></p>
{:else}
	<header class="profile">
		<div class="identity">
			<h1>{view.profile.displayName}</h1>
			<p class="handle">@{view.profile.handle}</p>
		</div>

		<!-- 自分自身にフォローの導線は出さない。押せない導線は迷いのもとになる。 -->
		{#if !view.profile.isMe}
			<FollowButton
				handle={view.profile.handle}
				displayName={view.profile.displayName}
				following={view.profile.isFollowing}
				onchange={(following) =>
					// サーバーから来た状態を基準にした差分にする。
					// 「1 か 0」にすると、元からフォローしていた相手を外したとき
					// 数字が減らない。
					(followerDelta =
						(following ? 1 : 0) - (view.kind === 'ready' && view.profile.isFollowing ? 1 : 0))}
			/>
		{/if}
	</header>

	{#if view.profile.bio}
		<p class="bio">{view.profile.bio}</p>
	{/if}

	<!--
		件数は数字だけを並べず、必ず語を添える。
		「12 / 3 / 4」だけでは何の数か読み上げで分からない。
	-->
	<dl class="counts">
		<div>
			<dt>投稿</dt>
			<dd>{view.profile.postCount}件</dd>
		</div>
		<div>
			<dt>フォロー中</dt>
			<dd>
				<a href={resolve('/users/[handle]/following', { handle: view.profile.handle })}>
					{view.profile.followingCount}人
				</a>
			</dd>
		</div>
		<div>
			<dt>フォロワー</dt>
			<dd>
				<a href={resolve('/users/[handle]/followers', { handle: view.profile.handle })}>
					{followerCount}人
				</a>
			</dd>
		</div>
		<div>
			<!--
				制覇マップそのものは未実装。数だけ先に出す。
				割合は色分けではなく数と語で示す。
			-->
			<dt>訪れた都道府県</dt>
			<dd>{view.profile.visitedPrefectureCount} / {PREFECTURE_TOTAL}（{conquestRate}%）</dd>
		</div>
	</dl>

	<h2>投稿</h2>

	{#if !postsLoaded}
		<p>読み込んでいます…</p>
	{:else if posts.length === 0}
		<p class="empty">まだ投稿がありません。</p>
	{:else}
		<ul class="feed">
			{#each posts as post (post.id)}
				<li><PostCard {post} /></li>
			{/each}
		</ul>

		{#if nextCursor}
			<button type="button" class="more" onclick={loadMore} disabled={loadingMore}>
				{loadingMore ? '読み込んでいます…' : 'さらに読み込む'}
			</button>
		{/if}
	{/if}
{/if}

<style>
	.profile {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-4);
	}

	h1 {
		margin: 0;
	}

	.handle {
		margin: var(--space-1) 0 0;
		color: var(--color-text-muted);
	}

	.bio {
		margin-top: var(--space-4);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}

	.counts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-4) var(--space-6);
		margin: var(--space-4) 0 0;
		padding: var(--space-4) 0;
		border-top: 1px solid var(--color-border);
		border-bottom: 1px solid var(--color-border);
	}

	.counts dt {
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	.counts dd {
		margin: var(--space-1) 0 0;
		font-weight: 600;
	}

	h2 {
		margin-top: var(--space-6);
		font-size: 1.125rem;
	}

	.empty {
		color: var(--color-text-muted);
	}

	.feed {
		display: flex;
		flex-direction: column;
		gap: var(--space-6);
		list-style: none;
		margin: 0;
		padding: 0;
	}

	.more {
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

	.more:disabled {
		cursor: progress;
	}

	.error {
		color: var(--color-danger);
	}
</style>
