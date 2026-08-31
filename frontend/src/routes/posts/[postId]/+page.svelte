<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { ApiError } from '$lib/api/client';
	import { deletePost, getPost, type Post } from '$lib/api/posts';
	import { session } from '$lib/auth/session.svelte';
	import BackLink from '$lib/components/BackLink.svelte';
	import CommentSection from '$lib/components/CommentSection.svelte';
	import PostCard from '$lib/components/PostCard.svelte';

	type State =
		{ kind: 'loading' } | { kind: 'ready'; post: Post } | { kind: 'error'; message: string };

	let view = $state<State>({ kind: 'loading' });
	let deleting = $state(false);
	let confirming = $state(false);

	$effect(() => {
		if (session.restored && session.isAuthenticated) {
			void load(page.params.postId);
		}
	});

	/**
	 * コメントの増減を投稿のカウンタへ反映する。
	 *
	 * **サーバーは正しく数えている。** ずれるのは画面の中だけで、
	 * 再読み込みすれば直る。それでも直すのは、コメントした直後に
	 * 「0件」と出るのが**壊れているように見える**ためである。
	 *
	 * 投稿を取り直さないのは、そのために全体を読み直すのが
	 * 見合わないからである。増減はここで分かっている。
	 */
	function changeCommentCount(delta: number) {
		if (view.kind !== 'ready') return;
		view = {
			...view,
			post: { ...view.post, commentCount: Math.max(0, view.post.commentCount + delta) }
		};
	}

	async function load(postId: string | undefined) {
		if (!postId) return;
		try {
			view = { kind: 'ready', post: await getPost(postId) };
		} catch (e) {
			view = {
				kind: 'error',
				message:
					e instanceof ApiError && e.status === 404
						? '投稿が見つかりません'
						: '投稿を取得できませんでした'
			};
		}
	}

	/** 自分の投稿かどうか。**表示の出し分けのためだけに使う。** */
	let isMine = $derived(view.kind === 'ready' && session.user?.id === view.post.author.id);

	async function handleDelete() {
		if (view.kind !== 'ready' || deleting) return;
		deleting = true;
		try {
			await deletePost(view.post.id);
			await goto(resolve('/'));
		} catch {
			view = { kind: 'error', message: '削除できませんでした' };
		} finally {
			deleting = false;
		}
	}
</script>

<svelte:head>
	<title>
		{view.kind === 'ready' ? `${view.post.prefecture.name}の投稿 — tabi-log` : 'tabi-log'}
	</title>
</svelte:head>

{#if !session.restored}
	<p>読み込んでいます…</p>
{:else if !session.isAuthenticated}
	<p>この投稿を見るには <a href={resolve('/login')}>ログイン</a> が必要です。</p>
{:else if view.kind === 'loading'}
	<p>読み込んでいます…</p>
{:else if view.kind === 'error'}
	<p class="error" role="alert"><span aria-hidden="true">✕</span> {view.message}</p>
	<p><a href={resolve('/')}>ホームへ戻る</a></p>
{:else}
	<nav aria-label="パンくず">
		<a href={resolve('/')}>ホーム</a> ／ {view.post.prefecture.name}の投稿
	</nav>

	<PostCard post={view.post} linkToDetail={false} />

	<CommentSection postId={view.post.id} onCountChange={changeCommentCount} />

	{#if isMine}
		<div class="owner-actions">
			{#if confirming}
				<!--
					削除は取り消せない。押し間違いで消えないよう一段挟む。
					ダイアログではなくその場に出すのは、フォーカスの移動を
					伴わない方が読み上げでも追いやすいためである。
				-->
				<p role="alert">この投稿を削除します。取り消せません。</p>
				<button type="button" class="danger" onclick={handleDelete} disabled={deleting}>
					{deleting ? '削除しています…' : '削除する'}
				</button>
				<button type="button" onclick={() => (confirming = false)} disabled={deleting}>
					やめる
				</button>
			{:else}
				<!-- **編集を先に置く。** 削除より使う頻度が高く、
				     並びが逆だと消す方に手が伸びる。 -->
				<a class="edit" href={resolve('/posts/[postId]/edit', { postId: String(view.post.id) })}>
					この投稿を編集する
				</a>
				<button type="button" onclick={() => (confirming = true)}>この投稿を削除する</button>
			{/if}
		</div>
	{/if}

	<BackLink href={resolve('/')} />
{/if}

<style>
	nav {
		margin-bottom: var(--space-4);
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	.error {
		color: var(--color-danger);
	}

	.owner-actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-3);
		margin-top: var(--space-6);
		padding-top: var(--space-4);
		border-top: var(--line);
	}

	/* リンクだが、隣の削除ボタンと同じ大きさに見せる。 */
	.edit {
		display: inline-flex;
		align-items: center;
		min-height: 2.75rem;
		padding: 0 var(--space-4);
		color: var(--color-text);
		background: var(--color-surface);
		border: var(--line);
		border-radius: var(--radius);
		text-decoration: none;
		font-weight: 600;
	}

	.owner-actions p {
		width: 100%;
		margin: 0;
		color: var(--color-danger);
	}

	.owner-actions button {
		padding: var(--space-2) var(--space-4);
		font: inherit;
		color: var(--color-text);
		background: transparent;
		border: var(--line);
		border-radius: var(--radius);
		cursor: pointer;
	}

	.owner-actions button.danger {
		color: var(--color-accent-text);
		background: var(--color-danger);
		border-color: var(--color-danger);
	}
</style>
