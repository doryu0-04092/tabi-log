<script lang="ts">
	import { likePost, unlikePost } from '$lib/api/reactions';

	let {
		postId,
		liked: serverLiked,
		count: serverCount
	}: { postId: number; liked: boolean; count: number } = $props();

	/**
	 * 押した結果をここに持つ。
	 *
	 * **null の間はサーバーから来た値をそのまま映す。** 受け取った値を
	 * 変数へ複製すると、親が投稿を取り直したときに古い数字が残る。
	 * 押した後だけこちらが優先する形にして、両方を正しく扱う。
	 */
	let local = $state<{ liked: boolean; count: number } | null>(null);

	let liked = $derived(local?.liked ?? serverLiked);
	let count = $derived(local?.count ?? serverCount);
	let sending = $state(false);
	let failed = $state(false);

	/**
	 * いいねを切り替える。
	 *
	 * **応答を待たずに見た目を変える。** 押してから数百ミリ秒あとに数字が動くと、
	 * 押せていないと思って二度押しすることになる。失敗したら元に戻し、
	 * 何が起きたかを文字で伝える（色や見た目の差だけにしない）。
	 */
	async function toggle() {
		if (sending) return;

		const previous = local;
		const next = { liked: !liked, count: count + (liked ? -1 : 1) };
		local = next;
		sending = true;
		failed = false;

		try {
			await (next.liked ? likePost(postId) : unlikePost(postId));
		} catch {
			local = previous;
			failed = true;
		} finally {
			sending = false;
		}
	}
</script>

<!--
	aria-pressed で「押した状態」を伝える。色の違いだけでは、
	色を判別しない環境で今どちらなのか分からない。
	件数は語を添えて読み上げられるようにする。
-->
<button
	type="button"
	class="like"
	class:on={liked}
	aria-pressed={liked}
	onclick={toggle}
	disabled={sending}
>
	<span class="mark" aria-hidden="true">{liked ? '♥' : '♡'}</span>
	いいね {count}件
</button>

{#if failed}
	<!--
		role="alert" で、視線がボタンから離れていても読み上げられる。
		見た目を元に戻すだけだと「押したのに戻った」理由が伝わらない。
	-->
	<span class="failed" role="alert">切り替えられませんでした</span>
{/if}

<style>
	.like {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		/* 指で押す領域を確保する。文字の大きさに合わせて縮めない。 */
		min-height: 2.75rem;
		padding: var(--space-2) var(--space-3);
		font: inherit;
		color: var(--color-text-muted);
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius);
		cursor: pointer;
	}

	.like:hover:not(:disabled) {
		background: var(--color-surface);
	}

	.like:disabled {
		cursor: progress;
	}

	.like.on {
		color: var(--color-danger);
		font-weight: 600;
	}

	.mark {
		font-size: 1.125rem;
		line-height: 1;
	}

	.failed {
		color: var(--color-danger);
	}
</style>
