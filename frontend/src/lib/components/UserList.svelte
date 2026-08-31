<script lang="ts">
	import { resolve } from '$app/paths';
	import LoadMore from '$lib/components/LoadMore.svelte';
	import { ApiError } from '$lib/api/client';
	import type { UserSummary } from '$lib/api/users';
	import Avatar from '$lib/components/Avatar.svelte';
	import BackLink from '$lib/components/BackLink.svelte';
	import FollowButton from '$lib/components/FollowButton.svelte';

	let {
		handle,
		heading,
		emptyMessage,
		load
	}: {
		handle: string;
		heading: string;
		emptyMessage: string;
		/** フォロワーとフォロー中で違うのは取得先だけなので、関数で受け取る。 */
		load: (
			handle: string,
			cursor?: string | null
		) => Promise<{ users: UserSummary[]; nextCursor?: string | null }>;
	} = $props();

	type State =
		| { kind: 'loading' }
		| { kind: 'ready'; users: UserSummary[]; nextCursor: string | null }
		| { kind: 'error'; message: string };

	let view = $state<State>({ kind: 'loading' });
	let loadingMore = $state(false);

	$effect(() => {
		void loadFirst(handle);
	});

	async function loadFirst(h: string) {
		view = { kind: 'loading' };
		try {
			const page = await load(h);
			view = { kind: 'ready', users: page.users, nextCursor: page.nextCursor ?? null };
		} catch (e) {
			view = {
				kind: 'error',
				message:
					e instanceof ApiError && e.status === 404
						? '利用者が見つかりません'
						: '一覧を取得できませんでした'
			};
		}
	}

	async function loadMore() {
		if (view.kind !== 'ready' || !view.nextCursor || loadingMore) return;
		loadingMore = true;
		try {
			const page = await load(handle, view.nextCursor);
			view = {
				kind: 'ready',
				users: [...view.users, ...page.users],
				nextCursor: page.nextCursor ?? null
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
	<title>{heading} — tabi-log</title>
</svelte:head>

<nav aria-label="パンくず">
	<a href={resolve('/users/[handle]', { handle })}>@{handle}</a> ／ {heading}
</nav>

<h1>{heading}</h1>

{#if view.kind === 'loading'}
	<p>読み込んでいます…</p>
{:else if view.kind === 'error'}
	<p class="error" role="alert"><span aria-hidden="true">✕</span> {view.message}</p>
	<p><a href={resolve('/')}>新着へ戻る</a></p>
{:else if view.users.length === 0}
	<p class="empty">{emptyMessage}</p>
{:else}
	<ul class="users">
		{#each view.users as user (user.id)}
			<li>
				<a class="identity" href={resolve('/users/[handle]', { handle: user.handle })}>
					<Avatar url={user.avatarUrl} displayName={user.displayName} size="small" />
					<span class="name">{user.displayName}</span>
					<span class="handle">@{user.handle}</span>
				</a>

				<!-- 自分自身にフォローの導線は出さない。 -->
				{#if !user.isMe}
					<FollowButton
						handle={user.handle}
						displayName={user.displayName}
						following={user.isFollowing}
					/>
				{/if}
			</li>
		{/each}
	</ul>

	<LoadMore
		hasMore={Boolean(view.nextCursor)}
		loading={loadingMore}
		onLoadMore={loadMore}
		label="さらに読み込む"
	/>
{/if}

<BackLink href={resolve('/users/[handle]', { handle })} label="プロフィール" />

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
		color: var(--color-text-muted);
	}

	.users {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		list-style: none;
		margin: 0;
		padding: 0;
	}

	.users li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: var(--space-3);
		background: var(--color-surface);
		border-radius: var(--radius);
	}

	.identity {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: var(--space-2);
		color: inherit;
		text-decoration: none;
	}

	.name {
		font-weight: 600;
	}

	.handle {
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	.error {
		color: var(--color-danger);
	}
</style>
