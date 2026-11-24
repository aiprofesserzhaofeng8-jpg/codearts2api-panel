package modelmeta

import _ "embed"

//go:embed data/models.dev.json
var snapshotJSON []byte

// Bundled is a numeric reference for first-time offline discovery. Persisted
// references and fresh online data take precedence in the catalog caller.
var Bundled = func() *Registry {
	r, err := Parse(snapshotJSON)
	if err != nil {
		panic("invalid bundled model reference: " + err.Error())
	}
	return r
}()
