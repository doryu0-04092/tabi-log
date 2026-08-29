<script lang="ts">
	import {
		createComment,
		deleteComment,
		listComments,
		MAX_COMMENT_LENGTH,
		type Comment
	} from '$lib/api/reactions';
	import Avatar from '$lib/components/Avatar.svelte';

	let { postId }: { postId: number } = $props();

	type State =
		| { kind: 'loading' }
		| { kind: 'ready'; comments: Comment[]; nextCursor: string | null }
		| { kind: 'error'; message: string };

	let view = $state<State>({ kind: 'loading' });
	let loadingMore = $state(false);

	let draft = $state('');
	let sending = $state(false);
	let formError = $state('');

	// 削除は取り消せないため、どのコメントを消そうとしているかを持って一段挟む。
	let confirmingId = $state<number | null>(null);
	let deletingId = $state<number | null>(null);
	let deleteError = $state('');

	$effect(() => {
		void load(postId);
	});

	async function load(id: number) {
		try {
			const page = await listComments(id);
			view = { kind: 'ready', comments: page.comments, nextCursor: page.nextCursor ?? null };
		} catch {
			view = { kind: 'error', message: 'コメントを取得できませんでした' };
		}
	}

	async function loadMore() {
		if (view.kind !== 'ready' || !view.nextCursor || loadingMore) return;
		loadingMore = true;
		try {
			const page = await listComments(postId, view.nextCursor);
			view = {
				kind: 'ready',
				// 古い順なので、続きは後ろに足す。
				comments: [...view.comments, ...page.comments],
				nextCursor: page.nextCursor ?? null
			};
		} catch {
			// 追加読み込みの失敗で、既に表示している分まで消さない。
			view = { ...view };
		} finally {
			loadingMore = false;
		}
	}

	let trimmed = $derived(draft.trim());
	let tooLong = $derived(trimmed.length > MAX_COMMENT_LENGTH);
	let canSubmit = $derived(trimmed.length > 0 && !tooLong && !sending);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!canSubmit || view.kind !== 'ready') return;

		sending = true;
		formError = '';
		try {
			// **投稿したものはサーバーが返した値で足す。**
			// 作成日時と id はサーバーが決めるため、手元で作ると後の再取得でずれる。
			const created = await createComment(postId, trimmed);
			view = { ...view, comments: [...view.comments, created] };
			draft = '';
		} catch {
			// 入力は消さない。書き直しをやり直させることになる。
			formError = 'コメントを送信できませんでした。もう一度お試しください。';
		} finally {
			sending = false;
		}
	}

	async function remove(commentId: number) {
		if (view.kind !== 'ready' || deletingId !== null) return;
		deletingId = commentId;
		deleteError = '';
		try {
			await deleteComment(commentId);
			view = { ...view, comments: view.comments.filter((c) => c.id !== commentId) };
			confirmingId = null;
		} catch {
			deleteError = 'コメントを削除できませんでした';
		} finally {
			deletingId = null;
		}
	}

	/** 投稿日時を「2026年8月29日 12:00」の形にする。 */
	function formatCreatedAt(iso: string): string {
		const d = new Date(iso);
		const hh = String(d.getHours()).padStart(2, '0');
		const mm = String(d.getMinutes()).padStart(2, '0');
		return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日 ${hh}:${mm}`;
	}
</script>

<section aria-labelledby="comments-heading">
	<h2 id="comments-heading">コメント</h2>

	{#if view.kind === 'loading'}
		<p>読み込んでいます…</p>
	{:else if view.kind === 'error'}
		<p class="error" role="alert"><span aria-hidden="true">✕</span> {view.message}</p>
	{:else}
		{#if view.comments.length === 0}
			<p class="empty">まだコメントはありません。</p>
		{:else}
			<ul class="comments">
				{#each view.comments as comment (comment.id)}
					<li>
						<div class="head">
							<Avatar
								url={comment.author.avatarUrl}
								displayName={comment.author.displayName}
								size="small"
							/>
							<span class="name">{comment.author.displayName}</span>
							<span class="handle">@{comment.author.handle}</span>
							<time datetime={comment.createdAt}>{formatCreatedAt(comment.createdAt)}</time>
						</div>

						<p class="body">{comment.body}</p>

						<!--
							canDelete はサーバーが閲覧者ごとに決めた値をそのまま使う。
							画面側で条件を書き直すと、サーバーの判定とずれる。
							なお表示を隠しても防御にはならない。拒否はサーバーが行う。
						-->
						{#if comment.canDelete}
							{#if confirmingId === comment.id}
								<p class="confirm" role="alert">このコメントを削除します。取り消せません。</p>
								<div class="actions">
									<button
										type="button"
										class="danger"
										onclick={() => remove(comment.id)}
										disabled={deletingId !== null}
									>
										{deletingId === comment.id ? '削除しています…' : '削除する'}
									</button>
									<button
										type="button"
										onclick={() => (confirmingId = null)}
										disabled={deletingId !== null}
									>
										やめる
									</button>
								</div>
							{:else}
								<div class="actions">
									<button
										type="button"
										onclick={() => {
											confirmingId = comment.id;
											deleteError = '';
										}}
									>
										削除する
									</button>
								</div>
							{/if}
						{/if}
					</li>
				{/each}
			</ul>
		{/if}

		{#if deleteError}
			<p class="error" role="alert"><span aria-hidden="true">✕</span> {deleteError}</p>
		{/if}

		{#if view.nextCursor}
			<button type="button" class="more" onclick={loadMore} disabled={loadingMore}>
				{loadingMore ? '読み込んでいます…' : '古いコメントをさらに読み込む'}
			</button>
		{/if}

		<form onsubmit={submit}>
			<!--
				label を必ず結び付ける。placeholder は入力すると消えるため、
				何を書く欄なのかが分からなくなる。
			-->
			<label for="comment-body">コメントを書く</label>
			<textarea
				id="comment-body"
				bind:value={draft}
				rows="3"
				maxlength={MAX_COMMENT_LENGTH * 2}
				aria-describedby="comment-count"
				disabled={sending}></textarea>

			<!--
				残り字数は色ではなく数で示し、aria-live で読み上げに乗せる。
				polite にしているのは、1文字ごとに割り込ませないためである。
			-->
			<p id="comment-count" class="count" class:over={tooLong} aria-live="polite">
				{trimmed.length} / {MAX_COMMENT_LENGTH} 文字
				{#if tooLong}（{trimmed.length - MAX_COMMENT_LENGTH}文字ぶん多すぎます）{/if}
			</p>

			{#if formError}
				<p class="error" role="alert"><span aria-hidden="true">✕</span> {formError}</p>
			{/if}

			<button type="submit" class="submit" disabled={!canSubmit}>
				{sending ? '送信しています…' : '送信する'}
			</button>
		</form>
	{/if}
</section>

<style>
	section {
		margin-top: var(--space-6);
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
	}

	h2 {
		margin-top: 0;
		font-size: 1.125rem;
	}

	.empty {
		color: var(--color-text-muted);
	}

	.comments {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		list-style: none;
		margin: 0;
		padding: 0;
	}

	.comments li {
		padding: var(--space-3);
		background: var(--color-surface);
		border-radius: var(--radius);
	}

	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: var(--space-2);
	}

	.name {
		font-weight: 600;
	}

	.handle,
	.head time {
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	.head time {
		margin-left: auto;
	}

	.body {
		margin: var(--space-2) 0 0;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}

	.confirm {
		margin: var(--space-2) 0 0;
		color: var(--color-danger);
	}

	.actions {
		display: flex;
		gap: var(--space-2);
		margin-top: var(--space-2);
	}

	button {
		min-height: 2.75rem;
		padding: var(--space-2) var(--space-4);
		font: inherit;
		color: var(--color-text);
		background: transparent;
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		cursor: pointer;
	}

	button:disabled {
		cursor: not-allowed;
		opacity: 0.6;
	}

	button.danger {
		color: var(--color-accent-text);
		background: var(--color-danger);
		border-color: var(--color-danger);
	}

	.more {
		width: 100%;
		margin-top: var(--space-4);
		background: var(--color-surface);
	}

	form {
		margin-top: var(--space-6);
	}

	label {
		display: block;
		margin-bottom: var(--space-2);
		font-weight: 600;
	}

	textarea {
		display: block;
		width: 100%;
		padding: var(--space-3);
		font: inherit;
		color: var(--color-text);
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		resize: vertical;
	}

	.count {
		margin: var(--space-2) 0 0;
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	.count.over {
		color: var(--color-danger);
		font-weight: 600;
	}

	.submit {
		margin-top: var(--space-3);
		color: var(--color-accent-text);
		background: var(--color-accent);
		border-color: var(--color-accent);
		font-weight: 600;
	}

	.error {
		color: var(--color-danger);
	}
</style>
