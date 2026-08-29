import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { createPostViaApi, createPostsViaApi, followViaApi, signupViaApi } from './fixtures/app';

/*
 * タイムラインの読み進めと新着の知らせ。
 *
 * **どちらも「1ページに収まらない」「あとから増える」という前提が要る。**
 * 画面から 21 件作ると検証したいものと関係のない待ち時間で不安定になるため、
 * 前提づくりだけ API で行う（fixtures/app.ts の後半）。
 *
 * ---
 *
 * **見るのは新着ではなくフォロー中のフィードにしている。** テストは並列に
 * 走るため、新着フィードには他のテストが作った投稿が割り込む。実際に
 * 「21件作って1ページ目に20件」という前提が、別のテストの投稿に
 * 押し出されて崩れた。フォロー中フィードなら、そのテストが
 * フォローした相手の投稿しか入らないので前提が壊れない。
 */
test.describe('タイムライン', () => {
	// 1ページは20件。21件目は次のページに入る。
	const PAGE_SIZE = 20;

	/** 読む人と投稿する人を用意し、読む人が投稿する人をフォローした状態にする。 */
	async function pair(
		request: import('@playwright/test').APIRequestContext,
		browser: import('@playwright/test').Browser,
		baseURL?: string
	) {
		const reader = await signupViaApi(request, { displayName: '読んでいる人' });
		const authorContext = await browser.newContext({ baseURL });
		const author = await signupViaApi(authorContext.request, { displayName: '投稿している人' });
		await followViaApi(request, reader.token, author.user.handle);
		return { reader, author, authorContext };
	}

	test('下までスクロールすると次のページが自動で読み込まれる', async ({
		page,
		browser,
		baseURL
	}) => {
		const { author, authorContext } = await pair(page.request, browser, baseURL);
		const label = `送り${Date.now()}`;
		await createPostsViaApi(authorContext.request, author.token, PAGE_SIZE + 1, label);
		await authorContext.close();

		await page.goto('/?tab=following');

		await expect(page.getByText(`${label} ${PAGE_SIZE + 1}`)).toBeVisible();

		// **「1ページ目だけが出ている」状態は判定しない。**
		// 画像の高さが決まる前は一覧が短く、その時点で番兵が画面に入るため、
		// スクロールしなくても次のページの読み込みが始まることがある。
		// 途中の状態を条件にすると、速い環境と遅い環境で結果が変わる。

		// **押さずに最後まで読み込まれることを確かめる。**
		// ボタンを押して増えるだけなら、これまでと同じ挙動である。
		await page.mouse.wheel(0, 100_000);

		await expect(page.getByText(`${label} 1`, { exact: true })).toBeVisible({ timeout: 15_000 });

		// 続きが無くなればボタンごと消える。最後まで到達した証拠になる。
		await expect(page.getByRole('button', { name: '古い投稿をさらに読み込む' })).toBeHidden();
	});

	test('自動で読み込めない場合に備えてボタンも残っている', async ({ page, browser, baseURL }) => {
		const { author, authorContext } = await pair(page.request, browser, baseURL);
		const label = `手動${Date.now()}`;
		await createPostsViaApi(authorContext.request, author.token, PAGE_SIZE + 1, label);
		await authorContext.close();

		await page.goto('/?tab=following');
		await expect(page.getByText(`${label} ${PAGE_SIZE + 1}`)).toBeVisible();

		// キーボードだけで操作する人はスクロールで自動読み込みに頼れない。
		// 明示的に押せるものが必要になる。
		await expect(page.getByRole('button', { name: '古い投稿をさらに読み込む' })).toBeVisible();
	});

	test('新しい投稿が届くと帯で知らせ、押すと反映される', async ({ page, browser, baseURL }) => {
		const { author, authorContext } = await pair(page.request, browser, baseURL);

		const first = `先にあった投稿 ${Date.now()}`;
		await createPostViaApi(authorContext.request, author.token, { body: first });

		await page.goto('/?tab=following');
		await expect(page.getByText(first)).toBeVisible();

		const later = `あとから来た投稿 ${Date.now()}`;
		await createPostViaApi(authorContext.request, author.token, { body: later });
		await authorContext.close();

		// **確認は30秒ごと。** テストで30秒待つ代わりに、
		// 「タブに戻ってきた」ときの即時確認と同じ経路を通す。
		await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));

		const banner = page.getByRole('button', { name: /新しい投稿が \d+件/ });
		await expect(banner).toBeVisible({ timeout: 10_000 });

		// **押すまで差し込まない。** 読んでいる位置がずれるため。
		await expect(page.getByText(later)).toBeHidden();

		await banner.click();
		await expect(page.getByText(later)).toBeVisible();
		await expect(banner).toBeHidden();
	});

	test('新着の帯が出ている状態でアクセシビリティ違反が無い', async ({ page, browser, baseURL }) => {
		const { author, authorContext } = await pair(page.request, browser, baseURL);
		await createPostViaApi(authorContext.request, author.token, { body: `検査用 ${Date.now()}` });

		await page.goto('/?tab=following');
		await expect(page.getByRole('heading', { name: 'ホーム', level: 1 })).toBeVisible();
		await expect(page.getByRole('article').first()).toBeVisible();

		await createPostViaApi(authorContext.request, author.token, {
			body: `検査用の新着 ${Date.now()}`
		});
		await authorContext.close();

		await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
		await expect(page.getByRole('button', { name: /新しい投稿が \d+件/ })).toBeVisible({
			timeout: 10_000
		});

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
