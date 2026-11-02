package server

import (
	"codearts2api/internal/storage"
	"encoding/json"
	"math"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Validate selected public DTOs against the same schema used by TS generation.
// Variant provider payloads deliberately remain open; no inference body filter.
func TestManagementContractMatchesHandlers(t *testing.T) {
	raw, err := os.ReadFile("../../../api/admin.openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err = json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	var validate func(any, map[string]any, string)
	validate = func(value any, schema map[string]any, where string) {
		if ref, ok := schema["$ref"].(string); ok {
			validate(value, schemas[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any), where)
			return
		}
		typ := schema["type"]
		types := []any{typ}
		if many, ok := typ.([]any); ok {
			types = many
		}
		nullable := false
		for _, v := range types {
			if v == "null" {
				nullable = true
			}
		}
		if value == nil && nullable {
			return
		}
		matched := typ == nil
		for _, v := range types {
			switch v {
			case "object":
				_, matched = value.(map[string]any)
			case "array":
				_, matched = value.([]any)
			case "string":
				_, matched = value.(string)
			case "boolean":
				_, matched = value.(bool)
			case "number":
				_, matched = value.(float64)
			case "integer":
				n, ok := value.(float64)
				matched = ok && math.Trunc(n) == n
			}
			if matched {
				break
			}
		}
		if !matched {
			t.Errorf("%s: expected %v, got %v", where, typ, reflect.TypeOf(value))
			return
		}
		if required, ok := schema["required"].([]any); ok {
			obj, ok := value.(map[string]any)
			if !ok {
				t.Errorf("%s: expected object", where)
				return
			}
			for _, k := range required {
				if _, ok := obj[k.(string)]; !ok {
					t.Errorf("%s: missing %s", where, k)
				}
			}
		}
		if obj, ok := value.(map[string]any); ok {
			props, _ := schema["properties"].(map[string]any)
			for k, sub := range props {
				if val, exists := obj[k]; exists {
					validate(val, sub.(map[string]any), where+"."+k)
				}
			}
			return
		}
		if values, ok := value.([]any); ok {
			if sub, exists := schema["items"].(map[string]any); exists {
				for _, val := range values {
					validate(val, sub, where+"[]")
				}
			}
			return
		}
	}
	s := poolServer(t, 1)
	group, err := s.Store.SaveGroup(storage.AccountGroup{Name: "contract-fixture", AccountIDs: []string{s.Store.Accounts()[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.AddKeyInGroupAndSave("contract-fixture", group.ID); err != nil {
		t.Fatal(err)
	}
	s.Store.UpdateSettings(func(c *storage.Settings) { c.AdminPassword = "contract-fixture" })
	handler := s.Handler()
	for path, name := range map[string]string{"/health": "Health", "/admin/auth/session": "AdminSession", "/admin/api/state": "State"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "127.0.0.1:40000"
		r.Host = "127.0.0.1:10087"
		r.Header.Set(localPanelHeader, s.localPanelToken)
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
		var value any
		if json.Unmarshal(w.Body.Bytes(), &value) != nil {
			t.Fatal("invalid JSON")
		}
		validate(value, schemas[name].(map[string]any), name)
	}
	// The route inventory must not silently drift from the maintained contract.
	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]any)
	for _, match := range regexp.MustCompile(`mux.HandleFunc\("(/admin/[^\"]+)"`).FindAllStringSubmatch(string(source), -1) {
		if paths[match[1]] == nil {
			t.Error("undocumented management route", match[1])
		}
	}
}
