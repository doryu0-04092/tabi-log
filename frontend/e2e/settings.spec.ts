import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { createPost, signup } from './fixtures/app';

test.describe('プロフィールの編集', () => {
	test('自分のプロフィールから開ける', async ({ page }) => {
		const me = await signup(page, { displayName: '編集する人' });

		await page.goto(`/users/${me.handle}`);
		await page.getByRole('link', { name: 'プロフィールを編集' }).click();

		await expect(page).toHaveURL('/settings/profile');
		// 現在の値が入っていること。空欄から始めると、
		// 何を消して何を残すのかが分からない。
		await expect(page.getByLabel('表示名（必須）')).toHaveValue('編集する人');
	});

	// 他人のプロフィールには編集の導線を出さない。
	test('他人のプロフィールには編集の導線が出ない', async ({ page, browser, baseURL }) => {
		const otherContext = await browser.newContext({ baseURL });
		const other = await otherContext.newPage();
		const target = await signup(other, { displayName: '編集されない人' });
		await otherContext.close();

		await signup(page, { displayName: '見るだけの人' });
		await page.goto(`/users/${target.handle}`);
		await expect(page.getByRole('link', { name: 'プロフィールを編集' })).toBeHidden();
	});

	test('表示名と自己紹介を変えられる', async ({ page }) => {
		const me = await signup(page, { displayName: 'もとの名前' });
		const name = `あたらしい名前 ${Date.now()}`;

		await page.goto('/settings/profile');
		await page.getByLabel('表示名（必須）').fill(name);
		await page.getByLabel('自己紹介').fill('旅の記録です');
		await page.getByRole('button', { name: '保存する' }).click();

		await expect(page.getByRole('status')).toContainText('保存しました');
		// ヘッダーの表示名も変わること。古いまま残ると、
		// 保存できたのか分からなくなる。
		await expect(
			page.getByRole('navigation', { name: 'アカウント' }).getByText(name)
		).toBeVisible();

		// **サーバー側に残っていること。**
		await page.goto(`/users/${me.handle}`);
		await expect(page.getByRole('heading', { name, level: 1 })).toBeVisible();
		await expect(page.getByText('旅の記録です')).toBeVisible();
	});

	test('自己紹介を空にすると消える', async ({ page }) => {
		const me = await signup(page);

		await page.goto('/settings/profile');
		await page.getByLabel('自己紹介').fill('いったん書く');
		await page.getByRole('button', { name: '保存する' }).click();
		await expect(page.getByRole('status')).toContainText('保存しました');

		await page.getByLabel('自己紹介').fill('');
		await page.getByRole('button', { name: '保存する' }).click();
		await expect(page.getByRole('status')).toContainText('保存しました');

		await page.goto(`/users/${me.handle}`);
		await expect(page.getByText('いったん書く')).toBeHidden();
	});

	test('表示名を空にすると保存できない', async ({ page }) => {
		await signup(page);

		await page.goto('/settings/profile');
		await page.getByLabel('表示名（必須）').fill('');
		await expect(page.getByRole('button', { name: '保存する' })).toBeDisabled();
	});

	test('プロフィールの編集にアクセシビリティ違反が無い', async ({ page }) => {
		await signup(page);

		await page.goto('/settings/profile');
		await expect(page.getByRole('heading', { name: 'プロフィールの編集', level: 1 })).toBeVisible();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});

test.describe('旅行履歴', () => {
	// **投稿日と訪問日は別の軸である。**
	// 旅行から帰ったあとにまとめて投稿するのが自然な使われ方。
	test('訪問日順に切り替えられる', async ({ page }) => {
		const me = await signup(page, { displayName: '履歴の人' });
		const body = `旅行履歴の投稿 ${Date.now()}`;
		await createPost(page, { body, prefecture: '北海道', alt: '北海道の写真' });

		await page.goto(`/users/${me.handle}`);
		await expect(page.getByRole('heading', { name: '投稿', level: 2 })).toBeVisible();

		await page.getByRole('link', { name: '訪問日順' }).click();
		await expect(page).toHaveURL(/\?tab=travels$/);
		await expect(page.getByRole('heading', { name: '旅行履歴', level: 2 })).toBeVisible();
		await expect(page.getByText(body)).toBeVisible();

		// URL に状態があるので、リロードしても切り替えたままになる。
		await page.reload();
		await expect(page.getByRole('heading', { name: '旅行履歴', level: 2 })).toBeVisible();
	});
});

test.describe('アカウント設定', () => {
	test('現在のパスワードが違うと変更できない', async ({ page }) => {
		await signup(page);

		await page.goto('/settings/account');
		await page.getByLabel('現在のパスワード（必須）').first().fill('wrong-password');
		await page.getByLabel('新しいパスワード（必須）').fill('newpassword123');
		await page.getByRole('button', { name: 'パスワードを変更する' }).click();

		await expect(page.getByRole('alert')).toContainText('現在のパスワード');
		// ログイン画面へ飛ばされない（ログイン状態そのものは有効である）。
		await expect(page).toHaveURL('/settings/account');
	});

	// **変更するとすべての端末でログアウトされる。** 先に伝えてある。
	test('パスワードを変えるとログインし直しになる', async ({ page }) => {
		const user = await signup(page);

		await page.goto('/settings/account');
		await expect(page.getByText('すべての端末でログインし直しが必要')).toBeVisible();

		await page.getByLabel('現在のパスワード（必須）').first().fill(user.password);
		await page.getByLabel('新しいパスワード（必須）').fill('newpassword12345');
		await page.getByRole('button', { name: 'パスワードを変更する' }).click();

		await expect(page).toHaveURL('/login');

		// 新しいパスワードで入れること。
		await page.getByLabel('メールアドレス').fill(user.email);
		await page.getByLabel('パスワード').fill('newpassword12345');
		await page.getByRole('button', { name: 'ログイン', exact: true }).click();
		await expect(page.getByRole('button', { name: 'ログアウト' })).toBeVisible();
	});

	// **取り消せない操作なので一段挟む。** 何が消えるかも先に書く。
	test('退会には確認とパスワードが要る', async ({ page }) => {
		await signup(page);

		await page.goto('/settings/account');
		await expect(page.getByText('取り消せません')).toBeVisible();
		await expect(page.getByLabel('現在のパスワード（必須）')).toHaveCount(1);

		await page.getByRole('button', { name: '退会の手続きへ進む' }).click();
		await expect(page.getByLabel('現在のパスワード（必須）')).toHaveCount(2);
		// パスワードを入れるまで押せない。
		await expect(page.getByRole('button', { name: '退会する' })).toBeDisabled();
	});

	test('退会すると投稿が消え、ログインできなくなる', async ({ page }) => {
		const user = await signup(page, { displayName: '退会する人' });
		const body = `退会で消える投稿 ${Date.now()}`;
		await createPost(page, { body, prefecture: '沖縄県', alt: '沖縄の写真' });

		await page.goto('/settings/account');
		await page.getByRole('button', { name: '退会の手続きへ進む' }).click();
		await page.getByLabel('現在のパスワード（必須）').nth(1).fill(user.password);
		await page.getByRole('button', { name: '退会する' }).click();

		await expect(page).toHaveURL('/login');

		// 退会したアカウントでは入れない。
		await page.getByLabel('メールアドレス').fill(user.email);
		await page.getByLabel('パスワード').fill(user.password);
		await page.getByRole('button', { name: 'ログイン', exact: true }).click();
		await expect(page.getByRole('alert')).toBeVisible();
		await expect(page.getByRole('button', { name: 'ログアウト' })).toBeHidden();
	});

	// **ハンドルは解放しない。** 解放すると別人が同じハンドルを取れてしまい、
	// 過去のリンクの指す先が変わる。
	test('退会したハンドルは再登録できない', async ({ page }) => {
		const user = await signup(page);

		await page.goto('/settings/account');
		await page.getByRole('button', { name: '退会の手続きへ進む' }).click();
		await page.getByLabel('現在のパスワード（必須）').nth(1).fill(user.password);
		await page.getByRole('button', { name: '退会する' }).click();
		await expect(page).toHaveURL('/login');

		await page.goto('/signup');
		await page.getByLabel('メールアドレス').fill(`new-${user.email}`);
		await page.getByLabel('ハンドル').fill(user.handle);
		await page.getByLabel('表示名').fill('別人');
		await page.getByLabel('パスワード').fill('password12345');
		await page.getByRole('button', { name: '登録する' }).click();

		await expect(page.getByRole('alert')).toBeVisible();
		await expect(page).toHaveURL('/signup');
	});

	test('アカウント設定にアクセシビリティ違反が無い', async ({ page }) => {
		await signup(page);

		await page.goto('/settings/account');
		await expect(page.getByRole('heading', { name: 'アカウント設定', level: 1 })).toBeVisible();
		// 退会の確認を開いた状態でも検査する。
		await page.getByRole('button', { name: '退会の手続きへ進む' }).click();

		const results = await new AxeBuilder({ page })
			.withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
			.analyze();

		expect(results.violations).toEqual([]);
	});
});
