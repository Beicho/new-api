package dto

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

// BoolOrInt preserves the two wire types of logprobs: a boolean for chat,
// an integer for legacy completions. A nil pointer represents an absent field.
type BoolOrInt struct {
	Bool *bool
	Int  *int
}

func (v *BoolOrInt) UnmarshalJSON(data []byte) error {
	*v = BoolOrInt{}
	switch common.GetJsonType(data) {
	case "boolean":
		return common.Unmarshal(data, &v.Bool)
	case "number":
		return common.Unmarshal(data, &v.Int)
	default:
		return fmt.Errorf("logprobs must be a boolean or integer")
	}
}

func (v BoolOrInt) MarshalJSON() ([]byte, error) {
	if v.Bool != nil && v.Int == nil {
		return common.Marshal(*v.Bool)
	}
	if v.Int != nil && v.Bool == nil {
		return common.Marshal(*v.Int)
	}
	return nil, fmt.Errorf("logprobs must contain exactly one boolean or integer")
}
