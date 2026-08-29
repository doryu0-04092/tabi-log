<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { listPrefectures, searchPosts, type Post, type Prefecture } from '$lib/api/posts';
	import { searchUsers, type UserSummary } from '$lib/api/users';
	import { session } from '$lib/auth/session.svelte';
	import FollowButton from '$lib/components/FollowButton.svelte';
	import PostCard from '$lib/components/PostCard.svelte';

	type Mode = 'posts' | 'users';
	type Sort = 'latest' | 'popular';

	type Results =
		| { kind: 'idle' }
		| { kind: 'loading' }
		| { kind: 'posts'; posts: Post[]; nextCursor: string | null }
		| { kind: 'users'; users: UserSummary[]; nextCursor: string | null }
		| { kind: 'error'; message: string };

	let results = $state<Results>({ kind: 'idle' });
	let loadingMore = $state(false);
	let prefectures = $state<Prefecture[]>([]);

	/**
	 * 検索条件は URL に持たせる。
	 *
	 * **画面の中だけに持つと、結果を人に渡せない。** 「この条件で見て」と
	 * リンクを送れることと、戻る操作で前の条件に戻れることの両方が要る。
	 */
	let params = $derived(page.url.searchParams);
	let mode = $derived<Mode>(params.get('mode') === 'users' ? 'users' : 'posts');
	let keyword = $derived(params.get('q') ?? '');
	let prefectureCode = $derived(params.get('prefectureCode') ?? '');
	let tag = $derived(params.get('tag') ?? '');
	let sort = $derived<Sort>(params.get('sort') === 'popular' ? 'popular' : 'latest');

	// 入力欄の値。送信するまで URL は変えない。
	// 打つたびに履歴が増えると、戻る操作が使い物にならなくなる。
	let keywordInput = $state('');
	let prefectureInput = $state('');
	let tagInput = $state('');
	let modeInput = $state<Mode>('posts');
	let sortInput = $state<Sort>('latest');
	let formError = $state('');

	// URL が変わったら入力欄も合わせる（戻る操作やリンクで開いた場合）。
	$effect(() => {
		keywordInput = keyword;
		prefectureInput = prefectureCode;
		tagInput = tag;
		modeInput = mode;
		sortInput = sort;
	});

	$effect(() => {
		if (session.restored && session.isAuthenticated && prefectures.length === 0) {
			void loadPrefectures();
		}
	});

	$effect(() => {
		if (!session.restored || !session.isAuthenticated) return;
		// 条件が1つも無いときは検索しない。全件を出しても発見にはならない。
		if (!keyword && !prefectureCode && !tag && sort === 'latest' && mode === 'posts') {
			results = { kind: 'idle' };
			return;
		}
		void run();
	});

	async function loadPrefectures() {
		try {
			prefectures = await listPrefectures();
		} catch {
			// 都道府県が取れなくてもキーワード検索はできる。画面全体は止めない。
		}
	}

	async function run() {
		results = { kind: 'loading' };
		try {
			if (mode === 'users') {
				const page = await searchUsers(keyword);
				results = { kind: 'users', users: page.users, nextCursor: page.nextCursor ?? null };
			} else {
				const feed = await searchPosts({ q: keyword, prefectureCode, tag, sort });
				results = { kind: 'posts', posts: feed.posts, nextCursor: feed.nextCursor ?? null };
			}
		} catch {
			results = { kind: 'error', message: '検索できませんでした' };
		}
	}

	async function loadMore() {
		if (loadingMore) return;
		loadingMore = true;
		try {
			if (results.kind === 'users' && results.nextCursor) {
				const p = await searchUsers(keyword, results.nextCursor);
				results = {
					kind: 'users',
					users: [...results.users, ...p.users],
					nextCursor: p.nextCursor ?? null
				};
			} else if (results.kind === 'posts' && results.nextCursor) {
				const feed = await searchPosts({
					q: keyword,
					prefectureCode,
					tag,
					sort,
					cursor: results.nextCursor
				});
				results = {
					kind: 'posts',
					posts: [...results.posts, ...feed.posts],
					nextCursor: feed.nextCursor ?? null
				};
			}
		} catch {
			// 追加読み込みの失敗で、既に表示している分まで消さない。
		} finally {
			loadingMore = false;
		}
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		formError = '';

		const q = keywordInput.trim();
		// **1文字では全文検索の索引に当たらない。** サーバーも弾くが、
		// 送る前に理由を伝えたほうが早い。
		if (q.length === 1) {
			formError = 'キーワードは2文字以上で入力してください。';
			return;
		}
		if (modeInput === 'users' && q.length < 2) {
			formError = '利用者を探すにはキーワードを2文字以上入力してください。';
			return;
		}

		// 条件を問い合わせ文字列に組み直す。指定の無いものは載せない。
		// 空の項目まで並ぶと、共有したリンクが読みづらくなる。
		const pairs: [string, string][] = [];
		if (q) pairs.push(['q', q]);
		if (modeInput === 'users') {
			pairs.push(['mode', 'users']);
		} else {
			if (prefectureInput) pairs.push(['prefectureCode', prefectureInput]);
			if (tagInput.trim()) pairs.push(['tag', tagInput.trim()]);
			if (sortInput === 'popular') pairs.push(['sort', 'popular']);
		}

		const query = pairs
			.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
			.join('&');

		// **行き先は resolve('/explore') で解決している。**
		// eslint の no-navigation-without-resolve は goto の引数が resolve()
		// そのものであることを求めるが、resolve は問い合わせ文字列を受け取れない。
		// 解決済みのパスに条件を足すだけなので、ここは意図して外す。
		//
		// keepFocus を付けるのは、検索のたびにフォーカスが先頭へ飛ぶと
		// キーボードで条件を直しづらくなるためである。
		// eslint-disable-next-line svelte/no-navigation-without-resolve
		await goto(`${resolve('/explore')}${query ? `?${query}` : ''}`, { keepFocus: true });
	}
</script>

<svelte:head>
	<title>発見 — tabi-log</title>
</svelte:head>

{#if !session.restored}
	<p>読み込んでいます…</p>
{:else if !session.isAuthenticated}
	<p>発見を使うには <a href={resolve('/login')}>ログイン</a> が必要です。</p>
{:else}
	<h1>発見</h1>

	<form onsubmit={submit}>
		<div class="field">
			<label for="q">キーワード</label>
			<input
				id="q"
				type="search"
				bind:value={keywordInput}
				aria-describedby="q-hint"
				autocomplete="off"
			/>
			<p class="hint" id="q-hint">本文・スポット名を探します。2文字以上。</p>
		</div>

		<fieldset>
			<legend>探すもの</legend>
			<label><input type="radio" bind:group={modeInput} value="posts" /> 投稿</label>
			<label><input type="radio" bind:group={modeInput} value="users" /> 利用者</label>
		</fieldset>

		<!-- 利用者を探すときは投稿向けの絞り込みを出さない。効かない入力欄は迷いのもとになる。 -->
		{#if modeInput === 'posts'}
			<div class="field">
				<label for="prefecture">都道府県</label>
				<select id="prefecture" bind:value={prefectureInput}>
					<option value="">指定しない</option>
					{#each prefectures as p (p.code)}
						<option value={p.code}>{p.name}</option>
					{/each}
				</select>
			</div>

			<div class="field">
				<label for="tag">タグ</label>
				<input id="tag" type="text" bind:value={tagInput} autocomplete="off" />
			</div>

			<fieldset>
				<legend>並び順</legend>
				<label><input type="radio" bind:group={sortInput} value="latest" /> 新着</label>
				<label><input type="radio" bind:group={sortInput} value="popular" /> いいねの多い順</label>
			</fieldset>
		{/if}

		{#if formError}
			<p class="error" role="alert"><span aria-hidden="true">✕</span> {formError}</p>
		{/if}

		<button type="submit">探す</button>
	</form>

	<section aria-labelledby="results-heading">
		<h2 id="results-heading">結果</h2>

		{#if results.kind === 'idle'}
			<p class="hint">条件を入れて「探す」を押してください。</p>
		{:else if results.kind === 'loading'}
			<p>読み込んでいます…</p>
		{:else if results.kind === 'error'}
			<p class="error" role="alert"><span aria-hidden="true">✕</span> {results.message}</p>
		{:else if results.kind === 'posts'}
			{#if results.posts.length === 0}
				<!-- 0件はエラーではない。条件の見直し方を添える。 -->
				<p class="empty">条件に合う投稿は見つかりませんでした。条件を減らして試してください。</p>
			{:else}
				<ul class="feed">
					{#each results.posts as post (post.id)}
						<li><PostCard {post} /></li>
					{/each}
				</ul>
				{#if results.nextCursor}
					<button type="button" class="more" onclick={loadMore} disabled={loadingMore}>
						{loadingMore ? '読み込んでいます…' : 'さらに読み込む'}
					</button>
				{/if}
			{/if}
		{:else if results.users.length === 0}
			<p class="empty">条件に合う利用者は見つかりませんでした。</p>
		{:else}
			<ul class="users">
				{#each results.users as user (user.id)}
					<li>
						<a class="identity" href={resolve('/users/[handle]', { handle: user.handle })}>
							<span class="name">{user.displayName}</span>
							<span class="handle">@{user.handle}</span>
						</a>
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
			{#if results.nextCursor}
				<button type="button" class="more" onclick={loadMore} disabled={loadingMore}>
					{loadingMore ? '読み込んでいます…' : 'さらに読み込む'}
				</button>
			{/if}
		{/if}
	</section>
{/if}

<style>
	h1 {
		margin-top: 0;
	}

	form {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		padding: var(--space-4);
		background: var(--color-surface);
		border-radius: var(--radius);
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	label {
		font-weight: 600;
	}

	input[type='search'],
	input[type='text'],
	select {
		padding: var(--space-3);
		font: inherit;
		color: var(--color-text);
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
	}

	fieldset {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-4);
		align-items: center;
		margin: 0;
		padding: var(--space-3);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
	}

	legend {
		padding: 0 var(--space-2);
		font-weight: 600;
	}

	fieldset label {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-weight: 400;
	}

	.hint {
		margin: 0;
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	button[type='submit'] {
		min-height: 2.75rem;
		padding: var(--space-3) var(--space-4);
		font: inherit;
		font-weight: 600;
		color: var(--color-accent-text);
		background: var(--color-accent);
		border: 1px solid var(--color-accent);
		border-radius: var(--radius);
		cursor: pointer;
	}

	section {
		margin-top: var(--space-6);
	}

	h2 {
		font-size: 1.125rem;
	}

	.empty {
		color: var(--color-text-muted);
	}

	.feed {
		display: flex;
		flex-direction: column;
		gap: var(--space-6);
		list-style: none;
		margin: 0;
		padding: 0;
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

	.more {
		display: block;
		width: 100%;
		min-height: 2.75rem;
		margin-top: var(--space-6);
		padding: var(--space-3);
		font: inherit;
		color: var(--color-text);
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		cursor: pointer;
	}

	.more:disabled {
		cursor: progress;
	}

	.error {
		margin: 0;
		color: var(--color-danger);
	}
</style>
