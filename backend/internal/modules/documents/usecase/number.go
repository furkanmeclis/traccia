package usecase

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"strconv"
)

// LayoutNumber rejects precision loss rather than silently changing a canonical
// JSON value. Decimal lexeme spelling is not part of document identity.
type LayoutNumber float64

func (n *LayoutNumber) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	value, e := strconv.ParseFloat(string(raw), 64)
	if e != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("invalid layout number")
	}
	original, ok := new(big.Rat).SetString(string(raw))
	if !ok {
		return fmt.Errorf("invalid layout number")
	}
	encoded := strconv.FormatFloat(value, 'g', -1, 64)
	canonical, ok := new(big.Rat).SetString(encoded)
	if !ok || original.Cmp(canonical) != 0 {
		return fmt.Errorf("unsupported layout precision")
	}
	*n = LayoutNumber(value)
	return nil
}
func (n LayoutNumber) MarshalJSON() ([]byte, error) {
	value := float64(n)
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, fmt.Errorf("invalid layout number")
	}
	return strconv.AppendFloat(nil, value, 'g', -1, 64), nil
}
