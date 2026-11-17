package storage

import (
	"codearts2api/internal/config"
	"reflect"
)

// Compare only prompt-owned settings: another page may save unrelated options
// while this editor is open. Serialize the comparison and write under Store.mu.
func (s *Store) SavePromptConfiguration(expected, next config.PromptConfiguration) error {
	if err := next.Validate(); err != nil {
		return config.ModelFailure(400, "invalid_prompt_configuration", err.Error())
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !reflect.DeepEqual(s.st.Settings.PromptConfiguration(), expected) {
		return config.ModelFailure(409, "prompt_configuration_changed", "提示词已在其他位置修改，请重新读取并核对草稿")
	}
	before := s.st.Settings
	next.Apply(&s.st.Settings)
	if err := s.saveGroupStateLocked(); err != nil {
		s.st.Settings = before
		return config.ModelFailure(500, "prompt_save_failed", "提示词未保存，请检查状态目录权限后重试")
	}
	return nil
}
