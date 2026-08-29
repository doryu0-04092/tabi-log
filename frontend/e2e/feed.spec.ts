import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { createPost, signup, toggleFollow } from './fixtures/app';

test.describe('フィードの切り替え', () => {
	test('既定は新着で、現在地が分かる', async ({ page }) => {
		await signup(page);

		await expect(page.getByRole('link', { name: '新着' })).toHaveAttribute('aria-current', 'page');
		// 色の違いだけに頼らず、現在地を aria-current で示す。
		await expect(page.getByRole('link', { name: 'フォロー中' })).not.toHaveAttribute(
			'aria-current',
			'page'
		);
	});

	// **タブはリンクにしている。** URL に状態があるので、リロードしても
	// 開いていた側に戻る。
	test('フォロー中はリロードしても保たれる', async ({ page }) => {
		await signup(page);

		await page.getByRole('link', { name: 'フォロー中' }).click();
		await expect(page).toHaveURL(/\?tab=following$/);
		await expect(page.getByRole('link', { name: 'フォロー中' })).toHaveAttribute(
			'aria-current',
			'page'
		);

		await page.reload();
		await expect(page.getByRole('link', { name: 'フォロー中' })).toHaveAttribute(
			'aria-current',
			'page'
		);
	});

	// 何も出ない画面を作らない。次に何をすればよいかを示す。
	test('フォローしていないと次の一手を示す', async ({ page }) => {
		await signup(page);

		await page.goto('/?tab=following');
		await expect(page.getByText('フォロー中の人の投稿はまだありません。')).toBeVisible();
		await expect(page.getByText('気になる人を見つけてフォローすると')).toBeVisible();
	});

	// **自分の投稿はフォロー中フィードに出ない。** 自分自身はフォローできないため。
	test('自分の投稿はフォロー中フィードに出ない', async ({ page }) => {
		await signup(page);
		const body = `自分の投稿 ${Date.now()}`;
		await createPost(page, { body, prefecture: '北海道' });

		await page.goto('/');
		await expect(page.getByText(body)).toBeVisible();

		await page.goto('/?tab=following');
		await expect(page.getByText(body)).toBeHidden();
		await expect(page.getByText('フォロー中の人の投稿はまだありません。')).toBeVisible();
	});

	test('フォローするとその人の投稿が並ぶ', async ({ page, browser, baseURL }) => {
		// 相手を作り、投稿させる。
		const otherContext = await browser.newContext({ baseURL });
		const other = await otherContext.newPage();
		const target = await signup(other, { displayName: 'フィードに出る人' });
		const body = `フォロー中フィードの投稿 ${Date.now()}`;
		await createPost(other, { body, prefecture: '沖縄県' });
		await otherContext.close();

		await signup(page, { displayName: 'フィードを見る人' });

		// フォローする前は出ない。
		await page.goto('/?tab=following');
		await expect(page.getByText(body)).toBeHidden();

		await page.goto(`/users/${target.handle}`);
		await toggleFollow(page, 'フィードに出る人', true);

		await page.goto('/?tab=following');
		await expect(page.getByText(body)).toBeVisible();

		// 解除すると消える。
		await page.goto(`/users/${target.handle}`);
		await toggleFollow(page, 'フィードに出る人', false);

		await page.goto('/?tab=following');
		await expect(page.getByText(body)).toBeHidden();
	});

	test('フォロー中フィードにアクセシビリティ違反が無い', async ({ page }) => {
		await signup(page);

		await page.goto('/?tab=following');
		await expect(page.getByRole('heading', { name: 'ホーム', level: 1 })).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
