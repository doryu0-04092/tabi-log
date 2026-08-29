import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { createPost, createPostViaApi, signup, signupViaApi } from './fixtures/app';

/*
投稿の編集。

**画像は編集できない**（要件定義書 3.2）。画面にもその旨を出しており、
「できるはずなのに見つからない」状態にしないことまで含めて確かめる。
*/
test.describe('投稿の編集', () => {
	test('本文・都道府県・タグを直すと詳細に反映される', async ({ page }) => {
		await signup(page, { displayName: '編集する人' });
		const before = `編集前 ${Date.now()}`;
		await createPost(page, { body: before, prefecture: '北海道' });

		await page.getByRole('link', { name: 'この投稿を編集する' }).click();
		await expect(page.getByRole('heading', { name: '投稿を編集する', level: 1 })).toBeVisible();

		// **今の値が入っていること。** 空のフォームだと、
		// 直したいところ以外まで打ち直すことになる。
		await expect(page.getByLabel('本文（必須）')).toHaveValue(before);
		await expect(page.getByLabel('都道府県（必須）')).toHaveValue('01');

		const after = `編集後 ${Date.now()}`;
		await page.getByLabel('本文（必須）').fill(after);
		await page.getByLabel('都道府県（必須）').selectOption({ label: '沖縄県' });
		await page.getByLabel('タグ').fill('海 夕日');
		await page.getByRole('button', { name: '保存する' }).click();

		await expect(page).toHaveURL(/\/posts\/\d+$/);
		await expect(page.getByText(after)).toBeVisible();
		await expect(page.getByText(before)).toBeHidden();
		await expect(page.getByRole('link', { name: '沖縄県' })).toBeVisible();
	});

	// 訪問日を空にすると旅行履歴から外れる。**消せることを確かめる。**
	test('訪問日を空にできる', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: `訪問日を消す ${Date.now()}`, prefecture: '京都府' });

		await expect(page.getByText('訪問 2026年5月3日')).toBeVisible();

		await page.getByRole('link', { name: 'この投稿を編集する' }).click();
		await page.getByLabel('訪問日').fill('');
		await page.getByRole('button', { name: '保存する' }).click();

		await expect(page).toHaveURL(/\/posts\/\d+$/);
		await expect(page.getByText(/^訪問 /)).toBeHidden();
	});

	test('写真を差し替えられないことが画面に書いてある', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: `写真の説明 ${Date.now()}`, prefecture: '長野県' });

		await page.getByRole('link', { name: 'この投稿を編集する' }).click();
		await expect(page.getByText('写真は差し替えられません。')).toBeVisible();
		await expect(page.locator('input[type="file"]')).toHaveCount(0);
	});

	// **他人の投稿には編集の導線を出さない。**
	// 権限の担保はサーバー側で行っており、これは表示の確認。
	test('他人の投稿には編集リンクが出ない', async ({ page, browser, baseURL }) => {
		const other = await browser.newContext({ baseURL });
		const { token } = await signupViaApi(other.request, { displayName: '別の人' });
		const postId = await createPostViaApi(other.request, token, {
			body: `他人の投稿 ${Date.now()}`
		});
		await other.close();

		await signup(page, { displayName: '見るだけの人' });
		await page.goto(`/posts/${postId}`);

		await expect(page.getByRole('link', { name: 'この投稿を編集する' })).toBeHidden();
	});

	// 直接 URL を叩いても編集させない。
	test('他人の投稿の編集画面を直接開いても編集できない', async ({ page, browser, baseURL }) => {
		const other = await browser.newContext({ baseURL });
		const { token } = await signupViaApi(other.request);
		const postId = await createPostViaApi(other.request, token, {
			body: `直接開く ${Date.now()}`
		});
		await other.close();

		await signup(page);
		await page.goto(`/posts/${postId}/edit`);

		await expect(page.getByRole('alert')).toContainText('この投稿は編集できません');
		await expect(page.getByRole('button', { name: '保存する' })).toBeHidden();
	});

	test('編集画面にアクセシビリティ違反が無い', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: `検査用 ${Date.now()}`, prefecture: '大阪府' });

		await page.getByRole('link', { name: 'この投稿を編集する' }).click();
		await expect(page.getByRole('heading', { name: '投稿を編集する', level: 1 })).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
