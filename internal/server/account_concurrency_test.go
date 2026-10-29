package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAdminAccountConcurrencyValidationAndIsolation(t *testing.T) {
	s := newAdminTestServer(t, "a", "b")
	for _, body := range []string{
		`{"action":"concurrency","id":"a"}`,
		`{"action":"concurrency","id":"a","max_in_flight":null}`,
		`{"action":"concurrency","id":"a","max_in_flight":-1}`,
		`{"action":"concurrency","id":"a","max_in_flight":33}`,
		`{"action":"concurrency","id":"a","max_in_flight":1.5}`,
		`{"action":"concurrency","max_in_flight":2}`,
	} {
		if w := adminProbeRequest(t, s, http.MethodPost, "/admin/api/accounts", body); w.Code != 400 {
			t.Fatalf("invalid input accepted: %d %s", w.Code, w.Body.String())
		}
	}
	w := adminProbeRequest(t, s, http.MethodPost, "/admin/api/accounts", `{"action":"concurrency","id":"missing","max_in_flight":1}`)
	if w.Code != 404 {
		t.Fatal("missing account accepted")
	}
	w = adminProbeRequest(t, s, http.MethodPost, "/admin/api/accounts", `{"action":"concurrency","id":"b","max_in_flight":3}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var out struct {
		ID    string `json:"id"`
		Limit int    `json:"max_in_flight"`
		Pool  struct {
			Limit int `json:"max_in_flight"`
		} `json:"pool"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.ID != "b" || out.Limit != 3 || out.Pool.Limit != 3 {
		t.Fatal("wrong response", w.Body.String())
	}
	if s.Store.Account("a").MaxInFlight != 0 || s.Store.Account("b").MaxInFlight != 3 {
		t.Fatal("other account changed")
	}
	w = adminProbeRequest(t, s, http.MethodPost, "/admin/api/accounts", `{"action":"concurrency","id":"b","max_in_flight":0}`)
	if w.Code != 200 || s.Store.Account("b").MaxInFlight != 0 {
		t.Fatal("cannot restore inherited limit")
	}
}
