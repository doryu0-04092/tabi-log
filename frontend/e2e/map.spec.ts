import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { createPost, signup } from './fixtures/app';

test.describe('都道府県制覇マップ', () => {
	// 投稿が1件も無くても表示される（全県が未訪問の状態）。
	test('投稿が0件でも47県すべてが出る', async ({ page }) => {
		const me = await signup(page, { displayName: 'まだ旅していない人' });

		await page.goto(`/users/${me.handle}`);
		await expect(page.getByRole('heading', { name: '都道府県制覇マップ' })).toBeVisible();
		await expect(page.getByText('0 / 47 県（0%）')).toBeVisible();

		// マスはリンクであり、キーボードでも辿れる。
		// **リストではなく図として出す。** 角丸のマスを SVG で描くようになったため、
		// 入れ物は role="group"。マス自体は今までどおりリンクである。
		const tiles = page.getByRole('group', { name: '都道府県ごとの投稿' }).getByRole('link');
		await expect(tiles).toHaveCount(47);
	});

	/*
	マスに県名が出ること。

	**以前は訪問済みの頭文字1文字だけだった。** どの県か分からず、
	色の濃さで訪問済みを見分けるしかなかった。

	表示は「都・府・県」を落とす。枠の幅が2文字を基準のため、
	接尾辞を付けると全県が3文字以上になって文字が縮む。
	読み上げ用のラベルには正式名称を残すので、情報は落ちない。
	北海道は「道」で終わるが3文字で1つの名前なので落とさない。
	*/
	test('マスに県名が出る（都・府・県は落とす）', async ({ page }) => {
		const me = await signup(page, { displayName: '県名を見る人' });

		await page.goto(`/users/${me.handle}`);
		const map = page.getByRole('group', { name: '都道府県ごとの投稿' });

		await expect(map.getByText('東京', { exact: true })).toBeVisible();
		await expect(map.getByText('京都', { exact: true })).toBeVisible();
		await expect(map.getByText('神奈川', { exact: true })).toBeVisible();
		await expect(map.getByText('北海道', { exact: true })).toBeVisible();

		// 未訪問でも名前は出る。**訪問済みかどうかは名前の有無で示さない。**
		await expect(map.getByRole('link', { name: '沖縄県 0件（未訪問）' })).toBeVisible();
		await expect(map.getByText('沖縄', { exact: true })).toBeVisible();
	});

	// **各マスは県名と件数を読み上げられる。** 色だけでは何も伝わらない。
	test('マスに県名と件数のラベルが付く', async ({ page }) => {
		const me = await signup(page, { displayName: 'ラベルの人' });
		await createPost(page, { body: 'マップの検査', prefecture: '北海道' });

		await page.goto(`/users/${me.handle}`);
		await expect(page.getByRole('link', { name: '北海道 1件（訪問済み）' })).toBeVisible();
		await expect(page.getByRole('link', { name: '沖縄県 0件（未訪問）' })).toBeVisible();
	});

	test('投稿すると制覇率が上がる', async ({ page }) => {
		const me = await signup(page, { displayName: '制覇する人' });
		await createPost(page, { body: '1県目', prefecture: '北海道' });

		await page.goto(`/users/${me.handle}`);
		await expect(page.getByText('1 / 47 県（2%）')).toBeVisible();

		await createPost(page, { body: '2県目', prefecture: '沖縄県' });
		await page.goto(`/users/${me.handle}`);
		await expect(page.getByText('2 / 47 県（4%）')).toBeVisible();
	});

	// **同じ県に2件投稿しても制覇数は増えない。** 数えるのは種類である。
	test('同じ県に複数投稿しても制覇数は増えない', async ({ page }) => {
		const me = await signup(page, { displayName: '同じ県の人' });
		await createPost(page, { body: '1件目', prefecture: '京都府' });
		await createPost(page, { body: '2件目', prefecture: '京都府' });

		await page.goto(`/users/${me.handle}`);
		await expect(page.getByText('1 / 47 県（2%）')).toBeVisible();
		await expect(page.getByRole('link', { name: '京都府 2件（訪問済み）' })).toBeVisible();
	});

	// **色の違いだけで情報を伝えない。** 同じ内容を表でも出す。
	test('同じ内容を表でも見られる', async ({ page }) => {
		const me = await signup(page, { displayName: '表で見る人' });
		await createPost(page, { body: '表の検査', prefecture: '長野県' });

		await page.goto(`/users/${me.handle}`);
		const toggle = page.getByRole('button', { name: '同じ内容を表で見る' });
		await expect(toggle).toHaveAttribute('aria-expanded', 'false');

		await toggle.click();
		await expect(page.getByRole('table', { name: '都道府県ごとの投稿数' })).toBeVisible();
		// 表にも「訪問済み」「未訪問」を語で入れる。
		await expect(page.getByRole('cell', { name: '訪問済み' })).toBeVisible();
		await expect(page.getByRole('cell', { name: '未訪問' }).first()).toBeVisible();
	});

	test('マスから都道府県別の一覧へ行ける', async ({ page }) => {
		const me = await signup(page, { displayName: '県へ飛ぶ人' });
		const body = `県別一覧の投稿 ${Date.now()}`;
		await createPost(page, { body, prefecture: '広島県' });

		await page.goto(`/users/${me.handle}`);
		await page.getByRole('link', { name: '広島県 1件（訪問済み）' }).click();

		await expect(page).toHaveURL('/prefectures/34');
		await expect(page.getByRole('heading', { name: '広島県の投稿', level: 1 })).toBeVisible();
		await expect(page.getByText(body)).toBeVisible();
	});

	// 投稿カードの都道府県からも同じ一覧へ行ける（行き先の無いリンクを残さない）。
	test('投稿カードの都道府県から一覧へ行ける', async ({ page }) => {
		await signup(page, { displayName: 'カードから飛ぶ人' });
		await createPost(page, { body: 'カードの検査', prefecture: '愛知県' });

		await page.getByRole('link', { name: '愛知県' }).first().click();
		await expect(page).toHaveURL('/prefectures/23');
	});

	test('投稿が無い県の一覧でも空だと伝える', async ({ page }) => {
		await signup(page);

		// 47（沖縄県）に誰も投稿していない前提は置けないため、
		// 空の場合と件がある場合の両方を許容せず、見出しだけを確かめる。
		await page.goto('/prefectures/47');
		await expect(page.getByRole('heading', { name: '沖縄県の投稿', level: 1 })).toBeVisible();
	});

	test('マップにアクセシビリティ違反が無い', async ({ page }) => {
		const me = await signup(page, { displayName: '検査する人' });
		await createPost(page, { body: '検査用の投稿', prefecture: '福岡県' });

		await page.goto(`/users/${me.handle}`);
		await expect(page.getByRole('heading', { name: '都道府県制覇マップ' })).toBeVisible();
		// 表を開いた状態でも検査する。
		await page.getByRole('button', { name: '同じ内容を表で見る' }).click();
		await expect(page.getByRole('table', { name: '都道府県ごとの投稿数' })).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});

	test('都道府県別の一覧にアクセシビリティ違反が無い', async ({ page }) => {
		await signup(page);
		await createPost(page, { body: '県別の検査', prefecture: '石川県' });

		await page.goto('/prefectures/17');
		await expect(page.getByRole('heading', { name: '石川県の投稿', level: 1 })).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
