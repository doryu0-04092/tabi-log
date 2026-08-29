#!/usr/bin/env node
/*
 * 負荷試験の実行係。
 *
 *   種データを入れる → k6 を回す → **必ず後片付けする**
 *
 * ---
 *
 * **後片付けを自動にしているのが、このファイルの主な理由である。**
 *
 * 「クリーンアップの仕組みは用意したが、自動では呼ばれていない」という
 * 状態になりやすい。用意しただけでは、実行した人が手で消すことになり、
 * 消し忘れた分だけ次の測定が汚れる。**測るたびにデータが積み上がると、
 * 回を重ねるごとに遅くなり、原因が実装なのかデータ量なのか分からなくなる。**
 *
 * **失敗しても片付ける。ただし結果は残す。** k6 が閾値割れで
 * 0 以外を返しても、レポートを書き出したうえで片付けてから、
 * 同じ終了コードで終わる。
 *
 * ---
 *
 * 使い方は README.md を参照。
 */

import { spawnSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, '..');

const BASE_URL = process.env.PERF_BASE_URL || 'http://localhost:8080';
const PREFIX = 'perf_';
const PASSWORD = 'perf_password_12345';

/** 用意する利用者の数。少なすぎると同じ行ばかり読むことになる。 */
const USERS = Number(process.env.PERF_USERS_COUNT || 10);
/** 1人あたりの投稿数。 */
const POSTS_PER_USER = Number(process.env.PERF_POSTS_PER_USER || 200);

const scenario = process.argv[2] || 'smoke';
const allowed = ['smoke', 'load', 'stress', 'spike'];
if (!allowed.includes(scenario)) {
	console.error(`シナリオは ${allowed.join(' / ')} のいずれか。指定された値: ${scenario}`);
	process.exit(2);
}

/** docker compose 経由で mysql に SQL を流す。 */
function mysql(sql) {
	const result = spawnSync(
		'docker',
		[
			'compose',
			'exec',
			'-T',
			'mysql',
			'sh',
			'-c',
			`mysql -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE"`
		],
		{ cwd: root, input: sql, encoding: 'utf8' }
	);
	if (result.status !== 0) {
		throw new Error(`SQL の実行に失敗した:\n${result.stderr || result.stdout}`);
	}
	return result.stdout;
}

async function api(path, options = {}) {
	const response = await fetch(`${BASE_URL}/api${path}`, {
		...options,
		headers: { 'Content-Type': 'application/json', ...(options.headers ?? {}) }
	});
	const text = await response.text();
	return { status: response.status, body: text ? JSON.parse(text) : null };
}

/**
 * 利用者を API で作る。
 *
 * **パスワードのハッシュを SQL に書かない。** 書くと、ハッシュの
 * 作り方を変えたときに種データだけ古い形のまま残り、
 * ログインできない理由が分からなくなる。登録の経路をそのまま通す。
 */
async function seedUsers() {
	const users = [];
	for (let i = 0; i < USERS; i++) {
		const handle = `${PREFIX}${i}`;
		const email = `${handle}@perf.test`;

		let result = await api('/auth/signup', {
			method: 'POST',
			body: JSON.stringify({ email, handle, displayName: `負荷試験${i}`, password: PASSWORD })
		});

		// 前回の後片付けが終わる前に落ちた場合、既に居ることがある。
		// その場合はログインして ID を取り直す。
		if (result.status === 409) {
			result = await api('/auth/login', {
				method: 'POST',
				body: JSON.stringify({ email, password: PASSWORD })
			});
		}
		if (result.status !== 201 && result.status !== 200) {
			throw new Error(`利用者を作れない (${result.status}): ${JSON.stringify(result.body)}`);
		}

		users.push({ id: result.body.data.user.id, handle, email, password: PASSWORD });
	}
	return users;
}

/**
 * 投稿をまとめて入れる。
 *
 * **API ではなく SQL で入れる。** 投稿の作成は画像の処理を伴い、
 * 1件あたり数秒かかる。2000件を API で作ると準備だけで1時間を超える。
 * ここで用意したいのは「読むための量」であり、作成の経路ではない。
 *
 * 本文の語は flows.ts の検索語と揃えてある。**揃えないと、
 * 検索が常に0件を返し、索引に当たらないまま速い数字が出る。**
 */
function seedPosts(users) {
	const words = ['海鮮', '紅葉', '温泉', '朝市', '灯台'];
	const values = [];
	for (const user of users) {
		for (let i = 0; i < POSTS_PER_USER; i++) {
			const word = words[i % words.length];
			const prefecture = String((i % 47) + 1).padStart(2, '0');
			values.push(
				`(${user.id}, '${PREFIX}${word}を見に行った ${i}', '${prefecture}', ` +
					`DATE_SUB(CURRENT_DATE, INTERVAL ${i % 365} DAY), ` +
					`DATE_SUB(NOW(), INTERVAL ${i} MINUTE))`
			);
		}
	}

	// 1文が長くなりすぎないように分けて流す。
	const CHUNK = 500;
	for (let i = 0; i < values.length; i += CHUNK) {
		mysql(
			'INSERT INTO posts (user_id, body, prefecture_code, visited_on, created_at) VALUES ' +
				values.slice(i, i + CHUNK).join(',') +
				';'
		);
	}

	// 互いにフォローさせる。**フォロー中フィードが空だと、
	// いちばん重い問い合わせを測らずに終わる。**
	const follows = [];
	for (const a of users) {
		for (const b of users) {
			if (a.id !== b.id) follows.push(`(${a.id}, ${b.id})`);
		}
	}
	mysql(`INSERT IGNORE INTO follows (follower_id, followee_id) VALUES ${follows.join(',')};`);

	// 統計を更新しないと、入れた直後の実行計画が実態とずれる。
	mysql('ANALYZE TABLE posts, follows;');
}

/**
 * 試験で作ったものを消す。
 *
 * **印（perf_）が付いたものだけを消す。** 印で絞らずに消すと、
 * 同じデータベースにある開発用のデータまで巻き込む。
 */
function cleanup() {
	mysql(`
		SET @prefix = '${PREFIX}%';
		DELETE FROM posts WHERE body LIKE @prefix;
		DELETE FROM posts WHERE user_id IN (SELECT id FROM users WHERE handle LIKE @prefix);
		DELETE FROM follows WHERE follower_id IN (SELECT id FROM users WHERE handle LIKE @prefix)
		   OR followee_id IN (SELECT id FROM users WHERE handle LIKE @prefix);
		DELETE FROM likes WHERE user_id IN (SELECT id FROM users WHERE handle LIKE @prefix);
		DELETE FROM comments WHERE user_id IN (SELECT id FROM users WHERE handle LIKE @prefix);
		DELETE FROM notifications WHERE user_id IN (SELECT id FROM users WHERE handle LIKE @prefix)
		   OR actor_id IN (SELECT id FROM users WHERE handle LIKE @prefix);
		DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE handle LIKE @prefix);
		DELETE FROM users WHERE handle LIKE @prefix;
	`);
}

/** k6 を回す。レポートは results/ に残す。 */
function runK6(users) {
	const resultsDir = join(here, 'results');
	mkdirSync(resultsDir, { recursive: true });
	const stamp = new Date().toISOString().replace(/[:.]/g, '-');
	const summary = join(resultsDir, `${scenario}-${stamp}.json`);

	const result = spawnSync(
		'k6',
		['run', '--summary-export', summary, join(here, 'scenarios', `${scenario}.ts`)],
		{
			cwd: here,
			stdio: 'inherit',
			env: {
				...process.env,
				PERF_BASE_URL: BASE_URL,
				PERF_USERS: JSON.stringify(
					users.map((u) => ({ email: u.email, password: u.password, handle: u.handle }))
				)
			}
		}
	);

	console.log(`\nレポート: ${summary}`);
	return result.status ?? 1;
}

// ---------------------------------------------------------------------------

let exitCode = 1;
try {
	console.log(`[1/3] 種データを入れる（利用者 ${USERS}人 / 投稿 ${USERS * POSTS_PER_USER}件）`);
	// 前回の残りがあれば先に消す。**残ったまま足すと量が読めなくなる。**
	cleanup();
	const users = await seedUsers();
	seedPosts(users);

	console.log(`[2/3] k6 を回す（${scenario}）`);
	exitCode = runK6(users);
} catch (error) {
	console.error(error instanceof Error ? error.message : error);
	exitCode = 1;
} finally {
	// **失敗しても片付ける。** レポートは既に書き出してある。
	console.log('[3/3] 後片付け');
	try {
		cleanup();
		console.log('試験で作ったデータは残っていない');
	} catch (error) {
		console.error('後片付けに失敗した。手で確認すること:', error);
		exitCode = exitCode || 1;
	}
}

process.exit(exitCode);
