<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { ApiError, NetworkError } from '$lib/api/client';
	import { signup } from '$lib/auth/session.svelte';
	import '$lib/styles/forms.css';

	let email = $state('');
	let handle = $state('');
	let displayName = $state('');
	let password = $state('');
	let error = $state('');
	let submitting = $state(false);

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		if (submitting) return;

		error = '';
		submitting = true;
		try {
			await signup({ email, handle, displayName, password });
			await goto(resolve('/'));
		} catch (e) {
			if (e instanceof ApiError || e instanceof NetworkError) {
				error = e.message;
			} else {
				error = '予期しないエラーが発生しました';
			}
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:head>
	<title>新規登録 — tabi-log</title>
</svelte:head>

<h1>新規登録</h1>

<form class="form" onsubmit={handleSubmit} novalidate>
	{#if error}
		<p class="form-error" role="alert">
			<span aria-hidden="true">✕</span>
			<span>{error}</span>
		</p>
	{/if}

	<div class="field">
		<label for="email">メールアドレス</label>
		<input
			id="email"
			type="email"
			bind:value={email}
			autocomplete="email"
			required
			disabled={submitting}
			aria-describedby="email-hint"
		/>
		<!--
			確認メールを送らないことを先に伝える。届くと思って待たれると
			「登録できたのに使えない」と誤解される。
		-->
		<p class="hint" id="email-hint">確認メールは送信しません。ログイン時に使います。</p>
	</div>

	<div class="field">
		<label for="handle">ハンドル</label>
		<input
			id="handle"
			type="text"
			bind:value={handle}
			autocomplete="username"
			required
			disabled={submitting}
			aria-describedby="handle-hint"
		/>
		<p class="hint" id="handle-hint">英数字とアンダースコアで3〜30文字。URL に使われます。</p>
	</div>

	<div class="field">
		<label for="displayName">表示名</label>
		<input
			id="displayName"
			type="text"
			bind:value={displayName}
			autocomplete="nickname"
			required
			disabled={submitting}
			aria-describedby="display-name-hint"
		/>
		<p class="hint" id="display-name-hint">1〜50文字。投稿に表示されます。</p>
	</div>

	<div class="field">
		<label for="password">パスワード</label>
		<input
			id="password"
			type="password"
			bind:value={password}
			autocomplete="new-password"
			required
			disabled={submitting}
			aria-describedby="password-hint"
		/>
		<!--
			上限をバイト数で伝える。文字数だけ書くと、日本語のパスフレーズで
			「20文字なのに長すぎると言われる」ことになる（1文字3バイト）。
		-->
		<p class="hint" id="password-hint">8〜72バイト。日本語は1文字あたり3バイト程度です。</p>
	</div>

	<button class="submit" type="submit" disabled={submitting}>
		{submitting ? '登録しています…' : '登録する'}
	</button>
</form>

<p>すでにアカウントをお持ちの場合は <a href={resolve("/login")}>ログイン</a> へ。</p>
