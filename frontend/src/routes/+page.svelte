<script lang="ts">
	import { resolve } from '$app/paths';
	import { request } from '$lib/api/client';
	import type { components } from '$lib/api/gen';
	import { session } from '$lib/auth/session.svelte';

	// 型は docs/openapi.yaml から生成したものを使う。手で書くと、
	// 仕様を変えたときに気づかないまま食い違う。
	type Prefecture = components['schemas']['Prefecture'];

	type Result =
		| { state: 'loading' }
		| { state: 'ok'; prefectures: Prefecture[] }
		| { state: 'error'; message: string };

	let result = $state<Result>({ state: 'loading' });

	$effect(() => {
		void load();
	});

	async function load() {
		result = { state: 'loading' };
		try {
			result = { state: 'ok', prefectures: await request<Prefecture[]>('/prefectures') };
		} catch {
			result = { state: 'error', message: '都道府県の一覧を取得できませんでした' };
		}
	}

	/**
	 * 地方区分ごとにまとめる。
	 *
	 * API は JIS コード順で返し、同じ地方の県はその並びで連続する
	 * （docs/er-diagram.md の prefectures マスタ）。そのため、
	 * 地方が変わったところで区切るだけでまとまる。
	 */
	function groupByRegion(prefectures: Prefecture[]): { region: string; items: Prefecture[] }[] {
		const groups: { region: string; items: Prefecture[] }[] = [];
		for (const p of prefectures) {
			const last = groups.at(-1);
			if (last?.region === p.region) last.items.push(p);
			else groups.push({ region: p.region, items: [p] });
		}
		return groups;
	}
</script>

<svelte:head>
	<title>tabi-log</title>
</svelte:head>

<h1>tabi-log</h1>

<p>旅行先の写真と記録を共有する SNS です。現在は立ち上げ段階です。</p>

{#if session.restored}
	{#if session.isAuthenticated}
		<p>
			<strong>{session.user?.displayName}</strong>（@{session.user?.handle}）としてログインしています。
		</p>
	{:else}
		<p>
			<a href={resolve("/login")}>ログイン</a> または <a href={resolve("/signup")}>新規登録</a> をしてください。
		</p>
	{/if}
{/if}

<h2>投稿できる都道府県</h2>

{#if result.state === 'loading'}
	<p>読み込んでいます…</p>
{:else if result.state === 'error'}
	<p class="error" role="alert"><span aria-hidden="true">✕</span> {result.message}</p>
{:else}
	<p>{result.prefectures.length} 件</p>
	<!-- 一覧は地方ごとにまとめて示す。全47件を並べると読み取りにくい。 -->
	<ul class="regions">
		{#each groupByRegion(result.prefectures) as group (group.region)}
			<li>
				<strong>{group.region}</strong>
				<span>{group.items.map((p) => p.name).join('・')}</span>
			</li>
		{/each}
	</ul>
{/if}

<style>
	h1 {
		margin-top: 0;
	}

	.error {
		color: var(--color-danger);
	}

	.regions {
		list-style: none;
		padding: 0;
		margin: 0;
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
	}

	.regions li {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2) var(--space-4);
		padding: var(--space-3) var(--space-4);
	}

	.regions li + li {
		border-top: 1px solid var(--color-border);
	}

	.regions strong {
		flex: 0 0 6rem;
	}

	.regions span {
		flex: 1;
		color: var(--color-text-muted);
	}
</style>
