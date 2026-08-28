import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

/** ヘッダーのアカウント領域。同じ文字列が本文にも出るため領域で絞る。 */
function nav(page: Page) {
	return page.getByRole('navigation', { name: 'アカウント' });
}

/** テストごとに衝突しない識別子を作る。 */
function unique() {
	const n = `${Date.now()}${Math.floor(Math.random() * 1000)}`;
	return {
		email: `e2e${n}@example.test`,
		handle: `e2e_${n}`.slice(0, 30),
		password: 'password12345'
	};
}

async function signup(page: Page, user: ReturnType<typeof unique>, displayName = 'たびびと') {
	await page.goto('/signup');
	await page.getByLabel('メールアドレス').fill(user.email);
	await page.getByLabel('ハンドル').fill(user.handle);
	await page.getByLabel('表示名').fill(displayName);
	await page.getByLabel('パスワード').fill(user.password);
	await page.getByRole('button', { name: '登録する' }).click();
}

test.describe('認証', () => {
	test('登録するとログイン状態になる', async ({ page }) => {
		const user = unique();
		await signup(page, user, 'たびびと太郎');

		await expect(page).toHaveURL('/');
		await expect(nav(page).getByText('たびびと太郎')).toBeVisible();
		await expect(page.getByRole('button', { name: 'ログアウト' })).toBeVisible();
	});

	// アクセストークンはメモリにしか無いためリロードで消える。
	// Cookie のリフレッシュトークンから復元できること。
	test('リロードしてもログイン状態が保たれる', async ({ page }) => {
		const user = unique();
		await signup(page, user, 'リロード確認');
		await expect(nav(page).getByText('リロード確認')).toBeVisible();

		await page.reload();

		await expect(nav(page).getByText('リロード確認')).toBeVisible();
		await expect(page.getByRole('button', { name: 'ログアウト' })).toBeVisible();
	});

	// **アクセストークンが localStorage に保存されていないこと。**
	// 保存すると、XSS が1つでもあれば持ち出される。
	test('アクセストークンを localStorage に保存しない', async ({ page }) => {
		const user = unique();
		await signup(page, user);
		await expect(page.getByRole('button', { name: 'ログアウト' })).toBeVisible();

		const stored = await page.evaluate(() => JSON.stringify(window.localStorage));
		expect(stored).toBe('{}');
	});

	// リフレッシュトークンは HttpOnly のため JavaScript から読めないこと。
	test('リフレッシュトークンを JavaScript から読めない', async ({ page, context }) => {
		const user = unique();
		await signup(page, user);
		await expect(page.getByRole('button', { name: 'ログアウト' })).toBeVisible();

		const cookies = await context.cookies();
		const refresh = cookies.find((c) => c.name === 'tabilog_refresh');
		expect(refresh, 'リフレッシュトークンの Cookie が無い').toBeDefined();
		expect(refresh?.httpOnly).toBe(true);
		expect(refresh?.sameSite).toBe('Strict');
		expect(refresh?.path).toBe('/api/auth');

		// document.cookie には現れない。
		const visible = await page.evaluate(() => document.cookie);
		expect(visible).not.toContain('tabilog_refresh');
	});

	test('ログアウトすると未ログインに戻る', async ({ page }) => {
		const user = unique();
		await signup(page, user);
		await expect(page.getByRole('button', { name: 'ログアウト' })).toBeVisible();

		await page.getByRole('button', { name: 'ログアウト' }).click();

		await expect(page).toHaveURL('/login');
		// ログアウト後にリロードしても復元されないこと。
		await page.goto('/');
		await expect(page.getByRole('link', { name: 'ログイン' }).first()).toBeVisible();
	});

	test('登録したアカウントでログインできる', async ({ page }) => {
		const user = unique();
		await signup(page, user, 'ログイン確認');
		await page.getByRole('button', { name: 'ログアウト' }).click();
		await expect(page).toHaveURL('/login');

		await page.getByLabel('メールアドレス').fill(user.email);
		await page.getByLabel('パスワード').fill(user.password);
		await page.getByRole('button', { name: 'ログイン' }).click();

		await expect(page).toHaveURL('/');
		await expect(nav(page).getByText('ログイン確認')).toBeVisible();
	});

	test('誤ったパスワードではログインできず、理由を明かさない', async ({ page }) => {
		const user = unique();
		await signup(page, user);
		await page.getByRole('button', { name: 'ログアウト' }).click();

		await page.getByLabel('メールアドレス').fill(user.email);
		await page.getByLabel('パスワード').fill('wrong-password-here');
		await page.getByRole('button', { name: 'ログイン' }).click();

		const alert = page.getByRole('alert');
		await expect(alert).toBeVisible();
		// 「パスワードが違う」と断定しない文言であること。
		await expect(alert).toContainText('メールアドレスまたはパスワード');
		await expect(page).toHaveURL('/login');
	});

	test('同じメールアドレスでは登録できない', async ({ page }) => {
		const user = unique();
		await signup(page, user);
		await page.getByRole('button', { name: 'ログアウト' }).click();

		// 同じアドレス、別のハンドルで登録を試みる。
		await page.goto('/signup');
		await page.getByLabel('メールアドレス').fill(user.email);
		await page.getByLabel('ハンドル').fill(`${user.handle}x`.slice(0, 30));
		await page.getByLabel('表示名').fill('重複確認');
		await page.getByLabel('パスワード').fill(user.password);
		await page.getByRole('button', { name: '登録する' }).click();

		await expect(page.getByRole('alert')).toContainText('既に使われています');
	});

	test('ログイン画面にアクセシビリティ違反が無い', async ({ page }) => {
		await page.goto('/login');
		await expect(page.getByRole('heading', { name: 'ログイン', level: 1 })).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});

	test('新規登録画面にアクセシビリティ違反が無い', async ({ page }) => {
		await page.goto('/signup');
		await expect(page.getByRole('heading', { name: '新規登録', level: 1 })).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
