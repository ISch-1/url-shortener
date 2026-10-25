package idgen

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBase62_RoundTrip_KnownValues(t *testing.T) {
	cases := []struct {
		n       int64
		encoded string
	}{
		{0, "0"},
		{61, "z"},
		{62, "10"},
		{123456789, "8M0kX"},
		{1 << 62, "5UfZOVH2ZO4"},
		{math.MaxInt64, "AzL8n0Y58m7"},
	}
	for _, c := range cases {
		got := EncodeBase62(c.n)
		assert.Equal(t, c.encoded, got, "encode %d", c.n)
		back, err := DecodeBase62(got)
		require.NoError(t, err)
		assert.Equal(t, c.n, back, "decode %s", got)
	}
}

func TestBase62_RoundTrip_Fuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 10_000; i++ {
		n := rng.Int63() // 0..MaxInt63
		enc := EncodeBase62(n)
		back, err := DecodeBase62(enc)
		require.NoError(t, err)
		assert.Equal(t, n, back, "round-trip failed for %d (encoded %q)", n, enc)
	}
}

func TestBase62_DecodeRejectsInvalid(t *testing.T) {
	_, err := DecodeBase62("")
	assert.Error(t, err)
	_, err = DecodeBase62("hello world") // contains space
	assert.Error(t, err)
	_, err = DecodeBase62("ábc") // non-ASCII
	assert.Error(t, err)
}

func TestBase62_DecodeRejectsOverflow(t *testing.T) {
	// MaxInt64 + 1: the final digit alone pushes it past the limit.
	_, err := DecodeBase62("AzL8n0Y58m8")
	assert.Error(t, err)
	_, err = DecodeBase62("zzzzzzzzzzzz")
	assert.Error(t, err)
}
