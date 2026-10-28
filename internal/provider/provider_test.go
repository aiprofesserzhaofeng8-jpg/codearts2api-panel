package provider

import "testing"

func TestExplicitNamespacesPreserveUpstreamBoundary(t *testing.T) {
	for _, tc := range []struct{ id, p, s, m string }{{"glm-5.3", "codearts", "", "glm-5.3"}, {"codearts/glm-5.3", "codearts", "", "glm-5.3"}, {"workbuddy/cn/glm-5.3", "workbuddy", "cn", "glm-5.3"}, {"workbuddy/global/glm-5.3", "workbuddy", "global", "glm-5.3"}, {"trae/cn/glm-5.3", "trae", "cn", "glm-5.3"}} {
		p, s, m := ParseModel(tc.id)
		if p != tc.p || s != tc.s || m != tc.m {
			t.Fatal("provider namespace was lost", tc.id)
		}
	}
}
