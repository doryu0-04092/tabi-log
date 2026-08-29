import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { createPost, signup, type TestUser } from './fixtures/app';

/** 別の利用者を作り、その画面を返す。呼び出し側が閉じる。 */
async function otherUser(
	browser: import('@playwright/test').Browser,
	baseURL: string | undefined,
	displayName: string
) {
	const context = await browser.newContext({ baseURL });
	const page = await context.newPage();
	const user = await signup(page, { displayName });
	return { context, page, user };
}

/** ヘッダーの通知リンク。未読があれば件数が付く。 */
function notificationLink(page: Page) {
	return page.getByRole('navigation', { name: '主要' }).getByRole('link', { name: /通知/ });
}

test.describe('通知', () => {
	test('何も起きていなければ空だと伝える', async ({ page }) => {
		await signup(page);

		await page.goto('/notifications');
		await expect(page.getByText('まだ通知はありません。')).toBeVisible();
	});

	test('いいねされると通知が届く', async ({ page, browser, baseURL }) => {
		await signup(page, { displayName: '投稿した人' });
		await createPost(page, { body: 'いいねされる投稿', prefecture: '北海道', alt: '北海道の写真' });
		const url = page.url();

		const other = await otherUser(browser, baseURL, 'いいねした人');
		await other.page.goto(url);
		await other.page.getByRole('button', { name: /^いいね/ }).click();
		await expect(other.page.getByRole('button', { name: /^いいね/ })).toHaveAttribute(
			'aria-pressed',
			'true'
		);
		await other.context.close();

		await page.goto('/notifications');
		await expect(page.getByText('いいねした人')).toBeVisible();
		await expect(page.getByText('があなたの投稿にいいねしました')).toBeVisible();
	});

	// **コメントの本文まで一覧に出す。** 開かないと何を言われたか
	// 分からないと、通知としての用をなさない。
	test('コメントされると本文つきで通知が届く', async ({ page, browser, baseURL }) => {
		await signup(page, { displayName: 'コメントされる人' });
		await createPost(page, { body: 'コメントされる投稿', prefecture: '京都府', alt: '京都の写真' });
		const url = page.url();

		const text = `通知に出るコメント ${Date.now()}`;
		const other = await otherUser(browser, baseURL, 'コメントした人');
		await other.page.goto(url);
		await other.page.getByLabel('コメントを書く').fill(text);
		await other.page.getByRole('button', { name: '送信する' }).click();
		await expect(other.page.getByText(text)).toBeVisible();
		await other.context.close();

		await page.goto('/notifications');
		await expect(page.getByText('があなたの投稿にコメントしました')).toBeVisible();
		await expect(page.getByText(text)).toBeVisible();
	});

	test('フォローされると通知が届く', async ({ page, browser, baseURL }) => {
		const me: TestUser = await signup(page, { displayName: 'フォローされる人' });

		const other = await otherUser(browser, baseURL, 'フォローした人');
		await other.page.goto(`/users/${me.handle}`);
		await other.page.getByRole('button', { name: /^フォローされる人を/ }).click();
		await expect(other.page.getByRole('button', { name: /^フォローされる人を/ })).toHaveAttribute(
			'aria-pressed',
			'true'
		);
		await other.context.close();

		await page.goto('/notifications');
		await expect(page.getByText('があなたをフォローしました')).toBeVisible();
	});

	// **自分の行為で自分に通知は来ない。**
	test('自分の投稿へのいいねでは通知が来ない', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: '自分でいいねする', prefecture: '沖縄県', alt: '沖縄の写真' });

		await page.getByRole('button', { name: /^いいね/ }).click();
		await expect(page.getByRole('button', { name: /^いいね/ })).toHaveAttribute(
			'aria-pressed',
			'true'
		);

		await page.goto('/notifications');
		await expect(page.getByText('まだ通知はありません。')).toBeVisible();
	});

	// **契機が取り消されると通知も消える。** 残ると「いいねされた」通知だけが宙に浮く。
	test('いいねが取り消されると通知も消える', async ({ page, browser, baseURL }) => {
		await signup(page, { displayName: '取り消される人' });
		await createPost(page, { body: '取り消しの検査', prefecture: '長野県', alt: '長野の写真' });
		const url = page.url();

		const other = await otherUser(browser, baseURL, '取り消す人');
		await other.page.goto(url);
		const like = other.page.getByRole('button', { name: /^いいね/ });
		await like.click();
		await expect(like).toHaveAttribute('aria-pressed', 'true');

		await page.goto('/notifications');
		await expect(page.getByText('があなたの投稿にいいねしました')).toBeVisible();

		await like.click();
		await expect(like).toHaveAttribute('aria-pressed', 'false');
		await other.context.close();

		await page.goto('/notifications');
		await expect(page.getByText('まだ通知はありません。')).toBeVisible();
	});

	test('ヘッダーに未読の件数が出て、既読にすると消える', async ({ page, browser, baseURL }) => {
		await signup(page, { displayName: '未読を見る人' });
		await createPost(page, { body: '未読の検査', prefecture: '広島県', alt: '広島の写真' });
		const url = page.url();

		const other = await otherUser(browser, baseURL, '未読を作る人');
		await other.page.goto(url);
		await other.page.getByRole('button', { name: /^いいね/ }).click();
		await expect(other.page.getByRole('button', { name: /^いいね/ })).toHaveAttribute(
			'aria-pressed',
			'true'
		);
		await other.context.close();

		// **数字だけでなく語も添える。** 「1」だけでは何の数か読み上げで分からない。
		await page.goto('/');
		await expect(notificationLink(page)).toContainText('1件の未読');

		await page.goto('/notifications');
		await expect(page.getByText('未読', { exact: true })).toBeVisible();
		await page.getByRole('button', { name: /すべて既読にする/ }).click();
		await expect(page.getByText('未読', { exact: true })).toBeHidden();

		await page.goto('/');
		await expect(notificationLink(page)).not.toContainText('未読');
	});

	test('1件だけ既読にできる', async ({ page, browser, baseURL }) => {
		await signup(page, { displayName: '1件既読の人' });
		await createPost(page, { body: '1件既読の検査', prefecture: '愛知県', alt: '愛知の写真' });
		const url = page.url();

		const other = await otherUser(browser, baseURL, '押した人');
		await other.page.goto(url);
		await other.page.getByRole('button', { name: /^いいね/ }).click();
		await expect(other.page.getByRole('button', { name: /^いいね/ })).toHaveAttribute(
			'aria-pressed',
			'true'
		);
		await other.context.close();

		await page.goto('/notifications');
		// 「すべて既読にする（1件）」も部分一致で拾うため、完全一致で絞る。
		await page.getByRole('button', { name: '既読にする', exact: true }).click();
		await expect(page.getByText('未読', { exact: true })).toBeHidden();

		// **サーバー側に残っていること。** 見た目だけ変わる実装を見逃さない。
		await page.reload();
		await expect(page.getByText('未読', { exact: true })).toBeHidden();
	});

	test('通知にアクセシビリティ違反が無い', async ({ page, browser, baseURL }) => {
		await signup(page, { displayName: '検査される人' });
		await createPost(page, { body: '検査用の投稿', prefecture: '石川県', alt: '石川の写真' });
		const url = page.url();

		const other = await otherUser(browser, baseURL, '検査する人');
		await other.page.goto(url);
		await other.page.getByLabel('コメントを書く').fill('検査用のコメント');
		await other.page.getByRole('button', { name: '送信する' }).click();
		await expect(other.page.getByText('検査用のコメント')).toBeVisible();
		await other.context.close();

		await page.goto('/notifications');
		await expect(page.getByText('があなたの投稿にコメントしました')).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
