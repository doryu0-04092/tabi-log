<script lang="ts">
	/*
	 * 一覧の続きを読み込む導線。
	 *
	 * **画面下端に近づいたら自動で次を読む（無限スクロール）。**
	 * 「さらに読み込む」ボタンだけの方式は、SNS のタイムラインとしては
	 * 手数が多すぎる。スクロールで読み続けるのが自然な使われ方である。
	 *
	 * ---
	 *
	 * **ボタンは消さない。** 当初は逆の判断（無限スクロールにしない）を
	 * していた。その理由は「キーボードと読み上げソフトでは終わりが
	 * 分からず、末尾へ到達できなくなる」というものだったが、
	 * **自動読み込みとボタンは排他ではない。** 両方置けば、
	 *
	 *   - スクロールで読む人は押さずに読み進められる
	 *   - キーボードの人は Tab で辿り着けるボタンを自分の意思で押せる
	 *
	 * となり、どちらの利用者も失わない。読み込み中と完了は
	 * aria-live で読み上げる。
	 *
	 * **自動読み込みは監視要素（番兵）が画面に入ったかどうかで判定する。**
	 * スクロール量を数えると、要素の高さやズーム倍率で挙動が変わる。
	 * Intersection Observer はブラウザ側が交差を判定するため、その差が出ない。
	 */

	type Props = {
		/** 続きがあるか。false なら何も描かない。 */
		hasMore: boolean;
		/** 読み込み中か。多重に呼ばないための状態。 */
		loading: boolean;
		/** 続きを読む。呼び出し側が cursor を持つ。 */
		onLoadMore: () => void;
		/** ボタンの文言。既定は一覧向けの汎用文言。 */
		label?: string;
		/**
		 * 自動読み込みを行うか。
		 *
		 * **コメントのように「古い方へ遡る」一覧では既定で切る。**
		 * 下へ進むほど古くなる一覧を勝手に伸ばすと、読んでいた位置が
		 * 押し下げられる。
		 */
		auto?: boolean;
	};

	let { hasMore, loading, onLoadMore, label = 'さらに読み込む', auto = true }: Props = $props();

	let sentinel = $state<HTMLDivElement | null>(null);

	$effect(() => {
		if (!auto || !sentinel || !hasMore) return;

		// **古いブラウザでは自動読み込みだけを落とす。**
		// ボタンは常に描いてあるので、機能そのものは失われない。
		if (typeof IntersectionObserver === 'undefined') return;

		const target = sentinel;
		const observer = new IntersectionObserver(
			(entries) => {
				// loading の判定は「今の値」で行う必要がある。
				// 交差の通知は $effect の外から来るため、props を直接見る。
				if (entries.some((e) => e.isIntersecting) && hasMore && !loading) {
					onLoadMore();
				}
			},
			// **末尾に着く前に読み始める。** 到達してから取りに行くと、
			// 読むものが無い時間が必ず挟まる。
			{ rootMargin: '400px 0px' }
		);
		observer.observe(target);
		return () => observer.disconnect();
	});
</script>

{#if hasMore}
	<!--
		番兵。高さを持たない要素は交差判定が不安定なので 1px 持たせる。
		読み上げ対象ではないので aria-hidden にする。
	-->
	<div class="sentinel" bind:this={sentinel} aria-hidden="true"></div>

	<button type="button" onclick={onLoadMore} disabled={loading}>
		{loading ? '読み込んでいます…' : label}
	</button>

	<!--
		読み込みの状態を読み上げる。**ボタンの文言とは別に持つ。**
		自動読み込みではボタンにフォーカスが無く、文言が変わっても伝わらない。
	-->
	<p class="status" role="status" aria-live="polite">
		{loading ? '続きを読み込んでいます' : ''}
	</p>
{/if}

<style>
	.sentinel {
		height: 1px;
	}

	button {
		display: block;
		width: 100%;
		min-height: 2.75rem;
		margin-top: var(--space-6);
		padding: var(--space-3);
		font: inherit;
		font-weight: 600;
		color: var(--color-text);
		background: var(--color-surface);
		border: var(--line);
		border-radius: var(--radius);
		box-shadow: var(--shadow-hard-sm);
		cursor: pointer;
	}

	button:disabled {
		cursor: progress;
	}

	/* 押した感触を出す。影の分だけ動かす。 */
	button:not(:disabled):active {
		transform: translate(3px, 3px);
		box-shadow: none;
	}

	.status {
		margin: var(--space-2) 0 0;
		text-align: center;
		color: var(--color-text-muted);
		font-size: 0.875rem;
		/* 空の間に高さを取らせない。 */
		min-height: 0;
	}
</style>
