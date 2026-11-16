package cnapi

import (
	"encoding/json"
	"testing"
)

func TestZhipuCodingWindows(t *testing.T) {
	for _, typ := range []string{"TOKENS_LIMIT", "CREDIT_LIMIT"} {
		var obj map[string]json.RawMessage
		json.Unmarshal([]byte(`{"success":true,"data":{"limits":[{"type":"`+typ+`","unit":6,"percentage":0,"nextResetTime":1795000000000},{"type":"`+typ+`","unit":3,"percentage":100,"nextResetTime":1796000000000}]}}`), &obj)
		w, err := zhipuCodingWindows(obj)
		if err != nil || len(w) != 2 || w[0].WindowSeconds != 604800 || *w[0].Utilization != 0 || w[1].WindowSeconds != 18000 || w[0].ResetsAt != 1795000000 {
			t.Fatal(w, err)
		}
	}
	for _, body := range []string{`{}`, `{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3}]}}`} {
		var obj map[string]json.RawMessage
		json.Unmarshal([]byte(body), &obj)
		w, err := zhipuCodingWindows(obj)
		if err != nil || len(w) != 0 {
			t.Fatal(w, err)
		}
	}
	var obj map[string]json.RawMessage
	json.Unmarshal([]byte(`{"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":25},{"type":"CREDIT_LIMIT","unit":6,"percentage":99}]}}`), &obj)
	w, err := zhipuCodingWindows(obj)
	if err != nil || len(w) != 1 || w[0].WindowSeconds != 0 {
		t.Fatal("guessed window or mixed credit", w, err)
	}
}
func TestMiniMaxCodingWindows(t *testing.T) {
	var obj map[string]json.RawMessage
	json.Unmarshal([]byte(`{"base_resp":{"status_code":0},"model_remains":[{"model_name":"video","current_interval_remaining_percent":99},{"model_name":"general","current_interval_remaining_percent":0,"end_time":1796000000000,"current_weekly_status":1,"current_weekly_remaining_percent":100,"weekly_end_time":"2026-11-17T00:00:00Z"}]}`), &obj)
	w, err := minimaxCodingWindows(obj)
	if err != nil || len(w) != 2 || *w[0].Utilization != 100 || *w[1].Utilization != 0 || w[0].ResetsAt != 1796000000 || w[1].ResetsAt == 0 {
		t.Fatal(w, err)
	}
	for _, body := range []string{`{}`, `{"model_remains":[{"model_name":"general","current_weekly_status":0,"current_weekly_remaining_percent":55}]}`, `{"model_remains":[{"model_name":"general"}]}`} {
		fresh := map[string]json.RawMessage{}
		json.Unmarshal([]byte(body), &fresh)
		w, err := minimaxCodingWindows(fresh)
		if err != nil || len(w) != 0 {
			t.Fatal(w, err)
		}
	}
}
func TestCodingQuotaRejectsInvalidBusinessAndNumbers(t *testing.T) {
	for _, body := range []string{`{"success":false}`, `{"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":-1}]}}`, `{"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":"NaN"}]}}`} {
		var obj map[string]json.RawMessage
		json.Unmarshal([]byte(body), &obj)
		if _, err := zhipuCodingWindows(obj); err == nil {
			t.Fatal(body)
		}
	}
	for _, body := range []string{`{"base_resp":{"status_code":1004}}`, `{"model_remains":[{"model_name":"general","current_interval_remaining_percent":101}]}`, `{"model_remains":[{"model_name":"general","current_interval_remaining_percent":-1}]}`} {
		var obj map[string]json.RawMessage
		json.Unmarshal([]byte(body), &obj)
		if _, err := minimaxCodingWindows(obj); err == nil {
			t.Fatal(body)
		}
	}
}
