import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { createPost, signup, toggleFollow } from './fixtures/app';

/** フォローボタン。文言が状態で変わるため、両方に当たる名前で掴む。 */
function followButton(page: Page, displayName: string) {
	return page.getByRole('button', { name: new RegExp(`^${displayName}を`) });
}

test.describe('プロフィール', () => {
	test('投稿者名から開ける', async ({ page }) => {
		await signup(page, { displayName: 'プロフィールの人' });
		const body = `プロフィールへの導線 ${Date.now()}`;
		await createPost(page, { body, prefecture: '北海道' });

		await page
			.getByRole('link', { name: /プロフィールの人/ })
			.first()
			.click();

		await expect(page).toHaveURL(/\/users\/[A-Za-z0-9_]+$/);
		await expect(page.getByRole('heading', { name: 'プロフィールの人', level: 1 })).toBeVisible();
		await expect(page.getByText(body)).toBeVisible();
	});

	// **数字だけを並べない。** 何の数かが読み上げで分かる形にする。
	test('件数が語とともに出る', async ({ page }) => {
		const me = await signup(page, { displayName: '件数の人' });
		await createPost(page, { body: '件数の検査', prefecture: '沖縄県' });

		await page.goto(`/users/${me.handle}`);
		await expect(page.getByRole('heading', { name: '件数の人', level: 1 })).toBeVisible();

		// 数字だけでなく単位まで含めて確かめる。「1」だけの表示では
		// 何の数か読み上げで分からない。
		await expect(page.getByText('1件')).toBeVisible();
		await expect(page.getByRole('link', { name: '0人' }).first()).toBeVisible();
		// 訪れた都道府県は 47 が分母。制覇率も数で示す。
		await expect(page.getByText('1 / 47（2%）')).toBeVisible();
	});

	// 自分にフォローの導線は出さない。押せない導線は迷いのもとになる。
	test('自分のプロフィールにはフォローボタンが出ない', async ({ page }) => {
		const user = await signup(page, { displayName: '自分' });

		await page.goto(`/users/${user.handle}`);
		await expect(page.getByRole('heading', { name: '自分', level: 1 })).toBeVisible();
		await expect(page.getByRole('button', { name: /フォロー/ })).toBeHidden();
	});

	test('存在しない利用者は見つからないと伝える', async ({ page }) => {
		await signup(page);

		await page.goto('/users/nobody_exists_here');
		await expect(page.getByRole('alert')).toContainText('利用者が見つかりません');
	});

	test('プロフィールにアクセシビリティ違反が無い', async ({ page }) => {
		await signup(page, { displayName: '検査の人' });
		await createPost(page, { body: '検査用の投稿', prefecture: '長野県' });
		await page
			.getByRole('link', { name: /検査の人/ })
			.first()
			.click();
		await expect(page.getByRole('heading', { name: '検査の人', level: 1 })).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});

test.describe('フォロー', () => {
	test('フォローするとフォロワー数が増え、解除すると戻る', async ({ page, browser, baseURL }) => {
		// フォローされる側を作る。
		const otherContext = await browser.newContext({ baseURL });
		const other = await otherContext.newPage();
		const target = await signup(other, { displayName: 'フォローされる人' });
		await otherContext.close();

		await signup(page, { displayName: 'フォローする人' });
		await page.goto(`/users/${target.handle}`);

		const follow = followButton(page, 'フォローされる人');
		await expect(follow).toHaveAttribute('aria-pressed', 'false');
		await expect(page.getByText('0人').first()).toBeVisible();

		await follow.click();
		await expect(follow).toHaveAttribute('aria-pressed', 'true');
		// ボタンだけ変わって数字が据え置きだと、反映されていないように見える。
		await expect(page.getByText('1人').first()).toBeVisible();

		await follow.click();
		await expect(follow).toHaveAttribute('aria-pressed', 'false');
		await expect(page.getByText('0人').first()).toBeVisible();
	});

	// **サーバー側に残っていることまで確かめる。**
	// 画面の状態だけ見ていると、見た目だけ動いて保存されていない実装を見逃す。
	test('フォローはリロードしても残る', async ({ page, browser, baseURL }) => {
		const otherContext = await browser.newContext({ baseURL });
		const other = await otherContext.newPage();
		const target = await signup(other, { displayName: '残るフォロー先' });
		await otherContext.close();

		await signup(page);
		await page.goto(`/users/${target.handle}`);
		await toggleFollow(page, '残るフォロー先', true);

		await page.reload();
		await expect(followButton(page, '残るフォロー先')).toHaveAttribute('aria-pressed', 'true');
	});

	test('フォローすると双方の一覧に出る', async ({ page, browser, baseURL }) => {
		const otherContext = await browser.newContext({ baseURL });
		const other = await otherContext.newPage();
		const target = await signup(other, { displayName: '一覧に出る人' });
		await otherContext.close();

		const me = await signup(page, { displayName: '一覧を見る人' });
		await page.goto(`/users/${target.handle}`);
		await toggleFollow(page, '一覧に出る人', true);

		// 相手のフォロワー一覧に自分が出る。
		await page.goto(`/users/${target.handle}/followers`);
		await expect(page.getByRole('heading', { name: 'フォロワー', level: 1 })).toBeVisible();
		// **本文の中で掴む。** ヘッダーの表示名も自分のプロフィールへのリンクなので、
		// 画面全体から探すと2つ見つかる。
		await expect(page.locator('#main').getByRole('link', { name: /一覧を見る人/ })).toBeVisible();

		// 自分のフォロー中一覧に相手が出る。
		await page.goto(`/users/${me.handle}/following`);
		await expect(page.getByRole('heading', { name: 'フォロー中', level: 1 })).toBeVisible();
		await expect(page.getByRole('link', { name: /一覧に出る人/ })).toBeVisible();
	});

	test('誰もいない一覧でも空だと伝える', async ({ page }) => {
		const me = await signup(page);

		await page.goto(`/users/${me.handle}/followers`);
		await expect(page.getByText('まだフォロワーはいません。')).toBeVisible();

		await page.goto(`/users/${me.handle}/following`);
		await expect(page.getByText('まだ誰もフォローしていません。')).toBeVisible();
	});

	// 一覧には同じ見た目のボタンが並ぶ。読み上げだけで区別できる必要がある。
	test('一覧のフォローボタンが相手ごとに区別できる', async ({ page, browser, baseURL }) => {
		const otherContext = await browser.newContext({ baseURL });
		const other = await otherContext.newPage();
		const target = await signup(other, { displayName: '区別される人' });
		await otherContext.close();

		const me = await signup(page, { displayName: '区別する人' });
		await page.goto(`/users/${target.handle}`);
		await toggleFollow(page, '区別される人', true);

		await page.goto(`/users/${me.handle}/following`);
		// 名前を含む読み上げ用のラベルで、どの相手のボタンかが分かる。
		await expect(
			page.getByRole('button', { name: '区別される人をフォロー中。押すと解除します' })
		).toBeVisible();
	});

	test('フォロワー一覧にアクセシビリティ違反が無い', async ({ page, browser, baseURL }) => {
		const otherContext = await browser.newContext({ baseURL });
		const other = await otherContext.newPage();
		const target = await signup(other, { displayName: '検査のフォロー先' });
		await otherContext.close();

		await signup(page, { displayName: '検査のフォロワー' });
		await page.goto(`/users/${target.handle}`);
		await toggleFollow(page, '検査のフォロー先', true);

		await page.goto(`/users/${target.handle}/followers`);
		// **本文の中で掴む。** ヘッダーの表示名も自分のプロフィールへのリンクなので、
		// 画面全体から探すと2つ見つかる（131行目と同じ理由）。
		// **ここだけ絞り忘れていた。** 描画の速さ次第で片方しか見つからず、
		// 通ったり落ちたりしていた（CI で表面化）。
		await expect(
			page.locator('#main').getByRole('link', { name: /検査のフォロワー/ })
		).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
