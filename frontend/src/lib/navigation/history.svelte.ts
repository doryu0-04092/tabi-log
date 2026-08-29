// アプリの中を何回移動したかを覚えておく。
//
// **「一つ前の画面へ戻る」を出してよいかの判断に使う。**
// 直接リンクで開いた画面（履歴が無い）で「戻る」を出すと、
// 押したときにアプリの外へ出てしまう。
//
// SvelteKit は「戻れるか」を直接は教えてくれないため、
// 移動の種類を数えて自前で持つ。

let depth = $state(0);

export const navHistory = {
	/** アプリの中で1回以上移動していれば、戻り先はアプリの中にある。 */
	get canGoBack(): boolean {
		return depth > 0;
	}
};

/**
 * 移動を記録する。`afterNavigate` から呼ぶ。
 *
 * - `enter`（最初の読み込み）は数えない。戻り先が無いためである
 * - `popstate`（戻る操作）は減らす。減らさないと、入口まで戻っても
 *   「戻る」が出続け、押すとアプリの外へ出る
 */
export function recordNavigation(type: string): void {
	if (type === 'enter') return;
	if (type === 'popstate') {
		depth = Math.max(0, depth - 1);
		return;
	}
	depth += 1;
}
