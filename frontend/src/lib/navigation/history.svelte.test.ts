/*
移動の記録そのものが正しく動くかを、画面から切り離して確かめる。

2026-09-01、E2E で「辿って来ても『前の画面へ戻る』が出ない」ことが確定した。
**どこが壊れているかを切り分けるために置く。** ここが通れば数え方は正しく、
原因は afterNavigate の呼ばれ方か、画面側の読み取りにある。
*/
import { describe, expect, test } from 'vitest';
import { navHistory, recordNavigation } from './history.svelte';

describe('移動の記録', () => {
	test('最初の読み込みだけでは戻れない', () => {
		expect(navHistory.canGoBack).toBe(false);
		recordNavigation('enter');
		expect(navHistory.canGoBack).toBe(false);
	});

	test('リンクを辿ると戻れるようになる', () => {
		recordNavigation('link');
		expect(navHistory.canGoBack).toBe(true);
	});

	test('戻る操作で減り、入口まで戻ると戻れなくなる', () => {
		recordNavigation('popstate');
		expect(navHistory.canGoBack).toBe(false);
	});

	// **下限は 0。** 減らしすぎて負になると、その後いくら進んでも戻れなくなる。
	test('入口より手前へは減らない', () => {
		recordNavigation('popstate');
		recordNavigation('popstate');
		recordNavigation('link');
		expect(navHistory.canGoBack).toBe(true);
	});
});
