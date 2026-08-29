<script lang="ts">
	import { resolve } from '$app/paths';
	import type { components } from '$lib/api/gen';
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
		linkToDetail = true
	}: { post: Post; linkToDetail?: boolean } = $props();

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
			<span class="name">{post.author.displayName}</span>
			<span class="handle">@{post.author.handle}</span>
		</a>
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
						alt は投稿時に必須として入力させたものをそのまま使う。
						空にすると画像が見えない利用者に何も伝わらない。
						srcset で一覧と拡大の画質を出し分ける。
					-->
					<img
						src={m.thumbUrl}
						srcset="{m.thumbUrl} 320w, {m.mediumUrl} 1080w"
						sizes="(max-width: 40rem) 100vw, 40rem"
						alt={m.altText}
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
			<a class="photos" href={resolve('/posts/[postId]', { postId: String(post.id) })}
				>{@render photos()}</a
			>
		{:else}
			<div class="photos">{@render photos()}</div>
		{/if}
	{/if}

	<div class="meta">
		<!-- 地名は「どこへ」の答えであり、この SNS の中核。写真の直下に置く。 -->
		<a class="prefecture" href={resolve('/')}>{post.prefecture.name}</a>
		{#if post.spotName}
			<span class="spot">{post.spotName}</span>
		{/if}
		<!-- 訪問日は投稿日と別の軸。「いつ行ったか」を明示する。 -->
		<time class="visited" datetime={post.visitedOn}>
			訪問 {formatVisitedOn(post.visitedOn)}
		</time>
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
		<span><span aria-hidden="true">💬</span> コメント {post.commentCount}件</span>
		{#if linkToDetail}
			<a href={resolve('/posts/[postId]', { postId: String(post.id) })}>詳細を見る</a>
		{/if}
	</footer>
</article>

<style>
	.card {
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		overflow: hidden;
		background: var(--color-bg);
	}

	header {
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

	.photo-list {
		display: grid;
		gap: 2px;
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

	.photo-list img {
		display: block;
		width: 100%;
		/* 縦横比を固定して、読み込み中に文字が飛び跳ねるのを防ぐ。 */
		aspect-ratio: 4 / 3;
		object-fit: cover;
		background: var(--color-surface);
	}

	.photo-list[data-count='1'] img {
		aspect-ratio: 3 / 2;
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

	footer {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-4);
		align-items: center;
		margin-top: var(--space-3);
		padding: var(--space-3) var(--space-4);
		border-top: 1px solid var(--color-border);
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	footer a {
		margin-left: auto;
	}
</style>
