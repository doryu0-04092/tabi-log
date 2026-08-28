// 画面の見た目を目で確認するための撮影。
//
// 通常の E2E とは目的が違う（合否を判定しない）ため、
// 既定の実行からは外し、必要なときだけ --grep で呼ぶ。
import { test } from '@playwright/test';
import { PNG } from '../fixtures/png';

/**
 * 画像が実際に描画されるまで待つ。
 *
 * **待たずに撮ると、読み込み前の空箱が写る。** 一度これで
 * 「画像が表示されない不具合」だと誤認した。
 */
async function waitForImages(page: import('@playwright/test').Page) {
	// **画像は loading="lazy" のため、画面に入るまで読み込まれない。**
	// fullPage で撮ると画面外も写るので、先に最後まで送って読み込ませる。
	await page.evaluate(async () => {
		const step = window.innerHeight;
		for (let y = 0; y < document.body.scrollHeight; y += step) {
			window.scrollTo(0, y);
			await new Promise((r) => setTimeout(r, 120));
		}
		window.scrollTo(0, 0);
	});

	await page.waitForFunction(
		() => {
			const imgs = [...document.querySelectorAll<HTMLImageElement>('.photo-list img')];
			return imgs.length > 0 && imgs.every((i) => i.complete && i.naturalWidth > 0);
		},
		undefined,
		{ timeout: 30_000 }
	);
}

test('画面を撮影する', async ({ page }) => {
	test.setTimeout(180_000);
	await page.setViewportSize({ width: 480, height: 900 });

	const n = Date.now();
	await page.goto('/signup');
	await page.getByLabel('メールアドレス').fill(`shot${n}@example.test`);
	await page.getByLabel('ハンドル').fill(`shot_${n}`.slice(0, 30));
	await page.getByLabel('表示名').fill('たびびと');
	await page.getByLabel('パスワード').fill('password12345');
	await page.getByRole('button', { name: '登録する' }).click();
	await page.getByRole('button', { name: 'ログアウト' }).waitFor();

	const posts: [string, string, string][] = [
		[
			'北海道',
			'函館の朝市で海鮮丼を食べた。イカが驚くほど新鮮で、朝から並んだ甲斐があった。',
			'器に盛られた海鮮丼'
		],
		[
			'沖縄県',
			'美ら海水族館でジンベエザメを見た。想像より遥かに大きかった。',
			'水槽を泳ぐジンベエザメ'
		]
	];

	for (const [pref, body, alt] of posts) {
		await page.goto('/posts/new');
		await page.setInputFiles('#photos', { name: 'p.png', mimeType: 'image/png', buffer: PNG });
		await page.getByText('使えます').waitFor({ timeout: 60_000 });
		await page.getByLabel('画像1の説明（必須）').fill(alt);
		await page.getByLabel('都道府県（必須）').selectOption({ label: pref });
		await page.getByLabel('訪問日（必須）').fill('2026-05-03');
		await page.getByLabel('本文（必須）').fill(body);
		await page.getByLabel('タグ').fill('グルメ 海鮮');
		await page.getByRole('button', { name: '投稿する' }).click();
		await page.waitForURL(/\/posts\/\d+$/);
	}

	await waitForImages(page);
	await page.screenshot({ path: 'screenshots/detail.png', fullPage: true });

	await page.goto('/');
	await page.getByRole('heading', { name: '新着' }).waitFor();
	await waitForImages(page);
	await page.screenshot({ path: 'screenshots/feed.png', fullPage: true });

	await page.goto('/posts/new');
	await page.getByRole('heading', { name: '投稿する' }).waitFor();
	await page.waitForTimeout(600);
	await page.screenshot({ path: 'screenshots/new.png', fullPage: true });
});
