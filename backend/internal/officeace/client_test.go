package officeace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/accounts"
	"codearts2api/internal/provider"
)

func TestSignerV4AndV11Determinism(t *testing.T) {
	ak := "TEST_ACCESS_KEY_0001"
	sk := "TEST_SECRET_KEY_0001"
	token := "TEST_SECURITY_TOKEN"
	date := "20261120T030000Z"

	v4Headers, err := signV4(ak, sk, token, "GET", "https://agentarts.cn-southwest-2.myhuaweicloud.com/v1/claw/client-permission-validate", nil, nil, date)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v4Headers["Authorization"], "SDK-HMAC-SHA256 Access=TEST_ACCESS_KEY_0001") {
		t.Fatalf("v4 auth header invalid: %s", v4Headers["Authorization"])
	}
	if v4Headers["x-sdk-date"] != date || v4Headers["x-security-token"] != token {
		t.Fatalf("v4 headers missing: %+v", v4Headers)
	}

	v11Headers, err := signV11(ak, sk, token, "project-123", "GET", "https://officeace.cn-southwest-2.huaweicloud-agentarts.com/v1/subscription", nil, nil, date)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v11Headers["Authorization"], "V11-HMAC-SHA256 Credential=TEST_ACCESS_KEY_0001/20261120/cn-southwest-2/apic") {
		t.Fatalf("v11 auth header invalid: %s", v11Headers["Authorization"])
	}
	if v11Headers["x-subscription-type"] != "v2" || v11Headers["x-project-id"] != "project-123" {
		t.Fatalf("v11 headers missing: %+v", v11Headers)
	}
}

func TestNormalizeAndVerifyOfficeAceCredentials(t *testing.T) {
	c := New()

	input := []byte(`{
		"access_key_id": "AK_TEST",
		"secret_access_key": "SK_TEST",
		"security_token": "TOKEN_TEST",
		"expires_at": "2026-11-21T00:00:00Z",
		"refresh_token": "RT_TEST",
		"dpop_private_key_jwk": {"kty":"EC","crv":"P-256","x":"123","y":"456","d":"789"},
		"user_phone": "13800000000",
		"domain_id": "domain_test_123"
	}`)

	norm, id, err := c.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if id.ID != "domain_test_123" || id.Name != "13800000000" || !id.HasRefresh {
		t.Fatalf("identity mismatch: %+v", id)
	}

	var parsed Credentials
	if err := json.Unmarshal(norm, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.AccessKeyID != "AK_TEST" || parsed.RefreshToken != "RT_TEST" {
		t.Fatalf("normalized mismatch: %+v", parsed)
	}
}

func TestBrowserLoginAndPolling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case AuthStatePath:
			w.Write([]byte(`{"state":"state_12345"}`))
		case AuthCodePath:
			if r.URL.Query().Get("state") == "state_12345" {
				w.Write([]byte(`{"code":"auth_code_xyz"}`))
			} else {
				w.WriteHeader(404)
			}
		case "/v1/oauth2/tokens":
			w.Write([]byte(`{
				"credentials": {
					"access_key_id": "AK_STAGED",
					"secret_access_key": "SK_STAGED",
					"security_token": "TOKEN_STAGED",
					"expiration": "2026-11-21T00:00:00Z"
				},
				"refresh_token": "REFRESH_STAGED"
			}`))
		case PermissionPath:
			w.Write([]byte(`{
				"account_id": "acct_999",
				"model_info": {
					"model_auth_info": {
						"model_app_key": "app_key_test",
						"model_app_secret": "app_secret_test"
					},
					"model_api_url_base": "maas-gw.test.com"
				}
			}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	c := New()
	c.CloudBase = server.URL
	c.AuthBase = server.URL
	c.TokenURL = server.URL + "/v1/oauth2/tokens"

	login, err := c.BeginLogin(context.Background(), "cn")
	if err != nil {
		t.Fatal(err)
	}
	if login.State != "state_12345" || !strings.Contains(login.URL, "authui/login.html") {
		t.Fatalf("login output unexpected: %+v", login)
	}

	raw, ready, err := c.CheckLogin(context.Background(), "cn", login.State)
	if err != nil || !ready {
		t.Fatalf("check login failed: ready=%v err=%v", ready, err)
	}

	var cred Credentials
	if err := json.Unmarshal(raw, &cred); err != nil {
		t.Fatal(err)
	}
	if cred.AccessKeyID != "AK_STAGED" || cred.ModelAppKey != "app_key_test" || cred.DomainID != "acct_999" {
		t.Fatalf("exchanged cred unexpected: %+v", cred)
	}
}

func TestCreditParsingAndWindows(t *testing.T) {
	subJSON := []byte(`{
		"skus": [
			{
				"sku_code": "officeace.personal.package.trial",
				"sku_name": "试用版",
				"quotas": [
					{
						"sku_attr_code": "officeace_points",
						"sku_value": 1000.0,
						"current_value": 200.0,
						"remaining_value": 800.0
					}
				]
			}
		],
		"bonus_skus": [
			{
				"activity_name": "每日签到奖励",
				"points": 50.0,
				"current_value": 0.0,
				"expired_time": "2026-11-25T00:00:00Z"
			}
		]
	}`)

	var subDoc subcriptionDoc
	if err := json.Unmarshal(subJSON, &subDoc); err != nil {
		t.Fatal(err)
	}

	nowMs := time.Now().UnixMilli()
	info := summarizeSubscriptionDoc(subDoc, nowMs)
	if !info.Available || info.Total != 1050 || info.Remaining != 850 || info.Used != 200 {
		t.Fatalf("credit info mismatch: %+v", info)
	}
	if len(info.Windows) != 3 {
		t.Fatalf("expected 3 windows (agg + sku + bonus), got %d", len(info.Windows))
	}
}

func TestStreamChatCompletions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/chat/completions" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}`,
			`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":" World"},"finish_reason":"stop"}]}`,
			`data: {"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "%s\n\n", chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	c := New()
	c.HTTP = server.Client()
	cred := Credentials{
		AccessKeyID:     "AK_STREAM",
		SecretAccessKey: "SK_STREAM",
		SecurityToken:   "TOKEN_STREAM",
		ModelAppKey:     "app_k",
		ModelAppSecret:  "app_s",
		MaaSBaseURL:     server.URL + "/v2",
	}
	credRaw, _ := json.Marshal(cred)

	req := provider.Request{
		Model: "deepseek-v4.1-flash",
		Body:  []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}

	var received []string
	err := c.Stream(context.Background(), credRaw, req, func(chunk []byte) error {
		received = append(received, string(chunk))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(received) != 3 {
		t.Fatalf("expected 3 emitted chunks, got %d", len(received))
	}
}

func TestUnprovisionedAccountReportsStateNotAnEmptyBalance(t *testing.T) {
	// Measured shape of a signed-in account that never redeemed an invitation:
	// the permission answer carries `"subscription":null`, and the subscription
	// endpoint answers with the envelope only.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case PermissionPath:
			w.Write([]byte(`{"account_id":"0352053387db479a8c7228b6b2f1ea9f","principal_urn":"iam::x:user:fixture","principal_id":"principal_fixture","subscription":null}`))
		case SubscriptionPath:
			w.Write([]byte(`{"domain_id":"0352053387db479a8c7228b6b2f1ea9f","subscribe_status":"UNSUBSCRIBED"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	c := New()
	c.HTTP = server.Client()
	c.CloudBase = server.URL
	c.SubBase = server.URL

	credRaw, _ := json.Marshal(Credentials{
		AccessKeyID:     "AK_UNSUB",
		SecretAccessKey: "SK_UNSUB",
		SecurityToken:   "TOKEN_UNSUB",
		AccountID:       "0352053387db479a8c7228b6b2f1ea9f",
	})

	// 1. Quota must name the account state, not an empty balance.
	out, err := c.Credit(context.Background(), credRaw)
	if err != nil {
		t.Fatal(err)
	}
	var info accounts.CreditInfo
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatal(err)
	}
	if info.Available || info.Status != "unsubscribed" || info.Reason != UnsubscribedReason {
		t.Fatalf("unprovisioned quota reported as data: %+v", info)
	}
	if len(info.Windows) != 0 {
		t.Fatalf("invented quota windows for an unprovisioned account: %+v", info.Windows)
	}

	// 2. Catalog must refuse instead of advertising the fallback list.
	if _, err := c.Models(context.Background(), credRaw); err == nil {
		t.Fatal("unprovisioned account advertised models")
	} else {
		var p *provider.Error
		if !errors.As(err, &p) || p.Status != 403 || p.Code != "OFFICEACE_NOT_PROVISIONED" || !strings.Contains(p.Message, "邀请码") {
			t.Fatalf("catalog refusal is not actionable: %v", err)
		}
	}

	// 3. Check-in must not be advertised as claimable.
	reward, err := c.DailyStatus(context.Background(), credRaw)
	if err != nil {
		t.Fatal(err)
	}
	if reward.State != "unavailable" || !strings.Contains(reward.Message, "邀请码") {
		t.Fatalf("check-in advertised for an unprovisioned account: %+v", reward)
	}

	// 4. Inference must fail with the same sentence, not a bare credential error.
	err = c.Stream(context.Background(), credRaw, provider.Request{Model: "glm-5.3", Body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}, func([]byte) error { return nil })
	var p *provider.Error
	if !errors.As(err, &p) || !strings.Contains(p.Message, "邀请码") {
		t.Fatalf("inference refusal is not actionable: %v", err)
	}
}

func TestSubscribedAccountWithoutGatewayIsDistinguished(t *testing.T) {
	// A redeemed account that the cloud has not handed gateway keys for yet: the
	// subscription block exists, so the advice must differ from "redeem a code".
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == PermissionPath {
			w.Write([]byte(`{"account_id":"acct_sub","subscription":{"status":"ACTIVE","model_info":null}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	c := New()
	c.HTTP = server.Client()
	c.CloudBase = server.URL
	credRaw, _ := json.Marshal(Credentials{AccessKeyID: "AK_SUB", SecretAccessKey: "SK_SUB", SecurityToken: "TOKEN_SUB", AccountID: "acct_sub"})

	_, err := c.Models(context.Background(), credRaw)
	var p *provider.Error
	if !errors.As(err, &p) || p.Code != "OFFICEACE_NOT_PROVISIONED" {
		t.Fatalf("subscribed account without gateway not refused: %v", err)
	}
	if strings.Contains(p.Message, "兑换") {
		t.Fatalf("advice ignored the existing subscription: %v", p.Message)
	}
}

func TestDailyClaimMadeInClientReadsAsAlreadyClaimed(t *testing.T) {
	// The operator checked in from the OfficeAce client; the gateway must read
	// that bucket as the claim state and must not post a second time.
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case SubscriptionPath:
			w.Write([]byte(`{"domain_id":"acct","subscribe_status":"SUBSCRIBED","bonus_skus":[
				{"activity_id":"bonus-new-user-2026","activity_name":"新用户专属福利","cbc_resource_id":"bonus:bonus-new-user-2026:20261120161800:once","points":"300","current_value":"0","effective_time":"2026-11-20T08:18:00.592Z"},
				{"activity_id":"bonus-daily-checkin-2026","activity_name":"每日签到有礼","cbc_resource_id":"bonus:bonus-daily-checkin-2026:20261120161800:daily","points":"50","current_value":"1.3474979423868314","effective_time":"2026-11-20T08:18:00.775Z"}]}`))
		case BonusClaimPath:
			posts++
			w.WriteHeader(403)
			w.Write([]byte(`{"error_code":"APIG.0308","error_msg":"该活动今日已参与"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	c := New()
	c.HTTP = server.Client()
	c.SubBase = server.URL
	credRaw, _ := json.Marshal(Credentials{AccessKeyID: "AK", SecretAccessKey: "SK", SecurityToken: "TOKEN", AccountID: "acct"})

	today := time.Now().In(officeAceDayZone).Format("2006-01-02")
	if today != "2026-11-20" {
		t.Skip("fixture is pinned to 2026-11-20")
	}

	status, err := c.DailyStatus(context.Background(), credRaw)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "already_claimed" || !strings.Contains(status.Message, "已领取") {
		t.Fatalf("client check-in not read as claimed: %+v", status)
	}

	claim, err := c.ClaimDaily(context.Background(), credRaw)
	if err != nil {
		t.Fatalf("claim reported a failure for an already-claimed day: %v", err)
	}
	if claim.State != "already_claimed" {
		t.Fatalf("claim state: %+v", claim)
	}
	if posts != 0 {
		t.Fatalf("posted a duplicate claim %d time(s)", posts)
	}
}

func TestCheckinDayBoundaryFollowsUtcPlusEight(t *testing.T) {
	// OfficeAce stamps UTC while the campaign day turns over at 00:00 UTC+8, so a
	// raw prefix match misfiles every grant made after 16:00 UTC.
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, officeAceDayZone) // 2026-11-20 04:00Z
	var sameDay, nextDay subcriptionDoc
	if err := json.Unmarshal([]byte(`{"bonus_skus":[{"cbc_resource_id":"bonus:x:1:daily","points":"50","effective_time":"2026-11-19T17:00:00.000Z"}]}`), &sameDay); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"bonus_skus":[{"cbc_resource_id":"bonus:x:1:daily","points":"50","effective_time":"2026-11-20T17:00:00.000Z"}]}`), &nextDay); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := dailyBonus(sameDay, now); !ok {
		t.Fatal("18:00Z of the previous UTC day is 02:00 UTC+8 today and must count")
	}
	if _, _, ok := dailyBonus(nextDay, now); ok {
		t.Fatal("17:00Z today is next-day 01:00 UTC+8 and must not count")
	}
}

func TestClaimRejectionTrustsCampaignState(t *testing.T) {
	// Measured rejection shape: the post is refused although the client already
	// granted the bucket. The state wins over the status code.
	reads, posts := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case SubscriptionPath:
			reads++
			if reads == 1 {
				w.Write([]byte(`{"subscribe_status":"SUBSCRIBED","bonus_skus":[]}`))
				return
			}
			today := time.Now().In(officeAceDayZone).Format("2006-01-02")
			w.Write([]byte(`{"subscribe_status":"SUBSCRIBED","bonus_skus":[{"activity_name":"每日签到有礼","cbc_resource_id":"bonus:bonus-daily-checkin-2026:x:daily","points":"50","effective_time":"` + today + `T01:00:00.000Z"}]}`))
		case BonusClaimPath:
			posts++
			w.WriteHeader(403)
			w.Write([]byte(`{"error_code":"APIG.0308","error_msg":"该活动今日已参与"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	c := New()
	c.HTTP = server.Client()
	c.SubBase = server.URL
	credRaw, _ := json.Marshal(Credentials{AccessKeyID: "AK", SecretAccessKey: "SK", SecurityToken: "TOKEN", AccountID: "acct"})

	result, err := c.ClaimDaily(context.Background(), credRaw)
	if err != nil {
		t.Fatalf("rejection surfaced as a hard error: %v", err)
	}
	if result.State != "already_claimed" || !strings.Contains(result.Message, "已领取") {
		t.Fatalf("campaign state not trusted over status: %+v", result)
	}
	if posts != 1 {
		t.Fatalf("unexpected post count %d", posts)
	}
}

func TestClaimKeepsActionableRejectionWhenBucketAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case SubscriptionPath:
			w.Write([]byte(`{"subscribe_status":"SUBSCRIBED","bonus_skus":[]}`))
		case BonusClaimPath:
			w.WriteHeader(403)
			w.Write([]byte(`{"error_code":"APIG.0308","error_msg":"该活动今日已参与"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	c := New()
	c.HTTP = server.Client()
	c.SubBase = server.URL
	credRaw, _ := json.Marshal(Credentials{AccessKeyID: "AK", SecretAccessKey: "SK", SecurityToken: "TOKEN", AccountID: "acct"})

	result, err := c.ClaimDaily(context.Background(), credRaw)
	if err == nil {
		t.Fatal("a rejection with no campaign state was reported as success")
	}
	if result.State != "failed" || !strings.Contains(result.Message, "APIG.0308") {
		t.Fatalf("rejection detail lost: %+v", result)
	}
}

func TestClaimParsesStringPoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case SubscriptionPath:
			w.Write([]byte(`{"subscribe_status":"SUBSCRIBED","bonus_skus":[]}`))
		case BonusClaimPath:
			w.Write([]byte(`{"bonus_skus":[{"activity_name":"每日签到有礼","points":"50"}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	c := New()
	c.HTTP = server.Client()
	c.SubBase = server.URL
	credRaw, _ := json.Marshal(Credentials{AccessKeyID: "AK", SecretAccessKey: "SK", SecurityToken: "TOKEN", AccountID: "acct"})

	result, err := c.ClaimDaily(context.Background(), credRaw)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "claimed" || result.Credits == nil || *result.Credits != 50 {
		t.Fatalf("decimal-string points not parsed: %+v", result)
	}
}

func TestCreditUsesPublishedPlanNameAndBonusOnly(t *testing.T) {
	// Measured personal-trial shape: the SKU carries feature counters only, and
	// every point comes from bonus buckets. The card must still add up.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == SubscriptionPath {
			w.Write([]byte(`{"domain_id":"acct","subscribe_status":"SUBSCRIBED",
				"skus":[{"sku_code":"officeace.personal.package.trial","sku_name":"个人体验版","effective_time":"2026-11-20T08:14:05.800Z","expired_time":"2026-12-20T08:14:05.800Z",
					"quotas":[{"sku_attr_code":"online_search_count","sku_value":"200","current_value":"0"}]}],
				"bonus_skus":[
					{"activity_name":"新用户专属福利","cbc_resource_id":"bonus:bonus-new-user-2026:x:once","points":"300","current_value":"0","effective_time":"2026-11-20T08:18:00.592Z","expired_time":"2026-12-20T08:18:00.592Z"},
					{"activity_name":"每日签到有礼","cbc_resource_id":"bonus:bonus-daily-checkin-2026:x:daily","points":"50","current_value":"1.3474979423868314","effective_time":"2026-11-20T08:18:00.775Z","expired_time":"2026-12-20T15:59:59.000Z"}]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	c := New()
	c.HTTP = server.Client()
	c.SubBase = server.URL
	credRaw, _ := json.Marshal(Credentials{AccessKeyID: "AK", SecretAccessKey: "SK", SecurityToken: "TOKEN", AccountID: "acct"})

	out, err := c.Credit(context.Background(), credRaw)
	if err != nil {
		t.Fatal(err)
	}
	var info accounts.CreditInfo
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.PackageName != "云果办 · 个人体验版" {
		t.Fatalf("plan name not taken from the cloud: %+v", info)
	}
	if info.Total != 350 || info.Remaining != 348.65 || info.Used != 1.35 {
		t.Fatalf("bonus-only credits did not add up: total=%v used=%v remaining=%v", info.Total, info.Used, info.Remaining)
	}
	if info.Status != "active" {
		t.Fatalf("subscribed account reported as %q", info.Status)
	}
}

func TestTokenRatesWrapperAndBonusCurrentValue(t *testing.T) {
	ratesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == TokenRatesPath {
			// Test wrapper object with "models" key and whitespace/newlines in model names
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"models": [
					{"model_name": "  glm-5.3 \n", "cost_coefficient": 1.5},
					{"modelName": "deepseek-v4.1-flash", "costCoefficient": 1.0}
				]
			}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer ratesServer.Close()

	c := New()
	c.HTTP = ratesServer.Client()
	c.SubBase = ratesServer.URL

	cred := Credentials{
		AccessKeyID:     "AK_RATE",
		SecretAccessKey: "SK_RATE",
		SecurityToken:   "TOKEN_RATE",
		AccountID:       "acct_rates",
	}

	rates, err := c.fetchTokenRates(context.Background(), cred)
	if err != nil {
		t.Fatal(err)
	}
	if rates["glm-5.3"] != 1.5 || rates["deepseek-v4.1-flash"] != 1.0 {
		t.Fatalf("rates parsed incorrectly: %+v", rates)
	}

	// Verify BonusSKUs current_value is treated as used, remaining = total - used
	bonusJSON := []byte(`{
		"bonus_skus": [
			{
				"activity_id": "act_daily_01",
				"activity_name": "每日签到奖励",
				"cbc_resource_id": "officeace.daily.bonus",
				"points": 100.0,
				"current_value": 30.0,
				"effective_time": "2026-11-20 10:00:00",
				"expired_time": "2026-11-25T00:00:00Z"
			}
		]
	}`)
	var doc subcriptionDoc
	if err := json.Unmarshal(bonusJSON, &doc); err != nil {
		t.Fatal(err)
	}
	nowMs := time.Now().UnixMilli()
	info := summarizeSubscriptionDoc(doc, nowMs)
	if info.Total != 100.0 || info.Used != 30.0 || info.Remaining != 70.0 {
		t.Fatalf("bonus current_value used math failed: total=%v used=%v remaining=%v", info.Total, info.Used, info.Remaining)
	}
}
