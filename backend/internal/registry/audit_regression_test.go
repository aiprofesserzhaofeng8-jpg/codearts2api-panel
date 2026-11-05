package registry

import (
	"codearts2api/internal/codearts"
	"testing"
)

func TestAuditNativeCatalogRetainsImageCapability(t *testing.T) {
	models := FromBuiltin([]codearts.BuiltinModel{{ModelID: "vision", Enable: true, SupportsImages: true}})
	if len(models) != 1 || models[0].ImageInput != "supported" {
		t.Fatal("native image capability discarded")
	}
}
