<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { listFollowing } from '$lib/api/users';
	import { session } from '$lib/auth/session.svelte';
	import UserList from '$lib/components/UserList.svelte';

	let handle = $derived(page.params.handle ?? '');
</script>

{#if !session.restored}
	<p>読み込んでいます…</p>
{:else if !session.isAuthenticated}
	<p>この一覧を見るには <a href={resolve('/login')}>ログイン</a> が必要です。</p>
{:else}
	<UserList
		{handle}
		heading="フォロー中"
		emptyMessage="まだ誰もフォローしていません。"
		load={listFollowing}
	/>
{/if}
