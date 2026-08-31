import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { createPost, signup } from './fixtures/app';

/** いいねボタン。件数が変わるため名前の前方一致で掴む。 */
function likeButton(page: Page) {
	return page.getByRole('button', { name: /^いいね/ });
}

test.describe('いいね', () => {
	test('押すと件数が増え、もう一度押すと戻る', async ({ page }) => {
		await signup(page, { displayName: 'いいねする人' });
		await createPost(page, { body: 'いいねされる投稿', prefecture: '北海道' });

		const like = likeButton(page);
		await expect(like).toHaveText(/いいね 0件/);
		// 押していない状態を aria-pressed でも示す。色の違いだけに頼らない。
		await expect(like).toHaveAttribute('aria-pressed', 'false');

		await like.click();
		await expect(like).toHaveText(/いいね 1件/);
		await expect(like).toHaveAttribute('aria-pressed', 'true');

		await like.click();
		await expect(like).toHaveText(/いいね 0件/);
		await expect(like).toHaveAttribute('aria-pressed', 'false');
	});

	// **サーバー側で数え直しても同じであることまで確かめる。**
	// 画面の数字だけ見ていると、見た目だけ動いて保存されていない実装を見逃す。
	test('いいねはリロードしても残る', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: '残るはずのいいね', prefecture: '沖縄県' });

		await likeButton(page).click();
		await expect(likeButton(page)).toHaveText(/いいね 1件/);

		await page.reload();
		await expect(likeButton(page)).toHaveText(/いいね 1件/);
		await expect(likeButton(page)).toHaveAttribute('aria-pressed', 'true');
	});

	test('いいねの状態はフィードにも出る', async ({ page }) => {
		await signup(page, { displayName: 'フィードのいいね' });
		const body = `フィードでいいねを確認する ${Date.now()}`;
		await createPost(page, { body, prefecture: '京都府' });

		await likeButton(page).click();
		await expect(likeButton(page)).toHaveText(/いいね 1件/);

		await page.goto('/');
		const card = page.getByRole('article').filter({ hasText: body });
		await expect(card.getByRole('button', { name: /^いいね/ })).toHaveAttribute(
			'aria-pressed',
			'true'
		);
	});
});

test.describe('コメント', () => {
	test('投稿してその場に出る', async ({ page }) => {
		await signup(page, { displayName: 'コメントする人' });
		await createPost(page, { body: 'コメントされる投稿', prefecture: '長野県' });

		await expect(page.getByText('まだコメントはありません。')).toBeVisible();

		await page.getByLabel('コメントを書く').fill('いい写真ですね');
		await page.getByRole('button', { name: '送信する' }).click();

		await expect(page.getByText('いい写真ですね')).toBeVisible();
		// 送信後は入力欄を空にする。同じ文字が残っていると二重送信のもとになる。
		await expect(page.getByLabel('コメントを書く')).toHaveValue('');
	});

	test('空白だけでは送信できない', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: '空白の検査', prefecture: '大阪府' });

		const submit = page.getByRole('button', { name: '送信する' });
		await expect(submit).toBeDisabled();

		await page.getByLabel('コメントを書く').fill('   ');
		await expect(submit).toBeDisabled();

		await page.getByLabel('コメントを書く').fill('あ');
		await expect(submit).toBeEnabled();
	});

	// **全角文字をバイト数で数えていないこと。**
	// 500文字ちょうどは通り、501文字で初めて止まる。
	test('全角500文字は送信でき、超えると止まる', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: '文字数の検査', prefecture: '福岡県' });

		const input = page.getByLabel('コメントを書く');
		const submit = page.getByRole('button', { name: '送信する' });

		await input.fill('あ'.repeat(501));
		await expect(page.getByText('501 / 500 文字')).toBeVisible();
		await expect(submit).toBeDisabled();

		await input.fill('い'.repeat(500));
		await expect(page.getByText('500 / 500 文字')).toBeVisible();
		await expect(submit).toBeEnabled();
		await submit.click();

		await expect(page.getByText('い'.repeat(500))).toBeVisible();
	});

	/*
	件数の表示が一覧と食い違わないこと。

	**サーバーは正しく数えている。** ずれるのは画面の中だけで、再読み込みすれば
	直る。それでも見るのは、コメントした直後に「0件」と出るのが
	**壊れているように見える**ためである。

	2026-08-31 にブラウザで一通り触って見つけた。既存の試験は
	「コメントが表示されること」しか見ておらず、**件数を見ていなかったため
	通っていた。**
	*/
	test('コメントを足すと件数も増える', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: '件数の検査', prefecture: '福井県' });

		await expect(page.getByText('コメント 0件')).toBeVisible();

		await page.getByLabel('コメントを書く').fill('数えられるコメント');
		await page.getByRole('button', { name: '送信する' }).click();
		await expect(page.getByText('数えられるコメント')).toBeVisible();

		// **再読み込みしない。** 読み直せば直るずれを見たいので、
		// その場で合っていることを確かめる。
		await expect(page.getByText('コメント 1件')).toBeVisible();
	});

	test('コメントを消すと件数も減る', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: '件数が減る検査', prefecture: '島根県' });

		await page.getByLabel('コメントを書く').fill('消される一言');
		await page.getByRole('button', { name: '送信する' }).click();
		await expect(page.getByText('コメント 1件')).toBeVisible();

		await page.getByRole('button', { name: '削除する', exact: true }).click();
		await expect(page.getByRole('alert')).toContainText('取り消せません');
		await page.getByRole('button', { name: '削除する', exact: true }).click();

		await expect(page.getByText('消される一言')).toBeHidden();
		await expect(page.getByText('コメント 0件')).toBeVisible();
	});

	test('コメントはリロードしても残る', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: '残るコメント', prefecture: '青森県' });

		const text = `保存されるコメント ${Date.now()}`;
		await page.getByLabel('コメントを書く').fill(text);
		await page.getByRole('button', { name: '送信する' }).click();
		await expect(page.getByText(text)).toBeVisible();

		await page.reload();
		await expect(page.getByText(text)).toBeVisible();
	});

	test('自分のコメントは削除できる', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: '削除の検査', prefecture: '広島県' });

		const text = `消すコメント ${Date.now()}`;
		await page.getByLabel('コメントを書く').fill(text);
		await page.getByRole('button', { name: '送信する' }).click();
		await expect(page.getByText(text)).toBeVisible();

		await page.getByRole('button', { name: '削除する', exact: true }).click();
		// 取り消せない操作なので一段挟む。
		await expect(page.getByRole('alert')).toContainText('取り消せません');
		await page.getByRole('button', { name: '削除する', exact: true }).click();

		await expect(page.getByText(text)).toBeHidden();
	});

	// **投稿の所有者は、他人が付けたコメントも消せる。**
	// 消せないと「投稿ごと削除する」以外の手段が無くなる。
	test('投稿の所有者は他人のコメントを削除できる', async ({ page, browser, baseURL }) => {
		await signup(page, { displayName: '投稿の持ち主' });
		await createPost(page, { body: '所有者の投稿', prefecture: '宮城県' });
		const url = page.url();

		// **別のブラウザコンテキストで開く。** 同じコンテキストだと Cookie を共有し、
		// 「別の利用者」になっていないのに気づけないまま通ってしまう。
		const otherContext = await browser.newContext({ baseURL });
		const other = await otherContext.newPage();
		await signup(other, { displayName: 'コメントした人' });

		const text = `他人のコメント ${Date.now()}`;
		await other.goto(url);
		await other.getByLabel('コメントを書く').fill(text);
		await other.getByRole('button', { name: '送信する' }).click();
		await expect(other.getByText(text)).toBeVisible();
		await otherContext.close();

		// 所有者の画面で、そのコメントに削除の導線が出ること。
		await page.reload();
		await expect(page.getByText(text)).toBeVisible();
		await page.getByRole('button', { name: '削除する', exact: true }).click();
		await expect(page.getByRole('alert')).toContainText('取り消せません');
		await page.getByRole('button', { name: '削除する', exact: true }).click();

		await expect(page.getByText(text)).toBeHidden();
	});

	// 無関係の利用者には削除の導線を出さない。
	// （権限の担保はサーバー側で行っており、これは表示の確認）
	test('無関係の利用者には削除ボタンが出ない', async ({ page, browser, baseURL }) => {
		await signup(page, { displayName: '書いた人' });
		await createPost(page, { body: '第三者の検査', prefecture: '愛知県' });
		const url = page.url();

		const text = `第三者には消せないコメント ${Date.now()}`;
		await page.getByLabel('コメントを書く').fill(text);
		await page.getByRole('button', { name: '送信する' }).click();
		await expect(page.getByText(text)).toBeVisible();

		const otherContext = await browser.newContext({ baseURL });
		const other = await otherContext.newPage();
		await signup(other, { displayName: '関係ない人' });

		await other.goto(url);
		await expect(other.getByText(text)).toBeVisible();
		await expect(other.getByRole('button', { name: '削除する', exact: true })).toBeHidden();

		await otherContext.close();
	});

	test('コメント欄にアクセシビリティ違反が無い', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: '検査用の投稿', prefecture: '石川県' });

		await page.getByLabel('コメントを書く').fill('検査用のコメント');
		await page.getByRole('button', { name: '送信する' }).click();
		await expect(page.getByText('検査用のコメント')).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
