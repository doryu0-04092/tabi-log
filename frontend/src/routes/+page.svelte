<script lang="ts">
	import { ApiError, NetworkError, request } from '$lib/api/client';
	import type { components } from '$lib/api/gen';

	// 型は docs/openapi.yaml から生成したものを使う。手で書くと、
	// 仕様を変えたときに気づかないまま食い違う。
	type Livez = components['schemas']['LivezResponse']['data'];
	type Readyz = components['schemas']['ReadyzResponse']['data'];
	type Health = Livez | Readyz;

	type Probe = { label: string; path: string };

	const probes: Probe[] = [
		{ label: 'livez（依存を見ない）', path: '/livez' },
		{ label: 'readyz（DB 疎通を含む）', path: '/readyz' }
	];

	type Result =
		| { state: 'loading' }
		| { state: 'ok'; health: Health }
		| { state: 'error'; message: string };

	let results = $state<Record<string, Result>>(
		Object.fromEntries(probes.map((p) => [p.path, { state: 'loading' } as Result]))
	);

	async function check(path: string) {
		results[path] = { state: 'loading' };
		try {
			results[path] = { state: 'ok', health: await request<Health>(path) };
		} catch (e) {
			if (e instanceof ApiError || e instanceof NetworkError) {
				results[path] = { state: 'error', message: e.message };
			} else {
				results[path] = { state: 'error', message: '予期しないエラーが発生しました' };
			}
		}
	}

	// SPA なのでクライアント側で初回に取得する。
	$effect(() => {
		for (const p of probes) void check(p.path);
	});
</script>

<svelte:head>
	<title>tabi-log — 疎通確認</title>
</svelte:head>

<h1>tabi-log</h1>

<p>
	旅行先の写真と記録を共有する SNS です。現在は立ち上げ段階で、バックエンドとの疎通確認のみ動作します。
</p>

<h2>バックエンドの状態</h2>

<!--
	状態は色だけでなく記号と文言でも示す。
	色の違いだけで情報を伝えないというアクセシビリティ要件のため。
-->
<ul class="probes">
	{#each probes as probe (probe.path)}
		{@const result = results[probe.path]}
		<li>
			<span class="label">{probe.label}</span>
			{#if result.state === 'loading'}
				<span class="status">確認中…</span>
			{:else if result.state === 'ok'}
				<span class="status ok">✓ 正常{'database' in result.health ? '（DB 疎通あり）' : ''}</span>
			{:else}
				<span class="status ng">✕ {result.message}</span>
			{/if}
		</li>
	{/each}
</ul>

<button type="button" onclick={() => probes.forEach((p) => void check(p.path))}>
	再確認する
</button>

<style>
	h1 {
		margin-top: 0;
	}

	.probes {
		list-style: none;
		padding: 0;
		margin: 0 0 var(--space-6);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
	}

	.probes li {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2) var(--space-4);
		justify-content: space-between;
		padding: var(--space-3) var(--space-4);
	}

	.probes li + li {
		border-top: 1px solid var(--color-border);
	}

	.label {
		font-weight: 600;
	}

	.status {
		color: var(--color-text-muted);
		font-family: var(--font-mono);
		font-size: 0.9rem;
	}

	.status.ok {
		color: var(--color-ok);
	}

	.status.ng {
		color: var(--color-danger);
	}

	button {
		padding: var(--space-2) var(--space-4);
		font: inherit;
		color: var(--color-accent-text);
		background: var(--color-accent);
		border: none;
		border-radius: var(--radius);
		cursor: pointer;
	}
</style>
