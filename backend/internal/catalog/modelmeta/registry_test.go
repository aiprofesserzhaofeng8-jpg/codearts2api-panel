// SPDX-License-Identifier: LGPL-3.0-only
package modelmeta

import (
	"strings"
	"testing"
)

func TestUpstreamMergePreservesExplicitValuesAndCopiesFallback(t *testing.T) {
	no, yes := false, true
	primary := UpstreamModelMetadata{ID: "source-id", ContextWindow: 16384, Reasoning: &no, InputModalities: []string{"text"}}
	fallback := UpstreamModelMetadata{ID: "native-id", ContextWindow: 1000000, MaxContextWindow: 1000000, MaxOutputTokens: 4096, Reasoning: &yes, InputModalities: []string{"text", "image"}, SupportedReasoningLevels: []string{"high"}}
	got := Merge(primary, fallback)
	if got.ID != "source-id" || got.ContextWindow != 16384 || got.MaxContextWindow != 0 || got.MaxOutputTokens != 4096 || *got.Reasoning || len(got.InputModalities) != 1 {
		t.Fatal("fallback overwrote explicit upstream metadata", got)
	}
	got.SupportedReasoningLevels[0] = "low"
	if fallback.SupportedReasoningLevels[0] != "high" || primary.MaxOutputTokens != 0 {
		t.Fatal("merge mutated its inputs")
	}
}

func TestRegistryUsesExactIDsAndExplicitProviders(t *testing.T) {
	registry, err := Parse([]byte(`{"native":{"models":{"glm-5.3":{"id":"glm-5.3","limit":{"context":131072,"output":16384},"modalities":{"input":["Text","image","audio","IMAGE"]},"reasoning_options":[{"type":"effort","values":[null,"high","EXTRA_HIGH","bad"]}]}}},"other":{"models":{"auto":{"limit":{"context":2000000}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := registry.Lookup("GLM-5.3", []string{"native"})
	if !ok || got.ContextWindow != 131072 || got.MaxOutputTokens != 16384 || len(got.InputModalities) != 2 || got.Reasoning == nil || !*got.Reasoning || strings.Join(got.SupportedReasoningLevels, ",") != "none,high,xhigh" {
		t.Fatal(got)
	}
	for _, id := range []string{"glm", "glm-5.3-latest", "auto"} {
		if _, ok := registry.Lookup(id, []string{"native"}); ok {
			t.Fatal("guessed model identity or provider", id)
		}
	}
	for _, body := range []string{"", "null", "{}", "[]", `{"x":{"models":{}}}`} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Fatal("accepted empty/invalid registry", body)
		}
	}
}
