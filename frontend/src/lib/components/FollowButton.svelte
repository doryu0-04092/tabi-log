<script lang="ts">
	import { followUser, unfollowUser } from '$lib/api/users';

	let {
		handle,
		displayName,
		following: serverFollowing,
		onchange
	}: {
		handle: string;
		displayName: string;
		following: boolean;
		/** 件数を持っている画面が数字を合わせられるようにする。 */
		onchange?: (following: boolean) => void;
	} = $props();

	/**
	 * 押した結果をここに持つ。
	 *
	 * **null の間はサーバーから来た値をそのまま映す。** 受け取った値を
	 * 変数へ複製すると、親がプロフィールを取り直したときに古い状態が残る。
	 */
	let local = $state<boolean | null>(null);
	let following = $derived(local ?? serverFollowing);
	let sending = $state(false);
	let failed = $state(false);

	/**
	 * フォローを切り替える。
	 *
	 * **応答を待たずに見た目を変える。** 押してから数百ミリ秒あとに変わると、
	 * 押せていないと思って二度押しすることになる。失敗したら元に戻し、
	 * 何が起きたかを文字で伝える。
	 */
	async function toggle() {
		if (sending) return;

		const previous = local;
		const next = !following;
		local = next;
		sending = true;
		failed = false;

		try {
			await (next ? followUser(handle) : unfollowUser(handle));
			onchange?.(next);
		} catch {
			local = previous;
			failed = true;
		} finally {
			sending = false;
		}
	}
</script>

<!--
	**ボタンの文言に相手の名前を入れる。** 一覧では同じ「フォロー」ボタンが
	何個も並ぶため、読み上げだけではどれを押すのか区別できない。
	見た目には aria-hidden で隠した名前は出さず、視覚的なラベルは短いままにする。
-->
<button
	type="button"
	class="follow"
	class:on={following}
	aria-pressed={following}
	onclick={toggle}
	disabled={sending}
>
	<span aria-hidden="true">{following ? 'フォロー中' : 'フォローする'}</span>
	<span class="visually-hidden">
		{displayName}を{following ? 'フォロー中。押すと解除します' : 'フォローする'}
	</span>
</button>

{#if failed}
	<span class="failed" role="alert">切り替えられませんでした</span>
{/if}

<style>
	.follow {
		/* 指で押す領域を確保する。 */
		min-height: 2.75rem;
		padding: var(--space-2) var(--space-4);
		font: inherit;
		font-weight: 600;
		color: var(--color-accent-text);
		background: var(--color-accent);
		border: 1px solid var(--color-accent);
		border-radius: var(--radius);
		cursor: pointer;
	}

	/* フォロー中は「押した状態」を、色だけでなく枠線の有無でも示す。 */
	.follow.on {
		color: var(--color-text);
		background: transparent;
		border-color: var(--color-border);
	}

	.follow:disabled {
		cursor: progress;
	}

	.failed {
		margin-left: var(--space-2);
		color: var(--color-danger);
	}
</style>
