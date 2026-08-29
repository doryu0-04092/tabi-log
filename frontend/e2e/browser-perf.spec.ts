import { expect, test, type Page } from '@playwright/test';
import { createPostViaApi, signupViaApi } from './fixtures/app';

/*
画面側の性能（Core Web Vitals）。

**API の応答時間とは別の話である。** サーバーが 50ms で返しても、
画面の組み立てや画像の読み込みで待たされれば、利用者にとっては遅い。
負荷試験（perf/）が測るのはサーバー側、ここで測るのは**見え方**である。

見る指標は3つ。目安は Google が示している「良好」の線に揃えてある。

| 指標 | 意味 | 目安 |
|---|---|---|
| FCP | 最初に何かが描かれるまで | 1.8秒 |
| LCP | いちばん大きい要素が描かれるまで | 2.5秒 |
| CLS | 描画後に位置がずれた量 | 0.1 |

**CLS だけは性質が違う。** FCP と LCP は環境が遅ければ伸びるが、
CLS は**作りの問題**である。読み込み中に高さが確保されていない要素が
あると、後から入ってきた要素が下の内容を押し下げ、
押そうとしたものが別のものに変わる。速い機械でも起きる。

---

**この数字を本番の予測に使わない。** 測っているのはローカルの
preview サーバーであり、CloudFront も回線の遅延も入っていない。
比較に使えるのは「同じ環境で、変更の前後を比べる」場合である。
*/

type Vitals = { fcp: number; lcp: number; cls: number };

/**
 * 表示に関する指標を集める。
 *
 * **読み込みの前に観測を仕掛ける必要がある。** 描画が終わってから
 * 見に行っても、LCP と CLS の記録は取れない（`buffered: true` で
 * 遡れる範囲はあるが、確実ではない）。
 */
async function measure(page: Page, path: string): Promise<Vitals> {
	await page.addInitScript(() => {
		const w = window as unknown as { __vitals: { lcp: number; cls: number } };
		w.__vitals = { lcp: 0, cls: 0 };

		new PerformanceObserver((list) => {
			for (const entry of list.getEntries()) {
				w.__vitals.lcp = Math.max(w.__vitals.lcp, entry.startTime);
			}
		}).observe({ type: 'largest-contentful-paint', buffered: true });

		new PerformanceObserver((list) => {
			for (const entry of list.getEntries()) {
				const shift = entry as PerformanceEntry & { value: number; hadRecentInput: boolean };
				// **利用者の操作の直後のずれは数えない。**
				// 押した結果として変わるのは当たり前であり、
				// 数えると「使うほど悪い数字になる」ことになる。
				if (!shift.hadRecentInput) {
					w.__vitals.cls += shift.value;
				}
			}
		}).observe({ type: 'layout-shift', buffered: true });
	});

	await page.goto(path);
	// 主要な内容が出るまで待つ。出る前に測ると、
	// 「まだ何も無いので速い」という数字になる。
	await expect(page.locator('#main')).toBeVisible();

	// 遅れて入る画像のずれまで拾う。
	await page.waitForTimeout(1500);

	return page.evaluate(() => {
		const w = window as unknown as { __vitals: { lcp: number; cls: number } };
		const paint = performance.getEntriesByName('first-contentful-paint')[0];
		return {
			fcp: paint ? paint.startTime : 0,
			lcp: w.__vitals.lcp,
			cls: w.__vitals.cls
		};
	});
}

/** 目安。Google が「良好」としている線に合わせてある。 */
const GOOD = { fcp: 1800, lcp: 2500, cls: 0.1 };

function report(name: string, vitals: Vitals) {
	console.log(
		`${name}: FCP=${vitals.fcp.toFixed(0)}ms LCP=${vitals.lcp.toFixed(0)}ms CLS=${vitals.cls.toFixed(3)}`
	);
}

test.describe('画面の表示性能', () => {
	test('ホーム（投稿の一覧）が目安に収まる', async ({ page }) => {
		const { token } = await signupViaApi(page.request, { displayName: '測る人' });
		// 一覧に画像が並ぶ状態で測る。**空の一覧では意味が無い。**
		// 画像の高さが確保されていなければ、ここで CLS に出る。
		for (let i = 0; i < 3; i++) {
			await createPostViaApi(page.request, token, { body: `表示性能の確認 ${i} ${Date.now()}` });
		}

		const vitals = await measure(page, '/');
		report('ホーム', vitals);

		expect(vitals.fcp).toBeLessThan(GOOD.fcp);
		expect(vitals.lcp).toBeLessThan(GOOD.lcp);
		expect(vitals.cls).toBeLessThan(GOOD.cls);
	});

	test('投稿の詳細が目安に収まる', async ({ page }) => {
		const { token } = await signupViaApi(page.request);
		const postId = await createPostViaApi(page.request, token, {
			body: `詳細の表示性能 ${Date.now()}`
		});

		const vitals = await measure(page, `/posts/${postId}`);
		report('投稿の詳細', vitals);

		expect(vitals.fcp).toBeLessThan(GOOD.fcp);
		expect(vitals.lcp).toBeLessThan(GOOD.lcp);
		expect(vitals.cls).toBeLessThan(GOOD.cls);
	});

	/*
	制覇マップは47個のマスを一度に描く。**一覧とは描画の重さが違う**ため
	別に測る。ここが重ければ、マスの描き方（SVG か CSS グリッドか）が
	そのまま効いてくる。
	*/
	test('プロフィール（制覇マップを含む）が目安に収まる', async ({ page }) => {
		const { user, token } = await signupViaApi(page.request, { displayName: '地図を見る人' });
		await createPostViaApi(page.request, token, { body: `地図の表示性能 ${Date.now()}` });

		const vitals = await measure(page, `/users/${user.handle}`);
		report('プロフィール', vitals);

		expect(vitals.fcp).toBeLessThan(GOOD.fcp);
		expect(vitals.lcp).toBeLessThan(GOOD.lcp);
		expect(vitals.cls).toBeLessThan(GOOD.cls);
	});

	/*
	**追加読み込みで位置がずれないこと。**

	無限スクロールは、下に足すぶんには読んでいる位置を動かさない。
	上に足す作り（新着の自動差し込み）にすると必ずずれるため、
	新着は帯で知らせるだけにしてある。その判断が効いているかを、
	数字で確かめる。
	*/
	test('続きを読み込んでも読んでいる位置がずれない', async ({ page }) => {
		const { token } = await signupViaApi(page.request);
		for (let i = 0; i < 3; i++) {
			await createPostViaApi(page.request, token, { body: `ずれの確認 ${i} ${Date.now()}` });
		}

		await measure(page, '/');

		// ここまでのずれを捨てて、操作したあとの分だけを見る。
		await page.evaluate(() => {
			const w = window as unknown as { __vitals: { cls: number } };
			w.__vitals.cls = 0;
		});

		await page.mouse.wheel(0, 100_000);
		await page.waitForTimeout(2000);

		const after = await page.evaluate(() => {
			const w = window as unknown as { __vitals: { cls: number } };
			return w.__vitals.cls;
		});
		console.log(`追加読み込み後の CLS: ${after.toFixed(3)}`);

		expect(after).toBeLessThan(GOOD.cls);
	});
});
