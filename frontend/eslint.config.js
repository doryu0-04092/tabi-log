import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import globals from 'globals';
import ts from 'typescript-eslint';

export default ts.config(
	js.configs.recommended,
	...ts.configs.recommended,
	...svelte.configs.recommended,
	{
		languageOptions: {
			globals: { ...globals.browser, ...globals.node }
		}
	},
	{
		files: ['**/*.svelte', '**/*.svelte.ts', '**/*.svelte.js'],
		languageOptions: {
			parserOptions: { parser: ts.parser }
		}
	},
	{
		ignores: ['build/', '.svelte-kit/', 'node_modules/', 'src/lib/api/gen.ts']
	}
);

// アクセシビリティの検査は eslint ではなく `npm run check`（svelte-check）が担う。
//
// Svelte のコンパイラは a11y の問題を警告として出す。svelte-check に
// --threshold warning を渡すことで、その警告が CI を落とす。
// 検査を eslint 側にも二重化すると、どちらを直せばよいかが曖昧になるため
// 責任を1か所に寄せている。
