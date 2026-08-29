import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { createPost, signup } from './fixtures/app';
import { PNG } from './fixtures/png';

/** アバターを1枚設定する。**処理の完了まで待つ。** */
async function setAvatar(page: import('@playwright/test').Page) {
	await page.goto('/settings/profile');
	await page.setInputFiles('#avatar', { name: 'avatar.png', mimeType: 'image/png', buffer: PNG });
	// 送信が終わっても、形式の検証と EXIF の除去が終わるまで設定できない。
	await expect(page.getByRole('button', { name: 'アバターを外す' })).toBeVisible({
		timeout: 30_000
	});
}

test.describe('アバター', () => {
	// 未設定でも画面は成立する。空白のままだと「読み込めていない」のか
	// 「設定していない」のか区別できないため、頭文字を出す。
	test('未設定でもプロフィールが開ける', async ({ page }) => {
		const me = await signup(page, { displayName: 'アバター無しの人' });

		await page.goto(`/users/${me.handle}`);
		await expect(page.getByRole('heading', { name: 'アバター無しの人', level: 1 })).toBeVisible();
		await expect(page.getByRole('img')).toHaveCount(0);
	});

	test('設定するとプロフィールに出る', async ({ page }) => {
		const me = await signup(page, { displayName: 'アバターの人' });
		await setAvatar(page);

		await page.goto(`/users/${me.handle}`);
		// **画像は装飾なので alt は空。** 名前は隣に文字で出ている。
		await expect(page.locator('header img[alt=""]')).toBeVisible();
	});

	test('投稿カードにも出る', async ({ page }) => {
		await signup(page, { displayName: 'カードの人' });
		await setAvatar(page);

		const body = `アバター付きの投稿 ${Date.now()}`;
		await createPost(page, { body, prefecture: '北海道', alt: '北海道の写真' });

		await page.goto('/');
		const card = page.getByRole('article').filter({ hasText: body });
		// 投稿写真（alt あり）とアバター（alt 空）の2枚がある。
		await expect(card.locator('img[alt=""]')).toBeVisible();
	});

	test('外すと消える', async ({ page }) => {
		const me = await signup(page, { displayName: '外す人' });
		await setAvatar(page);

		await page.getByRole('button', { name: 'アバターを外す' }).click();
		await expect(page.getByRole('button', { name: 'アバターを外す' })).toBeHidden();

		// **サーバー側に残っていないこと。**
		await page.goto(`/users/${me.handle}`);
		await expect(page.locator('header img[alt=""]')).toBeHidden();
	});

	test('画像の種類を選べる旨と EXIF の扱いを伝える', async ({ page }) => {
		await signup(page);

		await page.goto('/settings/profile');
		await expect(page.getByText('位置情報などの撮影情報は保存時に取り除かれます')).toBeVisible();
	});

	test('アバターの設定にアクセシビリティ違反が無い', async ({ page }) => {
		await signup(page);
		await setAvatar(page);

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
