import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { createPost, signup } from './fixtures/app';
import { PNG } from './fixtures/png';

test.describe('投稿', () => {
	test('画像を選んで投稿でき、詳細に表示される', async ({ page }) => {
		await signup(page, { displayName: '投稿する人' });
		await createPost(page, {
			body: '函館の朝市で海鮮丼を食べた',
			prefecture: '北海道'
		});

		await expect(page.getByText('函館の朝市で海鮮丼を食べた')).toBeVisible();
		await expect(page.getByText('北海道').first()).toBeVisible();
		await expect(page.getByText('訪問 2026年5月3日')).toBeVisible();
		// **画像の説明は入力させない方針にしたため alt は空である。**
		// 属性ごと消すと HTML として不正になり、読み上げがファイル名を
		// 読み上げてしまうので、alt="" は残している。
		await expect(page.locator('.photo-list img[alt=""]').first()).toBeVisible();
	});

	// **処理が終わる前は投稿させない。**
	// 送信直後は「準備しています」であり、送信ボタンは押せない。
	test('画像の準備が終わるまで投稿ボタンを押せない', async ({ page }) => {
		await signup(page);
		await page.goto('/posts/new');

		const submit = page.getByRole('button', { name: '投稿する' });
		await expect(submit).toBeDisabled();

		await page.setInputFiles('#photos', {
			name: 'photo.png',
			mimeType: 'image/png',
			buffer: PNG
		});

		// 準備が終わるまで押せないままであること。
		await expect(submit).toBeDisabled();
		await expect(page.getByText('使えます')).toBeVisible({ timeout: 30_000 });
		await expect(submit).toBeEnabled();
	});

	// **訪問日は任意になった**（2026-08-29）。覚えていない・特定の日に
	// 紐づかない投稿もあるため。省略した投稿は旅行履歴に出ない。
	test('訪問日を空のままでも投稿できる', async ({ page }) => {
		await signup(page);
		await page.goto('/posts/new');
		await page.setInputFiles('#photos', {
			name: 'photo.png',
			mimeType: 'image/png',
			buffer: PNG
		});
		await expect(page.getByText('使えます')).toBeVisible({ timeout: 30_000 });

		const body = `説明も訪問日も無い投稿 ${Date.now()}`;
		await page.getByLabel('都道府県（必須）').selectOption({ label: '東京都' });
		await page.getByLabel('本文（必須）').fill(body);
		await page.getByRole('button', { name: '投稿する' }).click();

		await expect(page).toHaveURL(/\/posts\/\d+$/);
		await expect(page.getByText(body)).toBeVisible();
		// **訪問日が無い投稿では「訪問 ◯年◯月◯日」を出さない。**
		await expect(page.getByText(/^訪問 /)).toBeHidden();
	});

	// 訪問日は「行った記録」なので未来を選べてはいけない。
	test('訪問日に未来を選べない', async ({ page }) => {
		await signup(page);
		await page.goto('/posts/new');

		const today = new Date().toISOString().slice(0, 10);
		await expect(page.getByLabel('訪問日')).toHaveAttribute('max', today);
	});

	test('投稿がフィードに出る', async ({ page }) => {
		await signup(page, { displayName: 'フィード確認' });

		// **本文は実行ごとに一意にする。** 固定の文言だと、過去の実行で作られた
		// 投稿とも一致し、セレクタが複数の要素を掴んで落ちる。
		const body = `フィードに出るはずの投稿 ${Date.now()}`;

		await createPost(page, { body, prefecture: '沖縄県' });

		await page.goto('/');
		await expect(page.getByText(body)).toBeVisible();
		const card = page.getByRole('article').filter({ hasText: body });
		await expect(card.locator('.photo-list img').first()).toBeVisible();
	});

	test('自分の投稿は削除できる', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: '削除する投稿', prefecture: '京都府' });

		await page.getByRole('button', { name: 'この投稿を削除する' }).click();
		// 取り消せない操作なので一段挟む。
		await expect(page.getByRole('alert')).toContainText('取り消せません');
		await page.getByRole('button', { name: '削除する' }).click();

		await expect(page).toHaveURL('/');
		await expect(page.getByText('削除する投稿')).toBeHidden();
	});

	// 他人の投稿には削除ボタンを出さない。
	// （権限の担保はサーバー側で行っており、これは表示の確認）
	test('他人の投稿には削除ボタンが出ない', async ({ page, browser, baseURL }) => {
		await signup(page, { displayName: '投稿者' });
		await createPost(page, { body: '他人が見る投稿', prefecture: '大阪府' });
		const url = page.url();

		// **別のブラウザコンテキストで開く。** 同じコンテキストだと Cookie を共有し、
		// 「別の利用者」になっていないのに気づけないまま通ってしまう。
		const otherContext = await browser.newContext({ baseURL });
		const other = await otherContext.newPage();
		// signup は表示名で本人確認まで行う。「ログアウト」が見えるだけだと、
		// 既存のセッションが復元されただけの状態と区別できない。
		await signup(other, { displayName: '別の人' });

		await other.goto(url);
		await expect(other.getByText('他人が見る投稿')).toBeVisible();
		await expect(other.getByRole('button', { name: 'この投稿を削除する' })).toBeHidden();

		await otherContext.close();
	});

	test('投稿作成画面にアクセシビリティ違反が無い', async ({ page }) => {
		await signup(page);
		await page.goto('/posts/new');
		await expect(page.getByRole('heading', { name: '投稿する', level: 1 })).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});

	test('投稿詳細にアクセシビリティ違反が無い', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: '検査用の投稿', prefecture: '長野県' });

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
