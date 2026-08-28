<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { ApiError, NetworkError } from '$lib/api/client';
	import { login } from '$lib/auth/session.svelte';
	import '$lib/styles/forms.css';

	let email = $state('');
	let password = $state('');
	let error = $state('');
	let submitting = $state(false);

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		if (submitting) return;

		error = '';
		submitting = true;
		try {
			await login(email, password);
			await goto(resolve('/'));
		} catch (e) {
			// サーバーは失敗の理由を区別しない（どのアドレスが登録済みかを
			// 調べられないようにするため）。画面もそれに合わせる。
			if (e instanceof ApiError) {
				error = e.message;
			} else if (e instanceof NetworkError) {
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
	<title>ログイン — tabi-log</title>
</svelte:head>

<h1>ログイン</h1>

<form class="form" onsubmit={handleSubmit} novalidate>
	{#if error}
		<!--
			role="alert" により、フォーカスを移さずに読み上げソフトへ伝わる。
			色に加えて記号と文言でも示す。
		-->
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
		/>
	</div>

	<div class="field">
		<label for="password">パスワード</label>
		<input
			id="password"
			type="password"
			bind:value={password}
			autocomplete="current-password"
			required
			disabled={submitting}
		/>
	</div>

	<button class="submit" type="submit" disabled={submitting}>
		{submitting ? 'ログインしています…' : 'ログイン'}
	</button>
</form>

<p>アカウントをお持ちでない場合は <a href={resolve("/signup")}>新規登録</a> へ。</p>
