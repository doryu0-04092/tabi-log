import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import LoadMore from './LoadMore.svelte';

/*
自動読み込みの配線を確かめる。

**E2E では「押さずに増えた」ことしか分からない。** 押していないのに
増えた理由が、番兵の交差なのか別の経路なのかは区別できない。
ここでは交差の通知だけを起こして、その1つが読み込みにつながることを見る。

jsdom には Intersection Observer が無いので、**観測の入口を差し替える。**
差し替えるのは「ブラウザが交差を教えてくる」部分だけで、
交差を受け取ったあとの判断（続きがあるか・読み込み中か）は本物を通す。
*/

type Trigger = (isIntersecting: boolean) => void;

let triggers: Trigger[] = [];
let disconnected = 0;

class FakeIntersectionObserver {
	constructor(private callback: IntersectionObserverCallback) {}

	observe() {
		triggers.push((isIntersecting) => {
			this.callback(
				[{ isIntersecting } as IntersectionObserverEntry],
				this as unknown as IntersectionObserver
			);
		});
	}

	disconnect() {
		disconnected++;
	}

	unobserve() {}
	takeRecords(): IntersectionObserverEntry[] {
		return [];
	}
}

type Props = {
	hasMore: boolean;
	loading: boolean;
	onLoadMore: () => void;
	label?: string;
	auto?: boolean;
};

function render(props: Props) {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(LoadMore, { target, props });
	flushSync();
	return { target, component };
}

describe('LoadMore', () => {
	beforeEach(() => {
		triggers = [];
		disconnected = 0;
		vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		document.body.innerHTML = '';
	});

	it('番兵が画面に入ると、押さなくても続きを読む', () => {
		const onLoadMore = vi.fn();
		const { component } = render({ hasMore: true, loading: false, onLoadMore });

		expect(triggers).toHaveLength(1);
		triggers[0](true);

		expect(onLoadMore).toHaveBeenCalledTimes(1);
		void unmount(component);
	});

	// **画面に入っただけでは読まない。** 交差の通知は要素が外へ出るときにも来る。
	it('番兵が画面から出たときには読まない', () => {
		const onLoadMore = vi.fn();
		const { component } = render({ hasMore: true, loading: false, onLoadMore });

		triggers[0](false);

		expect(onLoadMore).not.toHaveBeenCalled();
		void unmount(component);
	});

	// 読み込み中に重ねて呼ぶと、同じページを二重に取り込む。
	it('読み込み中は重ねて読まない', () => {
		const onLoadMore = vi.fn();
		const { component } = render({ hasMore: true, loading: true, onLoadMore });

		triggers[0](true);

		expect(onLoadMore).not.toHaveBeenCalled();
		void unmount(component);
	});

	// 続きが無ければ何も描かない。ボタンも番兵も出ない。
	it('続きが無ければ観測もボタンも作らない', () => {
		const onLoadMore = vi.fn();
		const { target, component } = render({ hasMore: false, loading: false, onLoadMore });

		expect(triggers).toHaveLength(0);
		expect(target.querySelector('button')).toBeNull();
		void unmount(component);
	});

	// **自動読み込みを切れること。** 古い方へ遡る一覧では、
	// 勝手に伸びると読んでいた位置が押し下げられる。
	it('auto を切ると観測しないが、ボタンは残る', () => {
		const onLoadMore = vi.fn();
		const { target, component } = render({
			hasMore: true,
			loading: false,
			onLoadMore,
			auto: false
		});

		expect(triggers).toHaveLength(0);
		expect(target.querySelector('button')).not.toBeNull();
		void unmount(component);
	});

	// キーボードだけで操作する人は自動読み込みに頼れない。
	it('ボタンを押しても続きを読む', () => {
		const onLoadMore = vi.fn();
		const { target, component } = render({ hasMore: true, loading: false, onLoadMore });

		target.querySelector('button')?.click();

		expect(onLoadMore).toHaveBeenCalledTimes(1);
		void unmount(component);
	});

	it('外すときに観測をやめる', () => {
		const { component } = render({ hasMore: true, loading: false, onLoadMore: vi.fn() });
		void unmount(component);
		flushSync();

		expect(disconnected).toBe(1);
	});
});
