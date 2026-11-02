package codearts

import "codearts2api/internal/accounts"

// Account is a source compatibility alias. New shared code uses accounts.Account.
// Deprecated: use accounts.Account.
type Account = accounts.Account

// StoredSession returns the immutable CodeArts generation without parsing JSON.
// The storage compatibility codec reconstructs this pointer when loading state.
func StoredSession(a *accounts.Account) *Session {
	if a == nil {
		return nil
	}
	session, _ := a.NativeCredential.(*Session)
	return session
}
