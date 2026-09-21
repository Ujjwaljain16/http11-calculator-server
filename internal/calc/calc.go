// Package calc implements integer arithmetic that reports overflow and
// division by zero as errors.
package calc

import (
	"errors"
	"math/big"
)

var (
	ErrDivideByZero = errors.New("division by zero")
	ErrOverflow     = errors.New("arithmetic overflow")
)

func Add(a, b int64) (int64, error) {
	return checked(new(big.Int).Add(big.NewInt(a), big.NewInt(b)))
}

func Sub(a, b int64) (int64, error) {
	return checked(new(big.Int).Sub(big.NewInt(a), big.NewInt(b)))
}

func Mul(a, b int64) (int64, error) {
	return checked(new(big.Int).Mul(big.NewInt(a), big.NewInt(b)))
}

// Div returns a / b truncated toward zero.
func Div(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrDivideByZero
	}
	return checked(new(big.Int).Quo(big.NewInt(a), big.NewInt(b)))
}

// checked converts an exact result back to int64, or returns ErrOverflow if
// it does not fit. Computing with big.Int first avoids wrap-around in every
// case, including math.MinInt64 * -1 and math.MinInt64 / -1.
func checked(result *big.Int) (int64, error) {
	if !result.IsInt64() {
		return 0, ErrOverflow
	}
	return result.Int64(), nil
}
