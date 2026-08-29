import http from 'k6/http';
import { check } from 'k6';
import { BASE_URL, credentials, type Credential } from './config.ts';

/*
負荷をかける操作。
**シナリオごとに書き写さない。** 3つのシナリオ（通常・限界・急増）は
かける負荷の形が違うだけで、叩く先は同じである。

---

**書き込みは「元に戻る操作」だけにしてある。**

いいねは押して外せば元に戻る。投稿とコメントは残るため、
試験のたびにデータが積み上がる。**積み上がった状態で測ると、
回を重ねるごとに遅くなり、原因が実装なのかデータ量なのか分からなくなる。**

投稿の作成を測っていないことは正直に書いておく。作成は画像の処理
（S3 のイベントで動く別プロセス）を伴い、**API の応答時間には
その待ち時間が含まれない。** ここで測っても意味のある数字にならない。
*/

/** 応答に名前を付けて記録する。どの操作が遅いかを分けて見るため。 */
type Named = { name: string };

function get(path: string, token: string, name: string) {
	return http.get(`${BASE_URL}/api${path}`, {
		headers: { Authorization: `Bearer ${token}` },
		tags: { name } satisfies Named
	});
}

/** ログインしてアクセストークンを得る。 */
export function login(user: Credential): string {
	const res = http.post(
		`${BASE_URL}/api/auth/login`,
		JSON.stringify({ email: user.email, password: user.password }),
		{ headers: { 'Content-Type': 'application/json' }, tags: { name: 'login' } }
	);
	if (res.status !== 200) {
		throw new Error(`ログインに失敗した (${res.status}): ${res.body}`);
	}
	return (res.json() as { data: { accessToken: string } }).data.accessToken;
}

/**
 * 全利用者ぶんのトークンを用意する。
 *
 * **setup() で1度だけ行い、結果を各 VU へ配る。** 各 VU が毎回
 * ログインすると、測りたい読み取りよりログインの方が多くなる。
 */
export function setupTokens(): { handle: string; token: string }[] {
	return credentials().map((user) => ({ handle: user.handle, token: login(user) }));
}

export type Session = { handle: string; token: string };

/**
 * 読み取り中心の一巡り。
 *
 * 想定している使われ方の順に並べてある。
 * ホーム → 気になる投稿を開く → 投稿者を見る → 検索する。
 */
export function browse(session: Session, handles: string[]) {
	const feed = get('/posts?limit=20', session.token, 'feed:latest');
	check(feed, { '新着フィードが取れる': (r) => r.status === 200 });

	check(get('/feed/following?limit=20', session.token, 'feed:following'), {
		'フォロー中フィードが取れる': (r) => r.status === 200
	});

	// フィードに出た投稿を1件開く。**固定の ID を叩かない。**
	// 同じ行ばかり読むとキャッシュに乗り、実態より速く見える。
	const posts = (feed.json() as { data: { posts: { id: number }[] } } | null)?.data?.posts ?? [];
	if (posts.length > 0) {
		const target = posts[Math.floor(Math.random() * posts.length)];
		check(get(`/posts/${target.id}`, session.token, 'post:detail'), {
			'投稿の詳細が取れる': (r) => r.status === 200
		});
		check(get(`/posts/${target.id}/comments?limit=20`, session.token, 'post:comments'), {
			'コメントが取れる': (r) => r.status === 200
		});
	}

	const other = handles[Math.floor(Math.random() * handles.length)];
	check(get(`/users/${other}`, session.token, 'user:profile'), {
		'プロフィールが取れる': (r) => r.status === 200
	});
	check(get(`/users/${other}/posts?limit=20`, session.token, 'user:posts'), {
		'利用者の投稿が取れる': (r) => r.status === 200
	});
	// **制覇マップは集計である。** 一覧より重くなりやすいので分けて測る。
	check(get(`/users/${other}/prefectures`, session.token, 'user:prefectures'), {
		'制覇マップが取れる': (r) => r.status === 200
	});
}

/**
 * 検索。**全文検索の索引（ngram）に当たる経路であり、一覧とは別に測る。**
 * 索引が肥大すると、ここだけが先に遅くなる（tech-stack.md）。
 */
export function search(session: Session) {
	const words = ['海鮮', '紅葉', '温泉', '朝市', '灯台'];
	const q = encodeURIComponent(words[Math.floor(Math.random() * words.length)]);

	check(get(`/search/posts?q=${q}&limit=20`, session.token, 'search:posts'), {
		'投稿の検索が返る': (r) => r.status === 200
	});
	check(get(`/search/posts?prefectureCode=13&limit=20`, session.token, 'search:byPrefecture'), {
		'都道府県での絞り込みが返る': (r) => r.status === 200
	});
	check(get(`/search/posts?sort=popular&limit=20`, session.token, 'search:popular'), {
		'人気順が返る': (r) => r.status === 200
	});
}

/** 通知。開くたびに引かれるうえ、未読数は画面のどこにいても出る。 */
export function notifications(session: Session) {
	check(get('/notifications?limit=20', session.token, 'notifications:list'), {
		'通知の一覧が取れる': (r) => r.status === 200
	});
	check(get('/notifications/unread-count', session.token, 'notifications:unread'), {
		'未読数が取れる': (r) => r.status === 200
	});
}

/**
 * いいねを押して外す。
 *
 * **書き込みでありながら元に戻る。** 試験のたびにデータが増えないので、
 * 何度回しても同じ条件で測れる。
 */
export function toggleLike(session: Session, postId: number) {
	const headers = { Authorization: `Bearer ${session.token}` };

	const liked = http.put(`${BASE_URL}/api/posts/${postId}/likes`, null, {
		headers,
		tags: { name: 'like:put' }
	});
	check(liked, { 'いいねできる': (r) => r.status === 204 });

	const unliked = http.del(`${BASE_URL}/api/posts/${postId}/likes`, null, {
		headers,
		tags: { name: 'like:delete' }
	});
	check(unliked, { 'いいねを外せる': (r) => r.status === 204 });
}

/** フィードから投稿 ID を1つ選ぶ。無ければ 0。 */
export function pickPostId(session: Session): number {
	const feed = get('/posts?limit=20', session.token, 'feed:latest');
	const posts = (feed.json() as { data: { posts: { id: number }[] } } | null)?.data?.posts ?? [];
	if (posts.length === 0) return 0;
	return posts[Math.floor(Math.random() * posts.length)].id;
}
