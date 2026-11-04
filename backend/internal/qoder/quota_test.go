package qoder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codearts2api/internal/accounts"
)

func TestQuotaFiniteSharedUnlimitedAndUnknown(t *testing.T) {
	var o map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"userQuota":{"total":300,"used":147.5,"remaining":152.5,"percentage":0.49},"addOnQuota":{"total":10,"used":2.25,"remaining":7.75},"orgResourcePackage":{"cap":2500,"used":1548,"remaining":952,"available":true,"unit":"credits"}}`), &o)
	b, e := parseQuota(o, 1793990000)
	if e != nil {
		t.Fatal(e)
	}
	var v accounts.CreditInfo
	_ = json.Unmarshal(b, &v)
	if !v.Available || v.Total != 2810 || v.Remaining != 1112.25 || len(v.Windows) != 3 || *v.Windows[0].Utilization < 49 {
		t.Fatalf("wrong credits %+v", v)
	}
	o = nil
	_ = json.Unmarshal([]byte(`{"orgResourcePackage":{"cap":-1,"used":0,"remaining":999,"available":true},"isQuotaExceeded":true}`), &o)
	b, e = parseQuota(o, 1)
	if e != nil {
		t.Fatal(e)
	}
	v = accounts.CreditInfo{}
	_ = json.Unmarshal(b, &v)
	if !v.Available || len(v.Windows) != 1 || !v.Windows[0].Unlimited || v.Windows[0].Remaining != nil || v.Total != 0 {
		t.Fatal("unlimited sentinel became a balance")
	}
	for _, s := range []string{`{}`, `{"isQuotaExceeded":true,"userQuota":{"total":0,"used":0,"remaining":0}}`, `{"userQuota":{"total":20,"used":2,"unit":"tokens"}}`} {
		o = nil
		_ = json.Unmarshal([]byte(s), &o)
		b, e = parseQuota(o, 1)
		if e != nil {
			t.Fatal(e)
		}
		v = accounts.CreditInfo{}
		_ = json.Unmarshal(b, &v)
		if v.Available {
			t.Fatal("unknown became zero balance")
		}
	}
	o = nil
	_ = json.Unmarshal([]byte(`{"userQuota":{"remaining":7.5},"isQuotaExceeded":true}`), &o)
	b, e = parseQuota(o, 1)
	if e != nil {
		t.Fatal(e)
	}
	v = accounts.CreditInfo{}
	_ = json.Unmarshal(b, &v)
	if !v.Available || v.Windows[0].Total != nil || *v.Windows[0].Remaining != 7.5 {
		t.Fatal("partial quota was fabricated or discarded")
	}
}
func TestQuotaQueriesOwnRealmAndUnwrapsEnvelope(t *testing.T) {
	for _, scope := range []string{"cn", "global"} {
		t.Run(scope, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v2/quota/usage" || r.Header.Get("Authorization") != "Bearer quota-fixture" {
					t.Error("wrong quota query")
				}
				w.Write([]byte(`{"code":0,"data":{"userQuota":{"total":10,"used":0,"remaining":10}}}`))
			}))
			defer up.Close()
			c := New()
			region := c.Regions[scope]
			region.OpenAPI = up.URL
			c.Regions[scope] = region
			a := Credentials{AccessToken: "quota-fixture", UID: "fixture", MachineID: "fixture", Scope: scope, ExpiresAt: time.Now().Add(time.Hour).Unix()}
			raw, _ := json.Marshal(a)
			b, _, e := c.Normalize(raw)
			if e != nil {
				t.Fatal(e)
			}
			out, e := c.Credit(context.Background(), b)
			if e != nil {
				t.Fatal(e)
			}
			var v accounts.CreditInfo
			_ = json.Unmarshal(out, &v)
			if !v.Available || v.Total != 10 || v.Used != 0 {
				t.Fatal("zero usage or envelope lost")
			}
		})
	}
}
