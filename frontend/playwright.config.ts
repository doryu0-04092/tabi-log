import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
	testDir: 'e2e',
	// manual/ は見た目の確認用で合否を判定しないため、通常の実行から外す。
	// 撮り直したいときは MANUAL=1 を付けて実行する。
	testIgnore: process.env.MANUAL ? [] : ['**/manual/**'],
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 1 : 0,
	reporter: process.env.CI ? 'list' : 'html',
	use: {
		baseURL: 'http://localhost:4173',
		trace: 'on-first-retry'
	},
	projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],

	// dev サーバーではなくビルド成果物を preview で配信して検証する。
	// 実際に配信されるものを対象にするためである
	// （ビルドは通るが成果物が壊れている、という状態を検出できる）。
	webServer: {
		command: 'npm run build && npm run preview -- --port 4173',
		port: 4173,
		reuseExistingServer: !process.env.CI,
		timeout: 120_000
	}
});
