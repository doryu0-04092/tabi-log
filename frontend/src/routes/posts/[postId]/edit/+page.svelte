<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { ApiError, NetworkError } from '$lib/api/client';
	import { getPost, listPrefectures, updatePost, type Post, type Prefecture } from '$lib/api/posts';
	import { session } from '$lib/auth/session.svelte';
	import BackLink from '$lib/components/BackLink.svelte';
	import '$lib/styles/forms.css';

	/*
	 * 投稿の編集。
	 *
	 * **画像は編集できない。** 差し替えを許すと、既に検証と EXIF 除去を
	 * 終えた画像を捨てて作り直す経路が要り、投稿の作成と同じ仕組みが
	 * 二重になる（requirements.md 3.2 で対象外としている）。
	 * 画面にもその旨を書き、「できるはずなのに見つからない」状態にしない。
	 *
	 * **入力欄は新規投稿と同じ並びにしてある。** 別の並びにすると、
	 * 同じことをする画面が2つあるように見える。
	 */

	type State =
		{ kind: 'loading' } | { kind: 'ready'; post: Post } | { kind: 'error'; message: string };

	let view = $state<State>({ kind: 'loading' });
	let prefectures = $state<Prefecture[]>([]);

	let body = $state('');
	let prefectureCode = $state('');
	let spotName = $state('');
	let visitedOn = $state('');
	let tagsInput = $state('');
	let error = $state('');
	let saving = $state(false);

	const today = new Date().toISOString().slice(0, 10);

	$effect(() => {
		if (session.restored && session.isAuthenticated) {
			void load(page.params.postId);
		}
	});

	async function load(postId: string | undefined) {
		if (!postId) return;
		try {
			const [post, list] = await Promise.all([getPost(postId), listPrefectures()]);
			prefectures = list;

			// **他人の投稿は編集させない。** サーバーも 403 を返すが、
			// 書ける形の画面を見せてから断るのは筋が悪い。
			if (post.author.id !== session.user?.id) {
				view = { kind: 'error', message: 'この投稿は編集できません' };
				return;
			}

			body = post.body;
			prefectureCode = post.prefecture.code;
			spotName = post.spotName ?? '';
			visitedOn = post.visitedOn ?? '';
			tagsInput = post.tags.join(' ');
			view = { kind: 'ready', post };
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

	/** 新規投稿と同じ解釈にする。ここだけ違うと利用者が迷う。 */
	function parseTags(raw: string): string[] {
		return raw
			.split(/[\s,、]+/)
			.map((t) => t.replace(/^#/, '').trim())
			.filter((t) => t !== '');
	}

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		if (view.kind !== 'ready' || saving) return;

		error = '';
		saving = true;
		try {
			await updatePost(view.post.id, {
				body,
				prefectureCode,
				spotName: spotName.trim() === '' ? null : spotName,
				// 空にすると訪問日を消す。旅行履歴からも外れる。
				visitedOn: visitedOn === '' ? null : visitedOn,
				tags: parseTags(tagsInput)
			});
			await goto(resolve('/posts/[postId]', { postId: String(view.post.id) }));
		} catch (e) {
			error =
				e instanceof ApiError || e instanceof NetworkError
					? e.message
					: '予期しないエラーが発生しました';
		} finally {
			saving = false;
		}
	}
</script>

<svelte:head>
	<title>投稿を編集する — tabi-log</title>
</svelte:head>

{#if !session.restored || view.kind === 'loading'}
	<p>読み込んでいます…</p>
{:else if !session.isAuthenticated}
	<p>編集するには <a href={resolve('/login')}>ログイン</a> が必要です。</p>
{:else if view.kind === 'error'}
	<p class="error" role="alert"><span aria-hidden="true">✕</span> {view.message}</p>
	<p><a href={resolve('/')}>ホームへ戻る</a></p>
{:else}
	<h1>投稿を編集する</h1>

	<form class="form" onsubmit={handleSubmit} novalidate>
		{#if error}
			<p class="form-error" role="alert">
				<span aria-hidden="true">✕</span>
				<span>{error}</span>
			</p>
		{/if}

		<!-- できないことを先に書く。探させない。 -->
		<p class="hint">
			<strong>写真は差し替えられません。</strong>
			変えたい場合は投稿し直してください。
		</p>

		<div class="field">
			<label for="prefecture">都道府県（必須）</label>
			<select id="prefecture" bind:value={prefectureCode} required disabled={saving}>
				<option value="" disabled>選んでください</option>
				{#each prefectures as p (p.code)}
					<option value={p.code}>{p.name}</option>
				{/each}
			</select>
		</div>

		<div class="field">
			<label for="spot">スポット名</label>
			<input
				id="spot"
				type="text"
				bind:value={spotName}
				maxlength="100"
				disabled={saving}
				aria-describedby="spot-hint"
			/>
			<p class="hint" id="spot-hint">「道の駅○○」など。任意です。</p>
		</div>

		<div class="field">
			<label for="visited">訪問日</label>
			<input
				id="visited"
				type="date"
				bind:value={visitedOn}
				max={today}
				disabled={saving}
				aria-describedby="visited-hint"
			/>
			<p class="hint" id="visited-hint">
				実際に訪れた日です。投稿した日とは別に記録されます。空にすると、
				<strong>プロフィールの「訪問日順」から外れます。</strong>
			</p>
		</div>

		<div class="field">
			<label for="body">本文（必須）</label>
			<textarea id="body" bind:value={body} rows="6" maxlength="1000" required disabled={saving}
			></textarea>
			<p class="hint">{body.length} / 1000文字</p>
		</div>

		<div class="field">
			<label for="tags">タグ</label>
			<input
				id="tags"
				type="text"
				bind:value={tagsInput}
				disabled={saving}
				aria-describedby="tags-hint"
			/>
			<p class="hint" id="tags-hint">空白か読点で区切ります。5個まで。例: グルメ 海鮮</p>
		</div>

		<button class="submit" type="submit" disabled={saving}>
			{saving ? '保存しています…' : '保存する'}
		</button>
	</form>

	<BackLink label="投稿" href={resolve('/posts/[postId]', { postId: String(view.post.id) })} />
{/if}

<style>
	h1 {
		margin-top: 0;
	}

	.error {
		color: var(--color-danger);
	}
</style>
