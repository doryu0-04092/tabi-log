<script lang="ts">
	import { resolve } from '$app/paths';
	import { ACCEPTED_IMAGE_TYPES, MAX_IMAGE_BYTES, uploadImage } from '$lib/api/posts';
	import { clearAvatar, setAvatar, updateProfile } from '$lib/api/users';
	import { session, setSessionUser } from '$lib/auth/session.svelte';
	import Avatar from '$lib/components/Avatar.svelte';

	const MAX_DISPLAY_NAME = 50;
	const MAX_BIO = 300;

	let displayName = $state('');
	let bio = $state('');
	let loaded = $state(false);
	let saving = $state(false);
	let error = $state('');
	let saved = $state(false);

	// 現在の値を入力欄の初期値にする。空欄から始めると、
	// 何を消して何を残すのかが分からない。
	$effect(() => {
		if (!loaded && session.restored && session.user) {
			displayName = session.user.displayName;
			bio = session.user.bio ?? '';
			avatarUrl = session.user.avatarUrl ?? null;
			loaded = true;
		}
	});

	// アバター。**投稿画像と同じ経路を通す**（presign → S3 → 処理の完了待ち）。
	// EXIF の除去が要るのはアバターも同じである。
	let avatarUrl = $state<string | null>(null);
	let avatarPhase = $state<'idle' | 'uploading' | 'saving'>('idle');
	let avatarError = $state('');

	let displayNameLength = $derived(displayName.trim().length);
	let bioLength = $derived(bio.trim().length);
	let canSubmit = $derived(
		displayNameLength >= 1 &&
			displayNameLength <= MAX_DISPLAY_NAME &&
			bioLength <= MAX_BIO &&
			!saving
	);

	async function pickAvatar(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		if (!file) return;

		avatarError = '';
		if (!ACCEPTED_IMAGE_TYPES.includes(file.type)) {
			avatarError = 'JPEG・PNG・WebP のいずれかを選んでください。';
			input.value = '';
			return;
		}
		if (file.size > MAX_IMAGE_BYTES) {
			avatarError = '画像のサイズが大きすぎます。';
			input.value = '';
			return;
		}

		try {
			// **送信が終わってもまだ使えない。** 形式の検証と EXIF の除去が
			// サーバー側で走り、完了して初めて設定できる。
			avatarPhase = 'uploading';
			const mediaId = await uploadImage(file);

			avatarPhase = 'saving';
			await setAvatar(mediaId);

			const updated = await updateProfile({});
			setSessionUser(updated);
			avatarUrl = updated.avatarUrl ?? null;
		} catch {
			avatarError = 'アバターを設定できませんでした。もう一度お試しください。';
		} finally {
			avatarPhase = 'idle';
			input.value = '';
		}
	}

	async function removeAvatar() {
		avatarError = '';
		avatarPhase = 'saving';
		try {
			await clearAvatar();
			const updated = await updateProfile({});
			setSessionUser(updated);
			avatarUrl = null;
		} catch {
			avatarError = 'アバターを外せませんでした。';
		} finally {
			avatarPhase = 'idle';
		}
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!canSubmit) return;

		saving = true;
		error = '';
		saved = false;
		try {
			// **自己紹介は空文字で送ると消える。** 省略との違いに意味がある。
			const updated = await updateProfile({ displayName: displayName.trim(), bio: bio.trim() });
			// ヘッダーの表示名を差し替える。古いまま残ると、
			// 保存できたのか分からなくなる。
			setSessionUser(updated);
			saved = true;
		} catch {
			error = '保存できませんでした。もう一度お試しください。';
		} finally {
			saving = false;
		}
	}
</script>

<svelte:head>
	<title>プロフィールの編集 — tabi-log</title>
</svelte:head>

{#if !session.restored}
	<p>読み込んでいます…</p>
{:else if !session.isAuthenticated}
	<p>設定を変えるには <a href={resolve('/login')}>ログイン</a> が必要です。</p>
{:else}
	<nav aria-label="パンくず">
		<a href={resolve('/users/[handle]', { handle: session.user?.handle ?? '' })}>プロフィール</a>
		／ 編集
	</nav>

	<h1>プロフィールの編集</h1>

	<section class="avatar-section" aria-labelledby="avatar-heading">
		<h2 id="avatar-heading">アバター</h2>

		<div class="avatar-row">
			<Avatar url={avatarUrl} displayName={displayName || 'あなた'} size="large" />

			<div class="avatar-actions">
				<!--
					**label を必ず結び付ける。** ファイル選択は既定の見た目が
					ばらつくため、label 越しに押せる形にしておく。
				-->
				<label class="file" for="avatar">
					{avatarPhase === 'uploading'
						? '準備しています…'
						: avatarPhase === 'saving'
							? '設定しています…'
							: '画像を選ぶ'}
				</label>
				<input
					id="avatar"
					type="file"
					accept={ACCEPTED_IMAGE_TYPES.join(',')}
					onchange={pickAvatar}
					disabled={avatarPhase !== 'idle'}
				/>

				{#if avatarUrl}
					<button type="button" onclick={removeAvatar} disabled={avatarPhase !== 'idle'}>
						アバターを外す
					</button>
				{/if}
			</div>
		</div>

		<p class="note">JPEG・PNG・WebP。位置情報などの撮影情報は保存時に取り除かれます。</p>

		{#if avatarError}
			<p class="error" role="alert"><span aria-hidden="true">✕</span> {avatarError}</p>
		{/if}
	</section>

	<form onsubmit={submit}>
		<div class="field">
			<label for="displayName">表示名（必須）</label>
			<input
				id="displayName"
				type="text"
				bind:value={displayName}
				aria-describedby="displayName-count"
				disabled={saving}
			/>
			<p id="displayName-count" class="count" class:over={displayNameLength > MAX_DISPLAY_NAME}>
				{displayNameLength} / {MAX_DISPLAY_NAME} 文字
			</p>
		</div>

		<div class="field">
			<label for="bio">自己紹介</label>
			<textarea id="bio" bind:value={bio} rows="4" aria-describedby="bio-count" disabled={saving}
			></textarea>
			<p id="bio-count" class="count" class:over={bioLength > MAX_BIO}>
				{bioLength} / {MAX_BIO} 文字（空にすると消えます）
			</p>
		</div>

		{#if error}
			<p class="error" role="alert"><span aria-hidden="true">✕</span> {error}</p>
		{/if}
		{#if saved}
			<!-- 保存できたことを語で伝える。色や見た目の変化だけにしない。 -->
			<p class="saved" role="status"><span aria-hidden="true">✓</span> 保存しました。</p>
		{/if}

		<button type="submit" disabled={!canSubmit}>
			{saving ? '保存しています…' : '保存する'}
		</button>
	</form>

	<p class="link">
		<a href={resolve('/settings/account')}>パスワードの変更・退会はこちら</a>
	</p>
{/if}

<style>
	nav {
		margin-bottom: var(--space-4);
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	h1 {
		margin-top: 0;
	}

	.avatar-section {
		margin-bottom: var(--space-6);
		padding-bottom: var(--space-4);
		border-bottom: 1px solid var(--color-border);
	}

	h2 {
		margin-top: 0;
		font-size: 1.125rem;
	}

	.avatar-row {
		display: flex;
		align-items: center;
		gap: var(--space-4);
	}

	.avatar-actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}

	/* ファイル選択の既定の見た目はブラウザごとに違う。
	   label を押せる形にし、入力欄そのものは隠す。 */
	.file {
		display: inline-flex;
		align-items: center;
		min-height: 2.75rem;
		padding: var(--space-2) var(--space-4);
		font-weight: 600;
		color: var(--color-text);
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		cursor: pointer;
	}

	input[type='file'] {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip-path: inset(50%);
	}

	/* フォーカスは隠した入力欄ではなく label に見せる。
	   label が先・input が後の並びなので :has で拾う。 */
	.file:has(+ input[type='file']:focus-visible) {
		outline: 3px solid var(--color-accent);
		outline-offset: 2px;
	}

	.note {
		margin: var(--space-3) 0 0;
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	.avatar-section button {
		min-height: 2.75rem;
		padding: var(--space-2) var(--space-4);
		font: inherit;
		color: var(--color-text);
		background: transparent;
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		cursor: pointer;
	}

	form {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	label {
		font-weight: 600;
	}

	input,
	textarea {
		padding: var(--space-3);
		font: inherit;
		color: var(--color-text);
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
	}

	textarea {
		resize: vertical;
	}

	.count {
		margin: 0;
		color: var(--color-text-muted);
		font-size: 0.875rem;
	}

	.count.over {
		color: var(--color-danger);
		font-weight: 600;
	}

	button {
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

	button:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.error {
		margin: 0;
		color: var(--color-danger);
	}

	.saved {
		margin: 0;
		font-weight: 600;
	}

	.link {
		margin-top: var(--space-6);
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
	}
</style>
