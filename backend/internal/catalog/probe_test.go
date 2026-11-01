package catalog

import "testing"

// TestDedupeModels 校验去空白、去空值、去重。
//
// 重复 ID 会变成两次真实上游请求（真实扣积分），调用方没有任何理由需要重复探测。
func TestDedupeModels(t *testing.T) {
	got := dedupeModels([]string{" glm-5.3 ", "", "glm-5.3", "GLM-5.2", "  ", "GLM-5.2"})
	want := []string{"glm-5.3", "GLM-5.2"}
	if len(got) != len(want) {
		t.Fatalf("dedupeModels = %v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 项 = %q，期望 %q", i, got[i], want[i])
		}
	}
	if empty := dedupeModels(nil); len(empty) != 0 {
		t.Errorf("nil 输入应返回空切片，实际 %v", empty)
	}
}
