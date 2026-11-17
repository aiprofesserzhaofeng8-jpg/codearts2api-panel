package accounts

import (
	"codearts2api/internal/provider"
	"encoding/json"
)

type protocolStore interface {
	CommitAccountConfiguration(string, json.RawMessage, json.RawMessage) error
}

func (s *Management) SetProtocol(id string, p provider.Protocol, adapter provider.ProtocolSettings) error {
	a := s.store.Account(id)
	if a == nil {
		return &ManagementError{ManagementMissing, "账号不存在"}
	}
	updated, err := adapter.SetProtocol(a.Credentials, p)
	if err != nil {
		return &ManagementError{ManagementInvalid, err.Error()}
	}
	st, ok := s.store.(protocolStore)
	if !ok {
		return &ManagementError{ManagementInternal, "账号配置存储不可用"}
	}
	if err = st.CommitAccountConfiguration(id, a.Credentials, updated); err != nil {
		return &ManagementError{ManagementInternal, "协议未保存：" + err.Error()}
	}
	s.invalidate(id)
	return nil
}
