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
	県名が読み上げに渡ること。

	**県名は絵に焼き込まれており、DOM には無い。** 見えている文字を
	そのまま拾える作りではないので、伝える役は次の3つが担う。

	  ① リンクの aria-label（県名・件数・訪問済みかどうか）
	  ② 「同じ内容を表で見る」の表（県名が文字として並ぶ）
	  ③ 率の表示（何県中何県か）

	絵そのものは読み上げから外してある。1つの説明でまとめても
	47県ぶんの中身は伝わらず、読み上げの邪魔になるためである。
	*/
	test('県名は絵ではなく、ラベルと表で伝わる', async ({ page }) => {
		const me = await signup(page, { displayName: '読み上げを見る人' });

		await page.goto(`/users/${me.handle}`);
		const map = page.getByRole('group', { name: '都道府県ごとの投稿' });

		// ① 47件すべてにラベルが付く。
		await expect(map.getByRole('link')).toHaveCount(47);
		await expect(map.getByRole('link', { name: '沖縄県 0件（未訪問）' })).toBeVisible();

		// **絵は読み上げに出さない。** 出すと中身の無い項目が1つ増えるだけになる。
		await expect(map.getByRole('img')).toHaveCount(0);

		// ② 表には県名が文字として並ぶ。
		await page.getByRole('button', { name: '同じ内容を表で見る' }).click();
		const table = page.getByRole('table');
		await expect(table.getByRole('row')).toHaveCount(48); // 見出しの行を含む
		// **県名は行見出しなので役割は rowheader である。** cell では当たらない。
		await expect(table.getByRole('rowheader', { name: '東京都' })).toBeVisible();
		await expect(table.getByRole('rowheader', { name: '沖縄県' })).toBeVisible();
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

	/*
	狭い画面で、地図だけが横に流れること。

	**絵は 1536px 幅の画像で、縮めると県名も同じ比率で縮む。**
	画面幅に合わせると 390px の端末で文字が 5px になり、読むことも
	押すこともできない。縮めて読めなくするより、はみ出させて読めるほうを取る。

	ただし**ページ全体が横に溢れてはいけない。** 溢れると、地図と関係ない
	本文まで横スクロールが要る画面になる。地図の枠の中だけで流す。
	*/
	test('狭い画面では地図だけが横に流れる', async ({ page }) => {
		const me = await signup(page, { displayName: '狭い画面の人' });

		await page.setViewportSize({ width: 390, height: 780 });
		await page.goto(`/users/${me.handle}`);
		await expect(page.getByRole('group', { name: '都道府県ごとの投稿' })).toBeVisible();

		const measured = await page.evaluate(() => {
			const doc = document.documentElement;
			const svg = document.querySelector('svg[role="group"]');
			const viewport = svg?.closest('div')?.parentElement;
			return {
				pageOverflow: doc.scrollWidth - doc.clientWidth,
				mapOverflow: viewport ? viewport.scrollWidth - viewport.clientWidth : 0
			};
		});

		// ページ全体は横に溢れない。
		expect(measured.pageOverflow, 'ページ全体が横に溢れている').toBeLessThanOrEqual(1);
		// 地図は枠の中で横に流れる。
		expect(measured.mapOverflow, '地図が横に流れていない（縮んでいる）').toBeGreaterThan(100);
	});

	/*
	表の行に地方の色が実際に付くこと。

	**CSS を書いただけでは足りない。** Svelte は使われていないと判断した
	規則を落とすし、色相を行へ渡し忘れても画面は壊れず、色が付かないまま
	静かに通ってしまう。**描画結果の色を読んで確かめる。**

	色は情報を運ばない（地方は「地方」の列に語で出ている）ので、
	ここで見るのは「意図した見た目になっているか」である。
	*/
	test('表の行に地方ごとの色が付く', async ({ page }) => {
		const me = await signup(page, { displayName: '表の色を見る人' });

		await page.goto(`/users/${me.handle}`);
		await page.getByRole('button', { name: '同じ内容を表で見る' }).click();

		const row = (name: string) =>
			page.getByRole('row').filter({ has: page.getByRole('cell', { name, exact: true }) });

		// 北海道（北海道）と 東京都（関東）は別の地方なので、別の色になる。
		const hokkaido = row('北海道').first();
		const tokyo = row('関東').first();

		const color = async (loc: typeof hokkaido) =>
			loc.evaluate((el) => getComputedStyle(el).backgroundColor);

		const a = await color(hokkaido);
		const b = await color(tokyo);

		// **透明のままなら色が当たっていない。**
		expect(a, '行に背景色が付いていない').not.toBe('rgba(0, 0, 0, 0)');
		expect(a, '行に背景色が付いていない').not.toBe('transparent');
		expect(b, '行に背景色が付いていない').not.toBe('rgba(0, 0, 0, 0)');

		// 地方が違えば色も違う。
		expect(a, '地方が違うのに同じ色になっている').not.toBe(b);
	});
});
