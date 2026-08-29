<script lang="ts">
	import { resolve } from '$app/paths';
	import LoadMore from '$lib/components/LoadMore.svelte';
	import {
		listNotifications,
		markAllNotificationsRead,
		markNotificationRead,
		type Notification
	} from '$lib/api/notifications';
	import { clearUnreadBadge } from '$lib/auth/session.svelte';
	import { session } from '$lib/auth/session.svelte';
	import BackLink from '$lib/components/BackLink.svelte';
	import Avatar from '$lib/components/Avatar.svelte';

	type State =
		| { kind: 'loading' }
		| { kind: 'ready'; items: Notification[]; nextCursor: string | null }
		| { kind: 'error'; message: string };

	let view = $state<State>({ kind: 'loading' });
	let loadingMore = $state(false);
	let markingAll = $state(false);
	let actionError = $state('');

	$effect(() => {
		if (session.restored && session.isAuthenticated) {
			void load();
		}
	});

	async function load() {
		try {
			const page = await listNotifications();
			view = { kind: 'ready', items: page.notifications, nextCursor: page.nextCursor ?? null };
		} catch {
			view = { kind: 'error', message: '通知を取得できませんでした' };
			return;
		}

		// **開いた時点で既読にする。** 鈴の赤い印は「見ていないものがある」印であり、
		// 一覧を開いた以上は消えるのが自然である。消えないと、何度開いても
		// 印が残り続けることになる。
		//
		// **未読の見た目は消さない。** どれが新しかったのかは、この画面に
		// 留まっている間は分かるようにしておく（次に開いたときには消える）。
		void markSeen();
	}

	/**
	 * 開いたことをサーバーに伝える。
	 *
	 * 失敗しても画面は止めない。次に開いたときにまた試されるだけである。
	 */
	async function markSeen() {
		if (view.kind !== 'ready' || view.items.every((n) => n.isRead)) return;
		try {
			await markAllNotificationsRead();
			// ヘッダーの鈴から赤い印を消す。
			clearUnreadBadge();
		} catch {
			// 既読にできなくても一覧は読める。
		}
	}

	async function loadMore() {
		if (view.kind !== 'ready' || !view.nextCursor || loadingMore) return;
		loadingMore = true;
		try {
			const page = await listNotifications(view.nextCursor);
			view = {
				kind: 'ready',
				items: [...view.items, ...page.notifications],
				nextCursor: page.nextCursor ?? null
			};
		} catch {
			// 追加読み込みの失敗で、既に表示している分まで消さない。
			view = { ...view };
		} finally {
			loadingMore = false;
		}
	}

	let unreadCount = $derived(
		view.kind === 'ready' ? view.items.filter((n) => !n.isRead).length : 0
	);

	/**
	 * 開いた通知を既読にする。
	 *
	 * **見た目を先に変える。** 通知を開いてから数百ミリ秒あとに未読の印が
	 * 消えると、押せていないと思って戻ってきてしまう。
	 */
	async function markRead(n: Notification) {
		if (n.isRead || view.kind !== 'ready') return;

		const previous = view.items;
		view = {
			...view,
			items: view.items.map((x) => (x.id === n.id ? { ...x, isRead: true } : x))
		};
		try {
			await markNotificationRead(n.id);
		} catch {
			view = { ...view, items: previous };
			actionError = '既読にできませんでした';
		}
	}

	async function markAll() {
		if (view.kind !== 'ready' || markingAll) return;
		markingAll = true;
		actionError = '';

		const previous = view.items;
		view = { ...view, items: view.items.map((x) => ({ ...x, isRead: true })) };
		try {
			await markAllNotificationsRead();
			clearUnreadBadge();
		} catch {
			view = { ...view, items: previous };
			actionError = 'すべてを既読にできませんでした';
		} finally {
			markingAll = false;
		}
	}

	/** 通知の文言。契機ごとに主語と述語が変わる。 */
	function describe(n: Notification): string {
		switch (n.type) {
			case 'like':
				return 'があなたの投稿にいいねしました';
			case 'comment':
				return 'があなたの投稿にコメントしました';
			default:
				return 'があなたをフォローしました';
		}
	}

	function formatCreatedAt(iso: string): string {
		const d = new Date(iso);
		const hh = String(d.getHours()).padStart(2, '0');
		const mm = String(d.getMinutes()).padStart(2, '0');
		return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日 ${hh}:${mm}`;
	}
</script>

<svelte:head>
	<title>通知 — tabi-log</title>
</svelte:head>

{#if !session.restored}
	<p>読み込んでいます…</p>
{:else if !session.isAuthenticated}
	<p>通知を見るには <a href={resolve('/login')}>ログイン</a> が必要です。</p>
{:else}
	<div class="head">
		<h1>通知</h1>
		{#if unreadCount > 0}
			<button type="button" onclick={markAll} disabled={markingAll}>
				{markingAll ? '既読にしています…' : `すべて既読にする（${unreadCount}件）`}
			</button>
		{/if}
	</div>

	{#if actionError}
		<p class="error" role="alert"><span aria-hidden="true">✕</span> {actionError}</p>
	{/if}

	{#if view.kind === 'loading'}
		<p>読み込んでいます…</p>
	{:else if view.kind === 'error'}
		<p class="error" role="alert"><span aria-hidden="true">✕</span> {view.message}</p>
	{:else if view.items.length === 0}
		<p class="empty">まだ通知はありません。</p>
	{:else}
		<ul class="notifications">
			{#each view.items as n (n.id)}
				<li class:unread={!n.isRead}>
					<p class="text">
						<Avatar url={n.actor.avatarUrl} displayName={n.actor.displayName} size="small" />
						<!--
							未読は色だけでなく語でも示す。色を判別しない環境でも
							「未読」と読み上げられる。
						-->
						{#if !n.isRead}
							<span class="badge">未読</span>
						{/if}
						<a class="actor" href={resolve('/users/[handle]', { handle: n.actor.handle })}>
							{n.actor.displayName}
						</a>{describe(n)}
					</p>

					{#if n.commentBody}
						<p class="quote">{n.commentBody}</p>
					{/if}

					<div class="foot">
						<time datetime={n.createdAt}>{formatCreatedAt(n.createdAt)}</time>
						{#if n.postId}
							<a
								href={resolve('/posts/[postId]', { postId: String(n.postId) })}
								onclick={() => markRead(n)}
							>
								投稿を見る
							</a>
						{/if}
						{#if !n.isRead}
							<button type="button" class="link" onclick={() => markRead(n)}>既読にする</button>
						{/if}
					</div>
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

	<BackLink label="ホーム" href={resolve('/')} />
{/if}

<style>
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-4);
	}

	h1 {
		margin: 0;
	}

	.empty {
		color: var(--color-text-muted);
	}

	.notifications {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		list-style: none;
		margin: var(--space-4) 0 0;
		padding: 0;
	}

	.notifications li {
		padding: var(--space-3);
		background: var(--color-surface);
		border-left: 3px solid transparent;
		border-radius: var(--radius);
	}

	/* 未読は左の線でも示す。語・線・背景の3つで、色だけに頼らない。 */
	.notifications li.unread {
		border-left-color: var(--color-accent);
		background: var(--color-bg);
	}

	.badge {
		display: inline-block;
		margin-right: var(--space-2);
		padding: 0 var(--space-2);
		font-size: 0.75rem;
		font-weight: 700;
		color: var(--color-accent-text);
		background: var(--color-accent);
		border-radius: var(--radius);
	}

	.text {
		margin: 0;
	}

	.actor {
		font-weight: 600;
		color: inherit;
	}

	.quote {
		margin: var(--space-2) 0 0;
		padding-left: var(--space-3);
		border-left: var(--line);
		color: var(--color-text-muted);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}

	.foot {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-3);
		margin-top: var(--space-2);
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	button {
		min-height: 2.75rem;
		padding: var(--space-2) var(--space-4);
		font: inherit;
		color: var(--color-text);
		background: transparent;
		border: var(--line);
		border-radius: var(--radius);
		cursor: pointer;
	}

	button:disabled {
		cursor: progress;
	}

	/* 一覧の中では控えめに見せる。押せる領域は保つ。 */
	button.link {
		min-height: 2.75rem;
		padding: 0 var(--space-2);
		color: var(--color-text-muted);
		border-color: transparent;
		text-decoration: underline;
	}

	.error {
		color: var(--color-danger);
	}
</style>
