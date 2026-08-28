import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

test.describe('トップページ', () => {
	test('未ログインではログインを促す', async ({ page }) => {
		await page.goto('/');

		await expect(page.getByRole('heading', { name: 'tabi-log', level: 1 })).toBeVisible();
		await expect(page.getByRole('link', { name: '新規登録' })).toBeVisible();
	});

	test('言語が日本語として宣言されている', async ({ page }) => {
		await page.goto('/');

		// lang が誤っていると読み上げソフトが別言語の音声で読む。
		await expect(page.locator('html')).toHaveAttribute('lang', 'ja');
	});

	test('キーボードだけで本文へスキップできる', async ({ page }) => {
		await page.goto('/');

		// **描画が終わるのを待ってから押す。** この画面は SPA で、
		// スクリプトが動き始めるまでスキップリンクが存在しない。
		// 待たずに押すと、並列実行のときだけ落ちる不安定なテストになる。
		const skipLink = page.getByRole('link', { name: '本文へスキップ' });
		await skipLink.waitFor({ state: 'attached' });

		// body を起点にした最初の Tab でスキップリンクにフォーカスが当たること。
		await page.locator('body').press('Tab');
		await expect(skipLink).toBeFocused();
	});

	test('アクセシビリティ違反が無い', async ({ page }) => {
		await page.goto('/');
		await expect(page.getByRole('heading', { name: 'tabi-log', level: 1 })).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
