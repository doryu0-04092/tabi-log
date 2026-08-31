<script lang="ts">
	import { resolve } from '$app/paths';
	import type { components } from '$lib/api/gen';
	import Avatar from '$lib/components/Avatar.svelte';
	import FollowButton from '$lib/components/FollowButton.svelte';
	import LikeButton from '$lib/components/LikeButton.svelte';

	type Post = components['schemas']['Post'];

	let {
		post,
		/**
		 * 詳細への導線を出すかどうか。
		 *
		 * 同じカードをフィードと詳細の両方で使うため、詳細画面では
		 * **今いる場所への「詳細を見る」を出さない**。
		 * 行き先が現在地のリンクは、読み上げでも視覚でも混乱のもとになる。
		 */
		linkToDetail = true,
		/**
		 * フォローの導線を出すかどうか。
		 *
		 * **プロフィール画面では出さない。** 見出しに同じ相手のボタンが
		 * 既にあり、投稿の数だけ同じボタンが並ぶことになる。
		 */
		showFollow = true
	}: { post: Post; linkToDetail?: boolean; showFollow?: boolean } = $props();

	/**
	 * 訪問日を「2026年5月3日」の形にする。
	 *
	 * ISO の並び（2026-05-03）のままでも読めるが、日本語の画面では
	 * 桁の並びを目で追うことになる。日付は読み上げソフトのためにも
	 * <time datetime> で機械可読な値を併せて持たせる。
	 */
	function formatVisitedOn(iso: string): string {
		const [y, m, d] = iso.split('-');
		return `${Number(y)}年${Number(m)}月${Number(d)}日`;
	}
</script>

<article class="card">
	<header>
		<a class="author" href={resolve('/users/[handle]', { handle: post.author.handle })}>
			<Avatar url={post.author.avatarUrl} displayName={post.author.displayName} size="small" />
			<span class="name">{post.author.displayName}</span>
			<span class="handle">@{post.author.handle}</span>
		</a>

		<!--
			**カードからその場でフォローできるようにする。**
			プロフィールまで開かないとフォローできないと、
			フィードを見ている流れが途切れる。
			自分の投稿には出さない（押せない導線は迷いのもとになる）。
		-->
		{#if showFollow && post.author.isMe === false}
			<FollowButton
				handle={post.author.handle}
				displayName={post.author.displayName}
				following={post.author.isFollowing ?? false}
			/>
		{/if}
	</header>

	<!--
		写真は幅いっぱいに置く。カード型1カラムの要点は、
		写真を大きく見せつつ、その直下に地名と訪問日を添えることである。
	-->
	{#snippet photos()}
		<ul class="photo-list" data-count={Math.min(post.media.length, 4)}>
			{#each post.media as m (m.id)}
				<li>
					<!--
						**alt は空にする。** 画像ごとの説明は入力させない方針にしたため、
						説明として出せる内容が無い（2026-08-29 の判断）。

						属性ごと消さないのは、alt の無い img は HTML として不正であり、
						読み上げがファイル名を読み上げてしまうためである。
						alt="" は「読み上げる内容が無い」を表す正しい書き方である。

						srcset で一覧と拡大の画質を出し分ける。
					-->
					<img
						src={m.thumbUrl}
						srcset="{m.thumbUrl} 320w, {m.mediumUrl} 1080w"
						sizes="(max-width: 40rem) 100vw, 40rem"
						alt=""
						width={m.width}
						height={m.height}
						loading="lazy"
					/>
				</li>
			{/each}
		</ul>
	{/snippet}

	{#if post.media.length > 0}
		{#if linkToDetail}
			<!--
				**写真そのものを押せるようにする。** ただし alt が空なので、
				このリンクには読み上げられる文字が無い。行き先が分かるよう
				aria-label を付ける（付けないと「リンク」としか読まれない）。
			-->
			<a
				class="photos"
				href={resolve('/posts/[postId]', { postId: String(post.id) })}
				aria-label="{post.prefecture.name}の投稿の詳細を見る"
			>
				{@render photos()}
			</a>
		{:else}
			<div class="photos">{@render photos()}</div>
		{/if}
	{/if}

	<div class="meta">
		<!-- 地名は「どこへ」の答えであり、この SNS の中核。写真の直下に置く。 -->
		<a class="prefecture" href={resolve('/prefectures/[code]', { code: post.prefecture.code })}>
			{post.prefecture.name}
		</a>
		{#if post.spotName}
			<span class="spot">{post.spotName}</span>
		{/if}
		<!--
			訪問日は投稿日と別の軸。「いつ行ったか」を明示する。
			**任意なので、無い投稿では出さない。**
		-->
		{#if post.visitedOn}
			<time class="visited" datetime={post.visitedOn}>
				訪問 {formatVisitedOn(post.visitedOn)}
			</time>
		{/if}
	</div>

	<p class="body">{post.body}</p>

	{#if post.tags.length > 0}
		<ul class="tags">
			{#each post.tags as tag (tag)}
				<li>#{tag}</li>
			{/each}
		</ul>
	{/if}

	<footer>
		<!--
			数は記号だけでなく語でも示す。記号の意味が伝わらない利用者にも
			「いいね 12件」と読み上げられる。
		-->
		<LikeButton postId={post.id} liked={post.isLiked} count={post.likeCount} />
		<!--
			**いいねと並ぶ以上、コメントも押せるようにする。**
			片方だけが押せると、押せないほうも押せると思って押される。

			飛び先は写真をクリックしたときと同じ詳細画面。ただし
			コメント欄まで送る（一覧では読む・書く操作がそこにある）。

			詳細画面では自分自身へのリンクになるため、リンクにしない。
		-->
		{#if linkToDetail}
			<a
				class="comments"
				href="{resolve('/posts/[postId]', { postId: String(post.id) })}#comments-heading"
			>
				<span aria-hidden="true">💬</span> コメント {post.commentCount}件
			</a>
		{:else}
			<span><span aria-hidden="true">💬</span> コメント {post.commentCount}件</span>
		{/if}
		{#if linkToDetail}
			<a href={resolve('/posts/[postId]', { postId: String(post.id) })}>詳細を見る</a>
		{/if}
	</footer>
</article>

<style>
	/* **ぼかした影を使わない。** 濃い輪郭とずらしたベタで立体を出す。
	   これがレトロポスターの質感の要である。 */
	.card {
		border: var(--line-strong);
		border-radius: var(--radius);
		overflow: hidden;
		background: var(--color-bg);
		box-shadow: var(--shadow-hard);
	}

	header {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
	}

	.author {
		display: flex;
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

	.photos {
		display: block;
	}

	/* **画像の枠は枚数によらず同じ高さにする。**
	   1枚のときだけ大きくなると、フィードを流し読みするときに
	   カードごとに高さが跳ねて読みづらい。
	   枠の中を枚数で割る形にしてある。 */
	.photo-list {
		display: grid;
		grid-auto-rows: 1fr;
		gap: 2px;
		height: 18rem;
		background: var(--color-border);
		list-style: none;
		margin: 0;
		padding: 0;
		background: var(--color-border);
	}

	/* 1枚のときは大きく1枚、複数枚は等分に割る。
	   枚数ごとに割り方を変えるのは、どの枚数でも収まりが崩れないようにするため。 */
	.photo-list[data-count='2'],
	.photo-list[data-count='4'] {
		grid-template-columns: 1fr 1fr;
	}

	.photo-list[data-count='3'] {
		grid-template-columns: 2fr 1fr;
	}

	.photo-list[data-count='3'] li:first-child {
		grid-row: span 2;
	}

	/* 枠を埋める。大きな写真でも枠より大きくならない。 */
	.photo-list img {
		display: block;
		width: 100%;
		height: 100%;
		object-fit: cover;
		background: var(--color-surface);
	}

	.photo-list li {
		min-height: 0;
	}

	.meta {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: var(--space-2) var(--space-3);
		padding: var(--space-3) var(--space-4) 0;
	}

	.prefecture {
		font-weight: 700;
		color: var(--color-accent);
		text-decoration: none;
	}

	.spot {
		color: var(--color-text);
	}

	.visited {
		margin-left: auto;
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	.body {
		margin: var(--space-2) 0 0;
		padding: 0 var(--space-4);
		/* 改行を保ちつつ、長い語も折り返す。 */
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}

	.tags {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		list-style: none;
		margin: var(--space-3) 0 0;
		padding: 0 var(--space-4);
	}

	.tags li {
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	/* コメントへのリンク。**いいねのボタンと見た目を揃える。**
	   片方だけ下線が付いていると、別の種類の操作に見える。 */
	.comments {
		color: inherit;
		text-decoration: none;
	}

	.comments:hover,
	.comments:focus-visible {
		text-decoration: underline;
	}

	footer {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-4);
		align-items: center;
		margin-top: var(--space-3);
		padding: var(--space-3) var(--space-4);
		border-top: var(--line);
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	footer a {
		margin-left: auto;
	}
</style>
