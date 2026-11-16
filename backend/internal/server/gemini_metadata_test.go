package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMessagesToolMetadataNeverSilentlyChangesAfterStart(t *testing.T) {
	recorder := httptest.NewRecorder()
	a := anthropicStream{w: recorder, flusher: recorder, index: -1}
	original := json.RawMessage(`{"google":{"thought_signature":"FIRST"}}`)
	if err := a.consumeTool(0, "call-a", "lookup", "{", original); err != nil {
		t.Fatal(err)
	}
	if err := a.consumeTool(0, "", "", "}"); err != nil {
		t.Fatal(err)
	}
	if err := a.consumeTool(0, "", "", "", original); err != nil {
		t.Fatal("same metadata rejected", err)
	}
	if err := a.consumeTool(0, "", "", "", json.RawMessage(`{"google":{"thought_signature":"SECOND"}}`)); err == nil {
		t.Fatal("changed metadata silently lost")
	}
	if !strings.Contains(recorder.Body.String(), "FIRST") || strings.Contains(recorder.Body.String(), "SECOND") {
		t.Fatal(recorder.Body.String())
	}
}
