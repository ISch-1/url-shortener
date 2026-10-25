// Package idgen: Snowflake-style monotonic 64-bit ids + base62 codec.
//
// Layout (bits, high → low):
//
//	1  bit  sign (always 0)
//	41 bits milliseconds since custom epoch (2020-01-01 UTC) — ~69 years
//	10 bits worker id (0..1023)
//	12 bits sequence within a millisecond (0..4095)
//
// One generator per process (per worker id). Safe for concurrent callers.
package idgen

import (
	"errors"
	"sync"
	"time"
)

const (
	epochMillis = int64(1577836800000) // 2020-01-01T00:00:00Z

	workerBits = 10
	seqBits    = 12

	maxWorker = (1 << workerBits) - 1 // 1023
	maxSeq    = (1 << seqBits) - 1    // 4095

	timeShift   = workerBits + seqBits // 22
	workerShift = seqBits              // 12
)

// ErrClockRegressed is returned when the wall clock jumps backwards by more
// than the small skew we tolerate. Emitting an id in that state risks
// duplication; the caller should retry after the clock catches up.
var ErrClockRegressed = errors.New("clock regressed beyond tolerance")

// Snowflake is the id generator. Construct with New.
type Snowflake struct {
	mu       sync.Mutex
	workerID int64
	lastMs   int64
	seq      int64
	now      func() time.Time // injectable for tests
}

// New constructs a generator for the given worker id. workerID must be in 1..1023
// (0 is reserved so an unset env var fails loudly).
func New(workerID int64) (*Snowflake, error) {
	if workerID < 1 || workerID > maxWorker {
		return nil, errors.New("workerID must be in 1..1023")
	}
	return &Snowflake{workerID: workerID, now: time.Now}, nil
}

// Next returns the next id. Returns ErrClockRegressed if the wall clock jumped
// backwards; callers may sleep and retry.
func (s *Snowflake) Next() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	nowMs := s.now().UnixMilli()
	if nowMs < s.lastMs {
		// Small skew (< 5 ms) is common on NTP-corrected clocks — spin.
		if s.lastMs-nowMs <= 5 {
			for nowMs < s.lastMs {
				nowMs = s.now().UnixMilli()
			}
		} else {
			return 0, ErrClockRegressed
		}
	}

	if nowMs == s.lastMs {
		s.seq = (s.seq + 1) & maxSeq
		if s.seq == 0 {
			// Sequence exhausted this millisecond — wait for the next tick.
			for nowMs <= s.lastMs {
				nowMs = s.now().UnixMilli()
			}
		}
	} else {
		s.seq = 0
	}
	s.lastMs = nowMs

	id := ((nowMs - epochMillis) << timeShift) |
		(s.workerID << workerShift) |
		s.seq
	return id, nil
}
