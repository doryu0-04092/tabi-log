import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Post } from '$lib/api/posts';
import NewPostsNotice from './NewPostsNotice.svelte';

/*
新着の知らせ。

**E2E では「タブに戻ってきたとき」の経路しか通せない。** 30秒待つ
テストは書けないためである。ここでは時計を差し替えて、
**間隔そのもの**と、裏に回っている間は問い合わせないことを確かめる。
*/

const INTERVAL_MS = 30_000;

/** 一覧の応答をこの ID だけで組み立てる。判定に使うのは id だけである。 */
function feed(ids: number[]) {
	return { posts: ids.map((id) => ({ id }) as Post) };
}

type Props = {
	newestId: number | undefined;
	fetchLatest: () => Promise<{ posts: Post[] }>;
	onApply: () => void;
	enabled?: boolean;
};

function render(props: Props) {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(NewPostsNotice, { target, props });
	flushSync();
	return { target, component };
}

/** 保留中の問い合わせが解決するまで待つ。時計を進めるだけでは足りない。 */
async function settle() {
	await Promise.resolve();
	await Promise.resolve();
	flushSync();
}

describe('NewPostsNotice', () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});

	afterEach(() => {
		vi.useRealTimers();
		document.body.innerHTML = '';
	});

	it('30秒ごとに確認し、新しいものがあれば件数を出す', async () => {
		const fetchLatest = vi.fn().mockResolvedValue(feed([12, 11, 10]));
		const { target, component } = render({
			newestId: 10,
			fetchLatest,
			onApply: vi.fn()
		});

		// **開いた直後には問い合わせない。** 今表示しているものが最新である。
		expect(fetchLatest).not.toHaveBeenCalled();
		expect(target.querySelector('button')).toBeNull();

		await vi.advanceTimersByTimeAsync(INTERVAL_MS);
		await settle();

		expect(fetchLatest).toHaveBeenCalledTimes(1);
		expect(target.querySelector('button')?.textContent).toContain('2件');

		void unmount(component);
	});

	it('新しいものが無ければ何も出さない', async () => {
		const fetchLatest = vi.fn().mockResolvedValue(feed([10, 9]));
		const { target, component } = render({ newestId: 10, fetchLatest, onApply: vi.fn() });

		await vi.advanceTimersByTimeAsync(INTERVAL_MS);
		await settle();

		expect(target.querySelector('button')).toBeNull();
		void unmount(component);
	});

	// **失敗しても表示を消さない。** 一時的な失敗で帯が消えると、
	// あったはずの新着が無かったことになる。
	it('確認に失敗しても出ていた知らせを消さない', async () => {
		const fetchLatest = vi
			.fn()
			.mockResolvedValueOnce(feed([11, 10]))
			.mockRejectedValueOnce(new Error('繋がらない'));
		const { target, component } = render({ newestId: 10, fetchLatest, onApply: vi.fn() });

		await vi.advanceTimersByTimeAsync(INTERVAL_MS);
		await settle();
		expect(target.querySelector('button')).not.toBeNull();

		await vi.advanceTimersByTimeAsync(INTERVAL_MS);
		await settle();
		expect(target.querySelector('button')).not.toBeNull();

		void unmount(component);
	});

	// 開きっぱなしのタブが裏で叩き続けないこと。
	it('画面が見えていない間は問い合わせない', async () => {
		const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
		const fetchLatest = vi.fn().mockResolvedValue(feed([11, 10]));
		const { component } = render({ newestId: 10, fetchLatest, onApply: vi.fn() });

		await vi.advanceTimersByTimeAsync(INTERVAL_MS * 3);
		await settle();

		expect(fetchLatest).not.toHaveBeenCalled();

		hidden.mockRestore();
		void unmount(component);
	});

	it('押すと知らせが消え、取り込みが呼ばれる', async () => {
		const onApply = vi.fn();
		const fetchLatest = vi.fn().mockResolvedValue(feed([11, 10]));
		const { target, component } = render({ newestId: 10, fetchLatest, onApply });

		await vi.advanceTimersByTimeAsync(INTERVAL_MS);
		await settle();

		target.querySelector('button')?.click();
		flushSync();

		expect(onApply).toHaveBeenCalledTimes(1);
		expect(target.querySelector('button')).toBeNull();

		void unmount(component);
	});

	it('外したあとは確認しない', async () => {
		const fetchLatest = vi.fn().mockResolvedValue(feed([11, 10]));
		const { component } = render({ newestId: 10, fetchLatest, onApply: vi.fn() });

		void unmount(component);
		flushSync();

		await vi.advanceTimersByTimeAsync(INTERVAL_MS * 2);
		expect(fetchLatest).not.toHaveBeenCalled();
	});
});
