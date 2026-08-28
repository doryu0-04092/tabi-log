package httpapi

import (
	"sync"
	"time"
)

// RateLimiter は「一定時間内に何回まで」を数える。
//
// **プロセスのメモリで数えるため、タスクが複数あると各タスクが独立に数える。**
// 2タスク構成では実効的な上限が2倍になる。ログイン試行の抑止としては
// その分弱まるが、素朴な総当たりを止める効果は残る。
//
// 厳密に効かせるには ElastiCache などの共有ストアか、
// ALB/WAF 側でのレート制限が必要になる。学習用の構成では
// 部品を増やす方の代償が大きいと判断し、この制約を受け入れている
// （docs/aws-architecture.md の「学習用の割り切り」に記載）。
type RateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*window
	limit    int
	period   time.Duration
	// lastSweep は古い記録を掃除した時刻。
	lastSweep time.Time
	now       func() time.Time
}

type window struct {
	count     int
	expiresAt time.Time
}

// NewRateLimiter を作る。limit 回を period の間に許す。
func NewRateLimiter(limit int, period time.Duration) *RateLimiter {
	return &RateLimiter{
		attempts: make(map[string]*window),
		limit:    limit,
		period:   period,
		now:      time.Now,
	}
}

// Allow は1回分を記録し、上限内なら true を返す。
//
// key には利用者が自由に決められない値を使うこと。
// 例えばメールアドレスだけを鍵にすると、攻撃者が他人のアドレスを
// 大量に試すことで**その利用者をログインできなくできる**。
// IP とアカウントの両方で別々に数える。
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	w, ok := l.attempts[key]
	if !ok || now.After(w.expiresAt) {
		l.attempts[key] = &window{count: 1, expiresAt: now.Add(l.period)}
		return true
	}

	w.count++
	return w.count <= l.limit
}

// Reset は記録を消す。ログインに成功したときに呼び、
// 正しく使えている利用者が上限に近づいたままにならないようにする。
func (l *RateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// sweepLocked は期限切れの記録を捨てる。
//
// 掃除しないと、失敗した鍵の数だけ map が伸び続ける。
// 攻撃者が毎回違うアドレスで試せば、それがそのままメモリの消費になる。
func (l *RateLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < l.period {
		return
	}
	l.lastSweep = now
	for k, w := range l.attempts {
		if now.After(w.expiresAt) {
			delete(l.attempts, k)
		}
	}
}
