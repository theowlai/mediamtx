package stream

import (
	"sync"
	"time"

	"github.com/bluenviron/mediamtx/internal/ntpestimator"
)

// sharedNTPEstimator is a NTP estimator shared by all formats of a stream.
// It works with time.Duration timestamps, since formats have different clock rates.
type sharedNTPEstimator struct {
	mutex     sync.Mutex
	estimator *ntpestimator.Estimator
}

func (e *sharedNTPEstimator) initialize() {
	e.estimator = &ntpestimator.Estimator{
		ClockRate: int(time.Second),
	}
}

func (e *sharedNTPEstimator) estimate(pts time.Duration) time.Time {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.estimator.Estimate(int64(pts))
}
