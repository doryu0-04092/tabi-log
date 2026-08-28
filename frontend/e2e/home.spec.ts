import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

test.describe('トップページ', () => {
	test('バックエンドの状態を表示する', async ({ page }) => {
		await page.goto('/');

		await expect(page.getByRole('heading', { name: 'tabi-log', level: 1 })).toBeVisible();

		// SPA のため内容はクライアント側で取得される。
		// 「確認中…」から確定した状態へ変わることを待つ。
		await expect(page.getByText('✓ 正常', { exact: false }).first()).toBeVisible();
		await expect(page.getByText('（DB 疎通あり）')).toBeVisible();
	});

	test('言語が日本語として宣言されている', async ({ page }) => {
		await page.goto('/');

		// lang が誤っていると読み上げソフトが別言語の音声で読む。
		await expect(page.locator('html')).toHaveAttribute('lang', 'ja');
	});

	test('キーボードだけで本文へスキップできる', async ({ page }) => {
		await page.goto('/');

		// body を起点にした最初の Tab でスキップリンクにフォーカスが当たること。
		// （keyboard.press だけだと開始位置が不定になるため、起点を明示する）
		await page.locator('body').press('Tab');
		await expect(page.getByRole('link', { name: '本文へスキップ' })).toBeFocused();
	});

	test('アクセシビリティ違反が無い', async ({ page }) => {
		await page.goto('/');
		await expect(page.getByText('✓ 正常', { exact: false }).first()).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
