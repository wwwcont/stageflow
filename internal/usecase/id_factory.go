package usecase

import (
	"fmt"
	"sync/atomic"
	"time"

	"stageflow/internal/domain"
)

// MonotonicRunIDFactory выдаёт детерминированные локальные идентификаторы до появления надёжного генератора ID.
type MonotonicRunIDFactory struct {
	counter atomic.Uint64
}

func NewMonotonicRunIDFactory(seed uint64) *MonotonicRunIDFactory {
	factory := &MonotonicRunIDFactory{}
	factory.counter.Store(seed)
	return factory
}

func (f *MonotonicRunIDFactory) NewRunID() domain.RunID {
	seq := f.counter.Add(1)
	return domain.RunID(fmt.Sprintf("run-%d-%06d", time.Now().UTC().Unix(), seq))
}
