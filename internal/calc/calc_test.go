package calc_test

import (
	"errors"
	"math"
	"testing"

	"calcserver/internal/calc"
)

func TestArithmetic_Valid(t *testing.T) {
	cases := []struct {
		name string
		op   func(a, b int64) (int64, error)
		a, b int64
		want int64
	}{
		{"add", calc.Add, 2, 3, 5},
		{"sub", calc.Sub, 10, 4, 6},
		{"mul", calc.Mul, 6, 7, 42},
		{"div", calc.Div, 9, 3, 3},
		{"div_truncates_toward_zero", calc.Div, 7, 2, 3},
		{"negative_numbers", calc.Add, -5, -10, -15},
		{"mixed_sign", calc.Sub, -5, 10, -15},
		{"zero_operand", calc.Mul, 0, 12345, 0},
		{"zero_result", calc.Sub, 5, 5, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.op(c.a, c.b)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestDiv_ByZero(t *testing.T) {
	_, err := calc.Div(9, 0)
	if !errors.Is(err, calc.ErrDivideByZero) {
		t.Fatalf("expected ErrDivideByZero, got %v", err)
	}
}

func TestArithmetic_Overflow(t *testing.T) {
	cases := []struct {
		name string
		op   func(a, b int64) (int64, error)
		a, b int64
	}{
		{"addition_overflow", calc.Add, math.MaxInt64, 1},
		{"addition_underflow", calc.Add, math.MinInt64, -1},
		{"subtraction_overflow", calc.Sub, math.MaxInt64, -1},
		{"subtraction_underflow", calc.Sub, math.MinInt64, 1},
		{"multiplication_overflow", calc.Mul, math.MaxInt64, 2},
		{"multiplication_min_times_minus_one", calc.Mul, math.MinInt64, -1},
		{"division_min_by_minus_one", calc.Div, math.MinInt64, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.op(c.a, c.b)
			if !errors.Is(err, calc.ErrOverflow) {
				t.Fatalf("expected ErrOverflow, got %v", err)
			}
		})
	}
}

func TestArithmetic_BoundaryValuesDoNotFalselyOverflow(t *testing.T) {
	if _, err := calc.Add(math.MaxInt64, 0); err != nil {
		t.Errorf("MaxInt64 + 0 should not overflow: %v", err)
	}
	if _, err := calc.Sub(math.MinInt64, 0); err != nil {
		t.Errorf("MinInt64 - 0 should not overflow: %v", err)
	}
	if _, err := calc.Mul(math.MinInt64, 1); err != nil {
		t.Errorf("MinInt64 * 1 should not overflow: %v", err)
	}
}
