package runevents

import (
	"sync"

	"stageflow/internal/domain"
)

type Broker struct {
	mu      sync.RWMutex
	nextID  int
	subject map[domain.RunID]map[int]chan domain.RunEvent
}

func NewBroker() *Broker {
	return &Broker{subject: make(map[domain.RunID]map[int]chan domain.RunEvent)}
}

func (b *Broker) Subscribe(runID domain.RunID, buffer int) (<-chan domain.RunEvent, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	ch := make(chan domain.RunEvent, buffer)
	if b.subject[runID] == nil {
		b.subject[runID] = make(map[int]chan domain.RunEvent)
	}
	b.subject[runID][id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subject[runID]
		if subs == nil {
			return
		}
		if existing, ok := subs[id]; ok {
			delete(subs, id)
			close(existing)
		}
		if len(subs) == 0 {
			delete(b.subject, runID)
		}
	}
}

func (b *Broker) Publish(event domain.RunEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subject[event.RunID] {
		select {
		case ch <- event:
		default:
		}
	}
}
