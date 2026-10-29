package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/codearts"
	"codearts2api/internal/registry"
	"codearts2api/internal/store"
)

// TestMetricsExposesStats 校验 /metrics 的输出格式与关键指标。
func TestMetricsExposesStats(t *testing.T) {
	s := newTestServer(t, 0, 1)
	// 三个账号分别处于 ok / error / disabled，用于验证按状态分组。
	// 注意 UpsertAccount 按 AK 去重，所以每个账号必须用不同的 AccessKey。
	for i, spec := range []struct {
		id      string
		enabled bool
		status  string
	}{
		{"a", true, "ok"},
		{"b", true, "error"},
		{"c", false, "ok"},
	} {
		s.Store.UpsertAccount(&codearts.Account{
			ID: spec.id, Name: spec.id, Enabled: spec.enabled, Status: spec.status,
			Session: &codearts.Session{AccessKey: "ak" + string(rune('0'+i)), SecretKey: "sk"},
		})
	}
	// 制造一点统计量。
	s.Store.Record(store.LogEntry{Model: "m", Status: 200}, 42)
	s.Store.Record(store.LogEntry{Model: "m", Status: 500, Err: "boom"}, 0)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Host = "127.0.0.1:10087"
	req.RemoteAddr = "127.0.0.1:12345" // 本机直连（未设管理密码时允许）
	rec := httptest.NewRecorder()
	s.withAdminAuth(s.handleMetrics)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200（body: %s）", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q，期望 text/plain", ct)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"codearts2api_requests_total 2",
		"codearts2api_requests_failed_total 1",
		"codearts2api_output_chars_total 42",
		// A historical error alone does not prevent the pool retrying an account.
		"codearts2api_accounts_ready 2",
		`codearts2api_accounts{status="ok"} 1`,
		`codearts2api_accounts{status="error"} 1`,
		`codearts2api_accounts{status="disabled"} 1`,
		"codearts2api_models",
		"codearts2api_uptime_seconds",
		"# TYPE codearts2api_requests_total counter",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("指标输出缺少 %q\n---\n%s", want, body)
		}
	}
}

func requireMetricsReady(t *testing.T, s *Server, want int) {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleMetrics(w, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(w.Body.String(), fmt.Sprintf("\ncodearts2api_accounts_ready %d\n", want)) {
		t.Fatalf("ready should be %d: %s", want, w.Body.String())
	}
}

func TestMetricsReadyMatchesAccountAvailability(t *testing.T) {
	for _, tc := range []struct {
		name, status              string
		disabled, paused          bool
		cooldown, limit, occupied int
		ready                     int
	}{
		{name: "ready", status: "ok", ready: 1},
		{name: "disabled", status: "ok", disabled: true},
		{name: "paused", status: "ok", paused: true},
		{name: "cooling", status: "ok", cooldown: 60},
		{name: "cooldown_expired", status: "ok", cooldown: -60, ready: 1},
		{name: "default_capacity_full", status: "ok", occupied: 2},
		{name: "override_capacity_full", status: "ok", limit: 1, occupied: 1},
		{name: "override_capacity_available", status: "ok", limit: 3, occupied: 2, ready: 1},
		{name: "historical_error_can_retry", status: "error", ready: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t, 0, 1)
			a := &codearts.Account{ID: "a", Name: "a", Session: &codearts.Session{AccessKey: "ak", SecretKey: "sk"}}
			a.Enabled, a.Paused, a.Status, a.MaxInFlight = !tc.disabled, tc.paused, tc.status, tc.limit
			if tc.cooldown != 0 {
				a.CooldownUntil = time.Now().Add(time.Duration(tc.cooldown) * time.Second).Unix()
			}
			s.Store.UpsertAccount(a)
			var releases []func()
			for i := 0; i < tc.occupied; i++ {
				acct, release := s.Store.AcquireAccountForModel("", "test-model", nil)
				if acct == nil {
					t.Fatal("cannot establish occupied slots")
				}
				t.Cleanup(release)
				releases = append(releases, release)
			}
			requireMetricsReady(t, s, tc.ready)
			if s.Store.PoolSnapshot()["a"].InFlight != tc.occupied {
				t.Fatal("scrape changed leases")
			}
			acct, release := s.Store.AcquireAccountForModel("", "test-model", nil)
			defer release()
			if (acct != nil) != (tc.ready == 1) {
				t.Fatal("metric disagrees with scheduler")
			}
			release()
			for _, release := range releases {
				release()
			}
			if tc.occupied > 0 {
				requireMetricsReady(t, s, 1)
			}
			if tc.paused {
				s.Store.SetAccountPaused("a", false)
				requireMetricsReady(t, s, 1)
			}
		})
	}
}

func TestMetricsReadyIsGlobalNotModelSpecific(t *testing.T) {
	s := newTestServer(t, 1, 1)
	s.Store.MarkModelResult("a", "cooling-model", false, "fixture", 60)
	requireMetricsReady(t, s, 1)
	a, release := s.Store.AcquireAccountForModel("", "cooling-model", nil)
	defer release()
	if a != nil {
		t.Fatal("cooling model should not be selectable")
	}
	a, releaseOther := s.Store.AcquireAccountForModel("", "other-model", nil)
	defer releaseOther()
	if a == nil {
		t.Fatal("model cooldown should not hide other models")
	}
}

func BenchmarkMetricsSnapshot(b *testing.B) {
	for _, count := range []int{12, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			st, err := store.Open(filepath.Join(b.TempDir(), "state.json"))
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < count; i++ {
				st.UpsertAccount(&codearts.Account{ID: fmt.Sprint(i), Enabled: true, Status: "ok", Session: &codearts.Session{AccessKey: fmt.Sprint("ak-", i)}})
			}
			s := &Server{Store: st, Reg: registry.New()}
			req := httptest.NewRequest("GET", "/metrics", nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.handleMetrics(httptest.NewRecorder(), req)
			}
		})
	}
}

// TestMetricsRequiresAdminAuth 未设密码时，非本机直连必须被拒绝 ——
// 与其它管理接口保持同一套鉴权模型。
func TestMetricsRequiresAdminAuth(t *testing.T) {
	s := newTestServer(t, 0, 1)

	// httptest 默认 RemoteAddr 是 192.0.2.1，不是环回地址。
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	s.withAdminAuth(s.handleMetrics)(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("非本机直连状态码 = %d，期望 403（body: %s）", rec.Code, rec.Body.String())
	}

	// 设了管理密码后，携带正确密码即可访问（Prometheus 用 Bearer）。
	s.Store.UpdateSettings(func(st *store.Settings) { st.AdminPassword = "pw" })
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "203.0.113.9:12345"
	req.Header.Set("Authorization", "Bearer pw")
	rec = httptest.NewRecorder()
	s.withAdminAuth(s.handleMetrics)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("带正确密码状态码 = %d，期望 200（body: %s）", rec.Code, rec.Body.String())
	}
}
