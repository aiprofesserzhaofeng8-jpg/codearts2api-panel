package usage

import (
	"errors"
	"fmt"
)

// PricingStore remains the only price transaction and historical snapshot writer.
type PricingStore interface {
	Prices() map[string]ModelPrice
	SetPriceAndSave(string, ModelPrice) error
}
type Management struct{ store PricingStore }

func NewManagement(store PricingStore) *Management  { return &Management{store: store} }
func (m *Management) Prices() map[string]ModelPrice { return m.store.Prices() }

var ErrPriceSave = errors.New("保存价格失败")

// SetPrice preserves validation order; persistence failures retain their cause.
// HTTP has already required the model and all three explicit unit prices.
func (m *Management) SetPrice(model string, price ModelPrice) error {
	if err := price.Validate(); err != nil {
		return err
	}
	if len(model) > 200 {
		return errors.New("模型 ID 过长")
	}
	if err := m.store.SetPriceAndSave(model, price); err != nil {
		return fmt.Errorf("%w: %w", ErrPriceSave, err)
	}
	return nil
}
