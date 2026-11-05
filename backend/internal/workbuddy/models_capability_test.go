package workbuddy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelImageCapabilityUsesUpstreamFlag(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":0,"data":{"models":[{"id":"vision","supportsImages":true},{"id":"text","supportsImages":false},{"id":"unknown"}]}}`)
	}))
	defer up.Close()
	c := New()
	c.CNBase = up.URL
	models, err := c.Models(context.Background(), []byte(`{"realm":"cn"}`))
	if err != nil || len(models) != 3 {
		t.Fatal(models, err)
	}
	if models[0].ImageInput != "supported" || models[1].ImageInput != "unsupported" || models[2].ImageInput != "" {
		t.Fatal("lost or guessed upstream capabilities", models)
	}
}
