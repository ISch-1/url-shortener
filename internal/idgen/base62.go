package idgen

import (
	"errors"
	"strings"
)

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var base62Index = func() [128]int8 {
	var idx [128]int8
	for i := range idx {
		idx[i] = -1
	}
	for i, c := range base62Alphabet {
		idx[c] = int8(i)
	}
	return idx
}()

// EncodeBase62 encodes a non-negative int64 to a base62 string using the
// alphabet 0-9A-Za-z. Zero encodes to "0".
func EncodeBase62(n int64) string {
	if n < 0 {
		// Snowflake IDs are always non-negative — sign bit is unused.
		panic("EncodeBase62 requires n >= 0")
	}
	if n == 0 {
		return "0"
	}
	var b strings.Builder
	b.Grow(11)
	for n > 0 {
		b.WriteByte(base62Alphabet[n%62])
		n /= 62
	}
	// Reverse in place.
	s := b.String()
	rs := []byte(s)
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return string(rs)
}

// DecodeBase62 is the inverse. Returns an error on any character outside the
// alphabet or on overflow of int64.
func DecodeBase62(s string) (int64, error) {
	if s == "" {
		return 0, errors.New("empty string")
	}
	var n int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 128 || base62Index[c] < 0 {
			return 0, errors.New("invalid base62 character")
		}
		// Overflow check: n*62 must not overflow int64.
		if n > (1<<62)/62 {
			return 0, errors.New("base62 overflow")
		}
		n = n*62 + int64(base62Index[c])
	}
	return n, nil
}
