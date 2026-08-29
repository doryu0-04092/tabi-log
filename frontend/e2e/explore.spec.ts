import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { createPost, signup } from './fixtures/app';

test.describe('発見', () => {
	test('ヘッダーから開ける', async ({ page }) => {
		await signup(page);

		await page.getByRole('link', { name: '検索' }).click();
		await expect(page).toHaveURL('/explore');
		await expect(page.getByRole('heading', { name: '詳細検索', level: 1 })).toBeVisible();
	});

	// 条件が無いうちは全件を出さない。全件は「発見」ではない。
	test('条件を入れるまでは結果を出さない', async ({ page }) => {
		await signup(page);

		await page.goto('/explore');
		await expect(page.getByText('条件を入れて「探す」を押してください。')).toBeVisible();
	});

	test('キーワードで投稿を探せる', async ({ page }) => {
		await signup(page, { displayName: '検索する人' });
		const word = `函館朝市${Date.now()}`;
		await createPost(page, {
			body: `${word}で海鮮丼を食べた`,
			prefecture: '北海道'
		});

		await page.goto('/explore');
		await page.getByLabel('キーワード').fill(word);
		await page.getByRole('button', { name: 'この条件で探す' }).click();

		await expect(page.getByText(`${word}で海鮮丼を食べた`)).toBeVisible();
	});

	// **1文字では全文検索の索引（ngram、トークン長2）に当たらない。**
	// 黙って0件にせず、理由を伝える。
	test('1文字のキーワードは理由を添えて止める', async ({ page }) => {
		await signup(page);

		await page.goto('/explore');
		await page.getByLabel('キーワード').fill('海');
		await page.getByRole('button', { name: 'この条件で探す' }).click();

		await expect(page.getByRole('alert')).toContainText('2文字以上');
	});

	test('都道府県で絞り込める', async ({ page }) => {
		await signup(page, { displayName: '絞り込む人' });
		const hokkaido = `北海道の投稿 ${Date.now()}`;
		const okinawa = `沖縄の投稿 ${Date.now()}`;
		await createPost(page, { body: hokkaido, prefecture: '北海道' });
		await createPost(page, { body: okinawa, prefecture: '沖縄県' });

		await page.goto('/explore');
		await page.getByLabel('都道府県').selectOption({ label: '沖縄県' });
		await page.getByRole('button', { name: 'この条件で探す' }).click();

		await expect(page.getByText(okinawa)).toBeVisible();
		await expect(page.getByText(hokkaido)).toBeHidden();
	});

	// **条件は URL に載る。** リンクを送れば相手も同じ結果を見られる。
	test('条件が URL に残り、リロードしても同じ結果になる', async ({ page }) => {
		await signup(page, { displayName: 'URL の人' });
		const word = `URLに残る${Date.now()}`;
		await createPost(page, { body: `${word}の投稿`, prefecture: '京都府' });

		await page.goto('/explore');
		await page.getByLabel('キーワード').fill(word);
		await page.getByRole('button', { name: 'この条件で探す' }).click();

		await expect(page).toHaveURL(new RegExp(`q=${encodeURIComponent(word)}`));
		await expect(page.getByText(`${word}の投稿`)).toBeVisible();

		await page.reload();
		// 入力欄にも条件が戻ること。空になると、何で絞ったのか分からなくなる。
		await expect(page.getByLabel('キーワード')).toHaveValue(word);
		await expect(page.getByText(`${word}の投稿`)).toBeVisible();
	});

	// 0件はエラーではない。次にどうすればよいかを添える。
	test('該当が無いときは空の結果として伝える', async ({ page }) => {
		await signup(page);

		await page.goto('/explore');
		await page.getByLabel('キーワード').fill(`該当しない語${Date.now()}`);
		await page.getByRole('button', { name: 'この条件で探す' }).click();

		await expect(page.getByText('条件に合う投稿は見つかりませんでした')).toBeVisible();
	});

	test('利用者を探せる', async ({ page, browser, baseURL }) => {
		const name = `探される人${Date.now()}`;
		const otherContext = await browser.newContext({ baseURL });
		const other = await otherContext.newPage();
		await signup(other, { displayName: name });
		await otherContext.close();

		await signup(page, { displayName: '探す人' });
		await page.goto('/explore');
		await page.getByLabel('キーワード').fill(name);
		await page.getByLabel('利用者').check();
		await page.getByRole('button', { name: 'この条件で探す' }).click();

		await expect(page.getByRole('link', { name: new RegExp(name) })).toBeVisible();
		// 検索結果からそのままフォローできる。
		await expect(page.getByRole('button', { name: new RegExp(`^${name}を`) })).toBeVisible();
	});

	// 利用者を探すときに投稿向けの絞り込みを出しても効かない。
	test('利用者を探すときは投稿向けの絞り込みを出さない', async ({ page }) => {
		await signup(page);

		await page.goto('/explore');
		await expect(page.getByLabel('都道府県')).toBeVisible();

		await page.getByLabel('利用者').check();
		await expect(page.getByLabel('都道府県')).toBeHidden();
		await expect(page.getByLabel('タグ')).toBeHidden();
	});

	test('発見にアクセシビリティ違反が無い', async ({ page }) => {
		await signup(page);

		await page.goto('/explore');
		await expect(page.getByRole('heading', { name: '詳細検索', level: 1 })).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
