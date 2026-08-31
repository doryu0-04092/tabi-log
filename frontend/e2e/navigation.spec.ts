/*
画面の移動にまつわる2つの不具合を、まず**落ちる形で固定する。**

2026-09-01 に指摘を受けた。どちらも実装は入っているのに効いておらず、
**CI は緑のまま素通りしていた。** 該当を確かめるテストが1つも無かったためである。

  ① 「この条件で探す」を押しても、検索結果まで画面が動かない
  ② どの画面からでも一つ前へ戻れるはずが、常に「ホームへ戻る」になる

原因の推測は書かない。まず再現させ、直したあとに理由を書く。
*/
import { expect, test, type Page } from '@playwright/test';
import { createPost, signup } from './fixtures/app';

test.describe('検索結果への移動', () => {
	/*
	検索したら結果が見える位置まで動くこと。

	**押した場所と結果が出る場所が離れている。** 絞り込みの入力欄が縦に並ぶため、
	狭い画面では結果が折り返しの下に隠れる。押しても何も起きていないように見える。
	*/
	test('「この条件で探す」を押すと検索結果まで画面が動く', async ({ page }) => {
		await signup(page, { displayName: 'スクロールの人' });

		// **見出しが折り返しの下にある状態を作る。** 画面が広いと最初から
		// 見えてしまい、動いたかどうかを確かめられない。
		await page.setViewportSize({ width: 480, height: 640 });
		await page.goto('/explore');

		const heading = page.getByRole('heading', { name: '結果' });
		await expect(heading).toBeAttached();

		const before = await heading.boundingBox();
		expect(before, '結果の見出しの位置が取れない').not.toBeNull();
		expect(before!.y, '前提: 押す前は結果が折り返しの下にある').toBeGreaterThan(640);

		await page.getByLabel('キーワード').fill('うどん');
		await page.getByRole('button', { name: 'この条件で探す' }).click();

		// 滑らかに動くので、止まるまで待つ。
		await expect
			.poll(async () => (await heading.boundingBox())?.y ?? Number.POSITIVE_INFINITY, {
				message: '結果の見出しが画面の中へ入ってこない',
				timeout: 10_000
			})
			.toBeLessThan(640);

		// 画面そのものが動いたことも確かめる。**見出しが偶然見えただけと区別する。**
		expect(await page.evaluate(() => window.scrollY), '画面が動いていない').toBeGreaterThan(0);
	});
});

test.describe('一つ前の画面へ戻る', () => {
	/*
	制覇マップ → 県別の一覧、と辿ったあとに元の画面へ戻れること。

	**行き先を固定にすると戻れない。** プロフィールから県別の一覧へ飛んだあと
	「ホームへ戻る」しか無いと、見ていた相手のプロフィールへは戻れない。
	*/
	test('制覇マップから県別の一覧へ行くと「前の画面へ戻る」が出る', async ({ page }) => {
		const me = await signup(page, { displayName: '戻る人' });
		await createPost(page, { body: '戻る導線の確認', prefecture: '香川県' });

		await page.goto(`/users/${me.handle}`);
		await page.getByRole('link', { name: '香川県 1件（訪問済み）' }).click();
		await expect(page).toHaveURL(/\/prefectures\/37$/);

		await expect(
			page.getByRole('button', { name: '前の画面へ戻る' }),
			'辿ってきたのに「前の画面へ戻る」が出ていない'
		).toBeVisible();
	});

	test('「前の画面へ戻る」で元のプロフィールに戻る', async ({ page }) => {
		const me = await signup(page, { displayName: '戻って確かめる人' });
		await createPost(page, { body: '戻り先の確認', prefecture: '徳島県' });

		await page.goto(`/users/${me.handle}`);
		await page.getByRole('link', { name: '徳島県 1件（訪問済み）' }).click();
		await expect(page).toHaveURL(/\/prefectures\/36$/);

		await page.getByRole('button', { name: '前の画面へ戻る' }).click();
		await expect(page).toHaveURL(new RegExp(`/users/${me.handle}$`));
	});

	/*
	直接開いたときは戻り先が無いので、決まった行き先を出すこと。

	**押すとアプリの外へ出てしまう。** 履歴が無い状態で history.back() を
	呼ぶと、直前に見ていた別のサイトへ帰ってしまう。
	*/
	test('直接開いた画面では決まった行き先を出す', async ({ page }) => {
		await signup(page, { displayName: '直接開く人' });

		// 読み込み直しなので、辿ってきた記録は残らない。
		await page.goto('/prefectures/37');
		await expect(page.getByRole('link', { name: /へ戻る$/ })).toBeVisible();
		await expect(page.getByRole('button', { name: '前の画面へ戻る' })).toHaveCount(0);
	});
});

test.describe('戻る導線のある画面', () => {
	/*
	**「どの画面からでも一つ前へ戻れる」は、部品を置いた画面にしか効かない。**
	置き忘れた画面は、指摘されるまで誰も気づかない。ここで数え上げる。

	新着（/）とログイン・登録は、戻る先が無いか入口そのものなので対象外。

	**必ずリンクを辿って開く。** page.goto は読み込み直しになり、
	辿ってきた記録が消えるため、部品があっても「前の画面へ戻る」は出ない。
	*/
	const screens: { name: string; open: (page: Page, handle: string) => Promise<void> }[] = [
		{
			name: '設定・プロフィールの編集',
			open: async (page, handle) => {
				await page.goto(`/users/${handle}`);
				await page.getByRole('link', { name: 'プロフィールを編集' }).click();
				await expect(page).toHaveURL(/\/settings\/profile$/);
			}
		},
		{
			name: '設定・アカウント',
			open: async (page, handle) => {
				await page.goto(`/users/${handle}`);
				await page.getByRole('link', { name: 'プロフィールを編集' }).click();
				await page.getByRole('link', { name: 'パスワードの変更・退会はこちら' }).click();
				await expect(page).toHaveURL(/\/settings\/account$/);
			}
		},
		{
			name: 'フォロワー一覧',
			open: async (page, handle) => {
				await page.goto(`/users/${handle}`);
				// 「0人」という同じ文字のリンクが2つ並ぶため、行き先で選ぶ。
				await page.locator('a[href$="/followers"]').click();
				await expect(page).toHaveURL(/\/followers$/);
			}
		},
		{
			name: 'フォロー中一覧',
			open: async (page, handle) => {
				await page.goto(`/users/${handle}`);
				await page.locator('a[href$="/following"]').click();
				await expect(page).toHaveURL(/\/following$/);
			}
		}
	];

	for (const screen of screens) {
		test(`${screen.name}から一つ前へ戻れる`, async ({ page }) => {
			const me = await signup(page, { displayName: '導線を数える人' });
			await screen.open(page, me.handle);

			await expect(
				page.getByRole('button', { name: '前の画面へ戻る' }),
				`${screen.name} に戻る導線が無い`
			).toBeVisible();
		});
	}
});
