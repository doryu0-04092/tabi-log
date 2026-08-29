<script lang="ts">
	import type { Post } from '$lib/api/posts';

	/*
	 * 新しい投稿が届いたことを知らせる帯。
	 *
	 * **勝手に差し込まない。** 読んでいる最中に上へ投稿が挿入されると、
	 * 位置がずれて読んでいた場所を見失う。知らせるだけにして、
	 * 反映するかどうかは本人に決めてもらう。
	 *
	 * ---
	 *
	 * **WebSocket ではなく 30 秒間隔のポーリングにしている。** 理由は2つ。
	 *
	 *   1. **全体タイムラインの更新は全利用者に配る内容になる。**
	 *      接続を維持したうえで投稿のたびに全員へ配ると、
	 *      利用者数に対して通信量が二乗で効いてくる
	 *   2. 常時反映は読んでいる側にとって鬱陶しい。実際の SNS も
	 *      「新しい投稿があります」を出すだけで、自動では差し込まない
	 *
	 * 代償として、**最大 30 秒の遅れが出る。** タイムラインの用途では
	 * 問題にならないと判断している。
	 */

	type Props = {
		/** 今表示している中でいちばん新しい投稿の ID。これより新しいものを数える。 */
		newestId: number | undefined;
		/** 一覧の先頭ページを取りに行く。呼び出し側がどのフィードかを決める。 */
		fetchLatest: () => Promise<{ posts: Post[] }>;
		/** 帯を押したときの処理。ふつうは再読み込みして先頭へ戻す。 */
		onApply: () => void;
		/** ポーリングを行うか。未ログインや読み込み失敗中は止める。 */
		enabled?: boolean;
	};

	let { newestId, fetchLatest, onApply, enabled = true }: Props = $props();

	const INTERVAL_MS = 30_000;

	let count = $state(0);

	async function check() {
		// **画面が見えていないときは問い合わせない。**
		// 開きっぱなしのタブが裏で 30 秒ごとに叩き続けるのを防ぐ。
		if (typeof document !== 'undefined' && document.hidden) return;
		if (newestId === undefined) return;

		try {
			const feed = await fetchLatest();
			const base = newestId;
			count = feed.posts.filter((p) => p.id > base).length;
		} catch {
			// **失敗しても表示は変えない。** 一時的な失敗で
			// 出ていた帯が消えると、あったはずの新着が無かったことになる。
		}
	}

	$effect(() => {
		if (!enabled || newestId === undefined) return;

		const timer = setInterval(() => void check(), INTERVAL_MS);

		// 裏に回っていた間の分を、戻ってきた時点で取りに行く。
		const onVisible = () => {
			if (!document.hidden) void check();
		};
		document.addEventListener('visibilitychange', onVisible);

		return () => {
			clearInterval(timer);
			document.removeEventListener('visibilitychange', onVisible);
		};
	});

	// 反映したら数え直しの基準が変わるので、帯を消す。
	$effect(() => {
		void newestId;
		count = 0;
	});

	function apply() {
		count = 0;
		onApply();
	}
</script>

{#if count > 0}
	<!--
		**role="status" で読み上げる。** 見えている人には帯が出るが、
		そうでない人には何も起きていないことになる。
		alert ではなく status にしているのは、割り込む必要が無いため。
	-->
	<div class="notice" role="status">
		<button type="button" onclick={apply}>
			<span aria-hidden="true">↑</span>
			新しい投稿が {count}{count >= 20 ? '件以上' : '件'} あります
		</button>
	</div>
{/if}

<style>
	.notice {
		/*
		 * **スクロールしても見えるように上へ貼り付ける。**
		 * 流れて消えると、知らせた意味が無くなる。
		 */
		position: sticky;
		top: var(--space-2);
		z-index: 5;
		display: flex;
		justify-content: center;
		margin-bottom: var(--space-4);
	}

	button {
		padding: var(--space-2) var(--space-6);
		font: inherit;
		font-weight: 700;
		color: var(--color-accent-text);
		background: var(--color-accent);
		border: var(--line-strong);
		border-radius: 999px;
		box-shadow: var(--shadow-hard-sm);
		cursor: pointer;
	}

	button:active {
		transform: translate(3px, 3px);
		box-shadow: none;
	}
</style>
