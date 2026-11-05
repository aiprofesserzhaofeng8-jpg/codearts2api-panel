package trae

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAuditTraeRetainsLargeSchemaNumbers(t *testing.T) {
	body, err := prepareBody([]byte(`{"messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"find","parameters":{"type":"object","properties":{"id":{"type":"integer","const":9007199254740993}},"x-decimal":1.234567890123456789}}}],"seed":9007199254740993,"max_tokens":32}`), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(body)
	if err != nil || !strings.Contains(string(raw), "9007199254740993") || !strings.Contains(string(raw), "1.234567890123456789") {
		t.Fatal("numeric meaning changed", string(raw), err)
	}
}
