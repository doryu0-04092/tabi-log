<script lang="ts">
	let {
		url,
		displayName,
		size = 'medium'
	}: { url?: string | null; displayName: string; size?: 'small' | 'medium' | 'large' } = $props();

	/** 未設定のときに出す頭文字。 */
	let initial = $derived(displayName.trim().slice(0, 1) || '?');
</script>

<!--
	**アバターは装飾である。** 名前は必ず隣に文字で出ているため、
	alt を空にして読み上げから外す。名前を alt に入れると、
	読み上げが同じ名前を2回言うことになる。

	未設定のときは頭文字を出す。空白のままだと「読み込めていない」のか
	「設定していない」のか区別できない。
-->
{#if url}
	<img class="avatar {size}" src={url} alt="" width="96" height="96" loading="lazy" />
{:else}
	<span class="avatar {size} placeholder" aria-hidden="true">{initial}</span>
{/if}

<style>
	.avatar {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		flex: none;
		border-radius: 50%;
		object-fit: cover;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
	}

	.small {
		width: 2rem;
		height: 2rem;
		font-size: 0.875rem;
	}

	.medium {
		width: 2.5rem;
		height: 2.5rem;
		font-size: 1rem;
	}

	.large {
		width: 4.5rem;
		height: 4.5rem;
		font-size: 1.75rem;
	}

	.placeholder {
		font-weight: 700;
		color: var(--color-text-muted);
	}
</style>
