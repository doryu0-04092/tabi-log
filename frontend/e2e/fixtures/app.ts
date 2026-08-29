// E2E で繰り返し使う操作をまとめる。
//
// 「登録する」「投稿を1件作る」は3つのテストファイルで必要になったため
// ここへ切り出した。各ファイルに写しを置くと、画面の文言を変えたときに
// 直し忘れた側だけが落ちる。

import { expect, type Page } from '@playwright/test';
import { PNG } from './png';

export type TestUser = { email: string; handle: string; password: string };

/**
 * テストごとに衝突しない識別子を作る。
 *
 * prefix はどのテストが作った利用者かをデータベース上で見分けるためのもの。
 */
export function unique(prefix = 'e2e'): TestUser {
	const n = `${Date.now()}${Math.floor(Math.random() * 1000)}`;
	return {
		email: `${prefix}${n}@example.test`,
		handle: `${prefix}_${n}`.slice(0, 30),
		password: 'password12345'
	};
}

/**
 * 新規登録してログイン状態にする。
 *
 * **表示名で本人確認まで行う。** 「ログアウト」ボタンが見えるだけだと、
 * 既存のセッションが復元されただけの状態と区別できない。
 */
export async function signup(
	page: Page,
	opts: { user?: TestUser; displayName?: string } = {}
): Promise<TestUser> {
	const user = opts.user ?? unique();
	const displayName = opts.displayName ?? 'たびびと';

	await page.goto('/signup');
	await page.getByLabel('メールアドレス').fill(user.email);
	await page.getByLabel('ハンドル').fill(user.handle);
	await page.getByLabel('表示名').fill(displayName);
	await page.getByLabel('パスワード').fill(user.password);
	await page.getByRole('button', { name: '登録する' }).click();

	await expect(
		page.getByRole('navigation', { name: 'アカウント' }).getByText(displayName)
	).toBeVisible();

	return user;
}

/** 投稿を1件作る。作成後の投稿詳細ページに遷移した状態で返る。 */
export async function createPost(
	page: Page,
	opts: { body: string; prefecture: string }
): Promise<void> {
	await page.goto('/posts/new');

	// 画像を選ぶ。実ファイルを置かずにメモリ上のバイト列を渡す。
	await page.setInputFiles('#photos', {
		name: 'photo.png',
		mimeType: 'image/png',
		buffer: PNG
	});

	// **送信が終わっても、まだ投稿には使えない。**
	// 形式の検証と EXIF の除去がサーバー側で走り、
	// 完了して初めて「使えます」になる。
	await expect(page.getByText('使えます')).toBeVisible({ timeout: 30_000 });

	await page.getByLabel('都道府県（必須）').selectOption({ label: opts.prefecture });
	await page.getByLabel('訪問日').fill('2026-05-03');
	await page.getByLabel('本文（必須）').fill(opts.body);
	await page.getByRole('button', { name: '投稿する' }).click();

	await expect(page).toHaveURL(/\/posts\/\d+$/);
}

/**
 * フォローを切り替え、**サーバーが受け付けるまで待つ。**
 *
 * ボタンは応答を待たずに見た目を変える（押してから数百ミリ秒あとに
 * 変わると二度押しされるため）。つまり `aria-pressed` が変わったことは
 * **サーバーに届いた証拠にならない。** 待たずに画面を移ると、
 * 送信中のリクエストが打ち切られることがある。
 */
export async function toggleFollow(page: Page, displayName: string, want: boolean): Promise<void> {
	const button = page.getByRole('button', { name: new RegExp(`^${displayName}を`) });
	const accepted = page.waitForResponse(
		(r) =>
			r.url().includes('/follow') &&
			r.request().method() === (want ? 'PUT' : 'DELETE') &&
			r.status() === 204
	);
	await button.click();
	await accepted;
	await expect(button).toHaveAttribute('aria-pressed', String(want));
}

/** いいねを切り替え、**サーバーが受け付けるまで待つ。** 理由は toggleFollow と同じ。 */
export async function toggleLike(page: Page, want: boolean): Promise<void> {
	const button = page.getByRole('button', { name: /^いいね/ });
	const accepted = page.waitForResponse(
		(r) =>
			r.url().includes('/likes') &&
			r.request().method() === (want ? 'PUT' : 'DELETE') &&
			r.status() === 204
	);
	await button.click();
	await accepted;
	await expect(button).toHaveAttribute('aria-pressed', String(want));
}
