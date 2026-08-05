package idgen

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnowflake_NextIsMonotonicallyIncreasing(t *testing.T) {
	s, err := New(1)
	require.NoError(t, err)

	prev := int64(-1)
	for i := 0; i < 10_000; i++ {
		id, err := s.Next()
		require.NoError(t, err)
		require.Greater(t, id, prev, "id must be strictly increasing at i=%d", i)
		prev = id
	}
}

func TestSnowflake_UniqueUnderConcurrency(t *testing.T) {
	s, err := New(1)
	require.NoError(t, err)

	const workers = 8
	const perWorker = 5_000

	var mu sync.Mutex
	seen := make(map[int64]struct{}, workers*perWorker)

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			local := make([]int64, 0, perWorker)
			for i := 0; i < perWorker; i++ {
				id, err := s.Next()
				require.NoError(t, err)
				local = append(local, id)
			}
			mu.Lock()
			for _, id := range local {
				if _, dup := seen[id]; dup {
					t.Errorf("duplicate id: %d", id)
				}
				seen[id] = struct{}{}
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	assert.Len(t, seen, workers*perWorker)
}

func TestSnowflake_WorkerIDBounds(t *testing.T) {
	_, err := New(0)
	assert.Error(t, err)
	_, err = New(1024)
	assert.Error(t, err)
	_, err = New(1023)
	assert.NoError(t, err)
}

func TestSnowflake_ClockRegressionBeyondToleranceErrors(t *testing.T) {
	s, err := New(1)
	require.NoError(t, err)

	// Inject a fake clock: first call returns "now"; second call returns
	// "now - 1 minute", well past the 5 ms tolerance.
	fakeTimes := []time.Time{
		time.Unix(1_800_000_000, 0),
		time.Unix(1_800_000_000-60, 0),
	}
	i := 0
	s.now = func() time.Time {
		t := fakeTimes[i]
		if i < len(fakeTimes)-1 {
			i++
		}
		return t
	}

	_, err = s.Next()
	require.NoError(t, err)

	_, err = s.Next()
	assert.ErrorIs(t, err, ErrClockRegressed)
}
