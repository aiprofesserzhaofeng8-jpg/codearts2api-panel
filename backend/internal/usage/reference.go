package usage

import (
	_ "embed"
	"encoding/json"
)

//go:embed reference/prices.json
var referenceJSON []byte

var references = func() (data struct {
	Revision string
	Prices   map[string]struct{ Input, Output, Cached float64 }
}) {
	if err := json.Unmarshal(referenceJSON, &data); err != nil {
		panic(err)
	}
	return
}()

func ReferencePrice(model string) (ModelPrice, bool) {
	if p, ok := DefaultPrices()[model]; ok {
		return p, true
	}
	r, ok := references.Prices[model]
	if !ok {
		return ModelPrice{}, false
	}
	return ModelPrice{Currency: "USD", Input: r.Input, Output: r.Output, Cached: r.Cached, Source: "LiteLLM · " + references.Revision[:8]}, true
}
