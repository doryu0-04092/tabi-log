import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
// vitest の設定（test キー）も同じファイルに書くため、
// defineConfig は 'vite' ではなく 'vitest/config' から取る。
import { defineConfig } from 'vitest/config';

export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				// プロジェクト全体を runes モードで統一する（node_modules は除く）。
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},

			// adapter-static + fallback で SPA として書き出す。
			//
			// 配信を CloudFront + S3 の静的配信に保ち、Go の API を唯一のサーバーに
			// するための選択である。代償として SSR を行わないため、
			// SNS で共有したときの OGP カードは出ない（docs/tech-stack.md 参照）。
			//
			// fallback を index.html にすることで、/posts/123 のようなディープリンクも
			// クライアント側のルーターが処理できる。
			adapter: adapter({
				pages: 'build',
				assets: 'build',
				fallback: 'index.html',
				precompress: false,
				strict: true
			})
		})
	],

	// ローカルでは Go の API へ中継し、ブラウザから見て同一オリジンにする。
	// 本番の CloudFront も /api を ALB に振り分けるため、
	// 開発と本番でフロントエンドのコードが変わらない。
	//
	// dev と preview で設定が別なので両方に置く。E2E はビルド成果物を
	// preview で配信して検証するため、preview 側にも必要になる。
	server: {
		port: 5173,
		proxy: { '/api': { target: 'http://localhost:8080', changeOrigin: false } }
	},

	preview: {
		proxy: { '/api': { target: 'http://localhost:8080', changeOrigin: false } }
	},

	test: {
		environment: 'jsdom',
		include: ['src/**/*.{test,spec}.{js,ts}']
	}
});
