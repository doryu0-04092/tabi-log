<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { ApiError, NetworkError } from '$lib/api/client';
	import {
		ACCEPTED_IMAGE_TYPES,
		MAX_IMAGES_PER_POST,
		MAX_IMAGE_BYTES,
		createPost,
		listPrefectures,
		uploadImage,
		type Prefecture
	} from '$lib/api/posts';
	import '$lib/styles/forms.css';

	/**
	 * 選んだ画像1枚の状態。
	 *
	 * **アップロードの完了と、サーバー側の処理の完了は別である。**
	 * S3 へ送り終わってもすぐには投稿に使えない（形式の検証と
	 * EXIF の除去が終わっていない）。利用者にはその区別が分からないため、
	 * 画面では「準備中」としてまとめて見せ、完了するまで送信させない。
	 */
	type Selected = {
		file: File;
		previewUrl: string;
		altText: string;
		mediaId: number | null;
		state: 'uploading' | 'ready' | 'failed';
		error?: string;
	};

	let prefectures = $state<Prefecture[]>([]);
	let selected = $state<Selected[]>([]);
	let body = $state('');
	let prefectureCode = $state('');
	let spotName = $state('');
	let visitedOn = $state('');
	let tagsInput = $state('');
	let error = $state('');
	let submitting = $state(false);

	// 未来の日付を選べないようにする。サーバー側でも拒否するが、
	// 選んでから怒られるより、選べない方が分かりやすい。
	const today = new Date().toISOString().slice(0, 10);

	$effect(() => {
		void loadPrefectures();
	});

	async function loadPrefectures() {
		if (prefectures.length > 0) return;
		try {
			prefectures = await listPrefectures();
		} catch {
			error = '都道府県の一覧を取得できませんでした';
		}
	}

	/** 全ての画像が投稿に使える状態か。 */
	let allReady = $derived(selected.length > 0 && selected.every((s) => s.state === 'ready'));

	async function handleFiles(event: Event) {
		const input = event.target as HTMLInputElement;
		const files = Array.from(input.files ?? []);
		input.value = ''; // 同じファイルを選び直せるようにする

		for (const file of files) {
			if (selected.length >= MAX_IMAGES_PER_POST) {
				error = `画像は${MAX_IMAGES_PER_POST}枚までです`;
				break;
			}
			if (!ACCEPTED_IMAGE_TYPES.includes(file.type)) {
				error = 'JPEG・PNG・WebP のみ選べます';
				continue;
			}
			if (file.size > MAX_IMAGE_BYTES) {
				error = '画像は10MBまでです';
				continue;
			}

			const entry: Selected = {
				file,
				previewUrl: URL.createObjectURL(file),
				altText: '',
				mediaId: null,
				state: 'uploading'
			};
			selected = [...selected, entry];
			void upload(entry);
		}
	}

	async function upload(entry: Selected) {
		try {
			const mediaId = await uploadImage(entry.file);
			update(entry, { mediaId, state: 'ready' });
		} catch (e) {
			update(entry, {
				state: 'failed',
				error: e instanceof Error ? e.message : 'アップロードに失敗しました'
			});
		}
	}

	function update(entry: Selected, patch: Partial<Selected>) {
		selected = selected.map((s) => (s.file === entry.file ? { ...s, ...patch } : s));
	}

	function remove(entry: Selected) {
		URL.revokeObjectURL(entry.previewUrl);
		selected = selected.filter((s) => s.file !== entry.file);
	}

	function parseTags(raw: string): string[] {
		return raw
			.split(/[\s,、]+/)
			.map((t) => t.replace(/^#/, '').trim())
			.filter((t) => t !== '');
	}

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		if (submitting) return;

		error = '';
		if (selected.length === 0) {
			error = '画像を1枚以上選んでください';
			return;
		}
		if (!allReady) {
			error = '画像の準備が終わるまでお待ちください';
			return;
		}
		if (selected.some((s) => s.altText.trim() === '')) {
			error = 'すべての画像に説明（代替テキスト）を入力してください';
			return;
		}

		submitting = true;
		try {
			const post = await createPost({
				body,
				prefectureCode,
				spotName: spotName.trim() === '' ? null : spotName,
				visitedOn,
				tags: parseTags(tagsInput),
				media: selected.map((s) => ({ mediaId: s.mediaId as number, altText: s.altText }))
			});
			await goto(resolve('/posts/[postId]', { postId: String(post.id) }));
		} catch (e) {
			error =
				e instanceof ApiError || e instanceof NetworkError
					? e.message
					: '予期しないエラーが発生しました';
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:head>
	<title>投稿する — tabi-log</title>
</svelte:head>

<h1>投稿する</h1>

<form class="form" onsubmit={handleSubmit} novalidate>
	{#if error}
		<p class="form-error" role="alert">
			<span aria-hidden="true">✕</span>
			<span>{error}</span>
		</p>
	{/if}

	<fieldset>
		<legend>写真</legend>

		<label for="photos">写真を選ぶ（1〜4枚）</label>
		<input
			id="photos"
			type="file"
			accept={ACCEPTED_IMAGE_TYPES.join(',')}
			multiple
			onchange={handleFiles}
			disabled={submitting || selected.length >= MAX_IMAGES_PER_POST}
			aria-describedby="photos-hint"
		/>
		<p class="hint" id="photos-hint">
			JPEG・PNG・WebP、10MBまで、{MAX_IMAGES_PER_POST}枚まで。
			<strong>位置情報（EXIF）はアップロード後に自動で削除されます。</strong>
		</p>

		{#if selected.length > 0}
			<ul class="photos">
				{#each selected as item, i (item.file)}
					<li>
						<img src={item.previewUrl} alt="" width="96" height="96" />

						<div class="photo-fields">
							<!--
								代替テキストは必須。写真が主役のサービスで任意にすると
								実質的に入力されず、画像が見えない利用者に何も伝わらない。
							-->
							<label for="alt-{i}">画像{i + 1}の説明（必須）</label>
							<input
								id="alt-{i}"
								type="text"
								value={item.altText}
								oninput={(e) => update(item, { altText: e.currentTarget.value })}
								maxlength="200"
								disabled={submitting}
								aria-describedby="alt-hint-{i}"
							/>
							<p class="hint" id="alt-hint-{i}">
								写真に何が写っているかを書きます。画像が見えない方に伝わります。
							</p>

							<!-- 状態は色ではなく文言で示す。 -->
							<p class="status" data-state={item.state}>
								{#if item.state === 'uploading'}
									<span aria-hidden="true">…</span> 準備しています（送信と検査）
								{:else if item.state === 'ready'}
									<span aria-hidden="true">✓</span> 使えます
								{:else}
									<span aria-hidden="true">✕</span> {item.error}
								{/if}
							</p>
						</div>

						<button type="button" onclick={() => remove(item)} disabled={submitting}>
							画像{i + 1}を取り消す
						</button>
					</li>
				{/each}
			</ul>
		{/if}
	</fieldset>

	<div class="field">
		<label for="prefecture">都道府県（必須）</label>
		<select id="prefecture" bind:value={prefectureCode} required disabled={submitting}>
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
			disabled={submitting}
			aria-describedby="spot-hint"
		/>
		<p class="hint" id="spot-hint">「道の駅○○」など。任意です。</p>
	</div>

	<div class="field">
		<label for="visited">訪問日（必須）</label>
		<input
			id="visited"
			type="date"
			bind:value={visitedOn}
			max={today}
			required
			disabled={submitting}
			aria-describedby="visited-hint"
		/>
		<!-- 投稿日とは別の軸であることを伝える。 -->
		<p class="hint" id="visited-hint">実際に訪れた日です。投稿した日とは別に記録されます。</p>
	</div>

	<div class="field">
		<label for="body">本文（必須）</label>
		<textarea id="body" bind:value={body} rows="6" maxlength="1000" required disabled={submitting}
		></textarea>
		<p class="hint">{body.length} / 1000文字</p>
	</div>

	<div class="field">
		<label for="tags">タグ</label>
		<input
			id="tags"
			type="text"
			bind:value={tagsInput}
			disabled={submitting}
			aria-describedby="tags-hint"
		/>
		<p class="hint" id="tags-hint">空白か読点で区切ります。5個まで。例: グルメ 海鮮</p>
	</div>

	<button class="submit" type="submit" disabled={submitting || !allReady}>
		{submitting ? '投稿しています…' : '投稿する'}
	</button>
</form>

<style>
	fieldset {
		margin: 0;
		padding: var(--space-4);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
	}

	legend {
		padding: 0 var(--space-2);
		font-weight: 600;
	}

	.photos {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		list-style: none;
		margin: var(--space-4) 0 0;
		padding: 0;
	}

	.photos li {
		display: flex;
		gap: var(--space-4);
		align-items: flex-start;
		padding: var(--space-3);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
	}

	.photos img {
		flex: 0 0 auto;
		width: 96px;
		height: 96px;
		object-fit: cover;
		border-radius: var(--radius);
		background: var(--color-surface);
	}

	.photo-fields {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.photo-fields label {
		font-weight: 600;
	}

	.photo-fields input {
		padding: var(--space-2) var(--space-3);
		font: inherit;
		color: var(--color-text);
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
	}

	.status {
		margin: var(--space-1) 0 0;
		font-size: 0.875rem;
		color: var(--color-text-muted);
	}

	.status[data-state='ready'] {
		color: var(--color-ok);
	}

	.status[data-state='failed'] {
		color: var(--color-danger);
	}

	.photos button {
		flex: 0 0 auto;
		padding: var(--space-2) var(--space-3);
		font: inherit;
		font-size: 0.875rem;
		color: var(--color-text);
		background: transparent;
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		cursor: pointer;
	}

	select,
	textarea {
		padding: var(--space-2) var(--space-3);
		font: inherit;
		color: var(--color-text);
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
	}

	textarea {
		resize: vertical;
	}
</style>
