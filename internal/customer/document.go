package customer

import (
	"strings"

	"github.com/andreccls/go-customers-api/internal/validation"
)

// NormalizeDocument strips punctuation from a CPF or CNPJ and validates its
// check digits. It returns the digits-only document.
func NormalizeDocument(s string) (string, bool) {
	d := validation.Digits(s)
	// Only separators are tolerated around the digits; letters are rejected.
	if strings.Trim(s, "0123456789.-/ ") != "" {
		return d, false
	}
	switch len(d) {
	case 11:
		return d, validCPF(d)
	case 14:
		return d, validCNPJ(d)
	}
	return d, false
}

func validCPF(d string) bool {
	if allSame(d) {
		return false
	}
	return checkDigit(d[:9], 10) == int(d[9]-'0') && checkDigit(d[:10], 11) == int(d[10]-'0')
}

func validCNPJ(d string) bool {
	if allSame(d) {
		return false
	}
	w1 := []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	w2 := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	return weighted(d[:12], w1) == int(d[12]-'0') && weighted(d[:13], w2) == int(d[13]-'0')
}

// checkDigit computes a CPF check digit: weights start at `start` and decrease.
func checkDigit(base string, start int) int {
	sum := 0
	for i := 0; i < len(base); i++ {
		sum += int(base[i]-'0') * (start - i)
	}
	return mod11(sum)
}

func weighted(base string, weights []int) int {
	sum := 0
	for i := 0; i < len(base); i++ {
		sum += int(base[i]-'0') * weights[i]
	}
	return mod11(sum)
}

func mod11(sum int) int {
	if r := sum % 11; r >= 2 {
		return 11 - r
	}
	return 0
}

func allSame(d string) bool { return strings.Count(d, d[:1]) == len(d) }
