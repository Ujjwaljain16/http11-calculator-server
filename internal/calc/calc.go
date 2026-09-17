// Package calc implements the four arithmetic operations, with no
// knowledge of HTTP (methods, paths, headers, query parameters, status
// codes, or sockets) - just integers in, an integer or an error out.
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

func Div(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrDivideByZero
	}
	// Quo truncates toward zero, matching Go's native / operator - the one
	// case that can still overflow (math.MinInt64 / -1) is caught by
	// checked, the same as for the other three operations.
	return checked(new(big.Int).Quo(big.NewInt(a), big.NewInt(b)))
}

// checked computes every operation at arbitrary precision via math/big -
// which cannot itself overflow - then uses big.Int's own IsInt64 to decide
// whether the true mathematical result fits back into int64. This single
// check replaces hand-rolled overflow arithmetic for +, -, and *, and
// correctly covers the one edge case ad hoc checks tend to miss
// (math.MinInt64 * -1 or math.MinInt64 / -1, which silently wrap under
// Go's own two's-complement int64 rules but do not fit in int64).
func checked(result *big.Int) (int64, error) {
	if !result.IsInt64() {
		return 0, ErrOverflow
	}
	return result.Int64(), nil
}
