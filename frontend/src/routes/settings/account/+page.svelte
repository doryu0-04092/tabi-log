<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { changePassword, deleteAccount } from '$lib/api/users';
	import { session } from '$lib/auth/session.svelte';

	// パスワードの変更。
	let currentPassword = $state('');
	let newPassword = $state('');
	let changing = $state(false);
	let changeError = $state('');

	// 退会。取り消せないので、確認の段を挟む。
	let confirming = $state(false);
	let deletePassword = $state('');
	let deleting = $state(false);
	let deleteError = $state('');

	let canChange = $derived(currentPassword.length > 0 && newPassword.length >= 8 && !changing);

	async function submitPassword(event: SubmitEvent) {
		event.preventDefault();
		if (!canChange) return;

		changing = true;
		changeError = '';
		try {
			await changePassword({ currentPassword, newPassword });
			// **変更すると全リフレッシュトークンが失効する。**
			// 自分のセッションも切れるため、そのままログインへ送る。
			await goto(resolve('/login'));
		} catch {
			changeError = '変更できませんでした。現在のパスワードをお確かめください。';
			changing = false;
		}
	}

	async function submitDelete(event: SubmitEvent) {
		event.preventDefault();
		if (deleting || deletePassword.length === 0) return;

		deleting = true;
		deleteError = '';
		try {
			await deleteAccount(deletePassword);
			await goto(resolve('/login'));
		} catch {
			deleteError = '退会できませんでした。パスワードをお確かめください。';
			deleting = false;
		}
	}
</script>

<svelte:head>
	<title>アカウント設定 — tabi-log</title>
</svelte:head>

{#if !session.restored}
	<p>読み込んでいます…</p>
{:else if !session.isAuthenticated}
	<p>設定を変えるには <a href={resolve('/login')}>ログイン</a> が必要です。</p>
{:else}
	<nav aria-label="パンくず">
		<a href={resolve('/settings/profile')}>プロフィールの編集</a> ／ アカウント設定
	</nav>

	<h1>アカウント設定</h1>

	<section aria-labelledby="password-heading">
		<h2 id="password-heading">パスワードの変更</h2>
		<!--
			**変更すると他の端末もログアウトされる。** 起きることを先に伝える。
			後から気づくと「勝手に落とされた」と受け取られる。
		-->
		<p class="note">変更すると、この端末を含むすべての端末でログインし直しが必要になります。</p>

		<form onsubmit={submitPassword}>
			<div class="field">
				<label for="currentPassword">現在のパスワード（必須）</label>
				<input
					id="currentPassword"
					type="password"
					autocomplete="current-password"
					bind:value={currentPassword}
					disabled={changing}
				/>
			</div>

			<div class="field">
				<label for="newPassword">新しいパスワード（必須）</label>
				<input
					id="newPassword"
					type="password"
					autocomplete="new-password"
					bind:value={newPassword}
					aria-describedby="newPassword-hint"
					disabled={changing}
				/>
				<p id="newPassword-hint" class="note">8文字以上。</p>
			</div>

			{#if changeError}
				<p class="error" role="alert"><span aria-hidden="true">✕</span> {changeError}</p>
			{/if}

			<button type="submit" disabled={!canChange}>
				{changing ? '変更しています…' : 'パスワードを変更する'}
			</button>
		</form>
	</section>

	<section aria-labelledby="delete-heading" class="danger-zone">
		<h2 id="delete-heading">退会</h2>
		<!--
			**何が消えるかを先に書く。** 押してから知るには重すぎる操作である。
		-->
		<p class="note">
			退会すると、投稿・画像・コメント・いいね・フォローがすべて削除されます。
			<strong>取り消せません。</strong>
			ハンドル（@{session.user?.handle}）は保持され、他の人が使うことはできません。
		</p>

		{#if confirming}
			<form onsubmit={submitDelete}>
				<div class="field">
					<label for="deletePassword">現在のパスワード（必須）</label>
					<input
						id="deletePassword"
						type="password"
						autocomplete="current-password"
						bind:value={deletePassword}
						disabled={deleting}
					/>
				</div>

				{#if deleteError}
					<p class="error" role="alert"><span aria-hidden="true">✕</span> {deleteError}</p>
				{/if}

				<div class="actions">
					<button type="submit" class="danger" disabled={deleting || deletePassword.length === 0}>
						{deleting ? '退会しています…' : '退会する'}
					</button>
					<button type="button" onclick={() => (confirming = false)} disabled={deleting}>
						やめる
					</button>
				</div>
			</form>
		{:else}
			<button type="button" onclick={() => (confirming = true)}>退会の手続きへ進む</button>
		{/if}
	</section>
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

	section {
		margin-top: var(--space-6);
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
	}

	h2 {
		margin-top: 0;
		font-size: 1.125rem;
	}

	.note {
		color: var(--color-text-muted);
	}

	form {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		margin-top: var(--space-4);
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	label {
		font-weight: 600;
	}

	input {
		padding: var(--space-3);
		font: inherit;
		color: var(--color-text);
		background: var(--color-bg);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
	}

	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-3);
	}

	button {
		min-height: 2.75rem;
		padding: var(--space-3) var(--space-4);
		font: inherit;
		color: var(--color-text);
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		cursor: pointer;
	}

	button[type='submit'] {
		font-weight: 600;
		color: var(--color-accent-text);
		background: var(--color-accent);
		border-color: var(--color-accent);
	}

	button.danger {
		color: var(--color-accent-text);
		background: var(--color-danger);
		border-color: var(--color-danger);
	}

	button:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	/* 取り返しのつかない操作は枠でも区切る。色だけに頼らない。 */
	.danger-zone {
		margin-top: var(--space-8);
		padding: var(--space-4);
		border: 2px solid var(--color-danger);
		border-radius: var(--radius);
	}

	.error {
		margin: 0;
		color: var(--color-danger);
	}
</style>
