package natsutil

import (
	"errors"
	"math/rand"
	"sync"
)

type Handler func(msg []byte)

type Subscription interface {
	Unsubscribe() error
}

type Bus interface {
	Publish(subject string, msg []byte) error
	Subscribe(subject string, handler Handler) (Subscription, error)
}

type memoryBus struct {
	mu   sync.RWMutex
	subs map[string]map[int]Handler
}

type memorySub struct {
	parent  *memoryBus
	subject string
	id      int
}

func NewInMemoryBus() Bus {
	return &memoryBus{subs: make(map[string]map[int]Handler)}
}

func (b *memoryBus) Publish(subject string, msg []byte) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	handlers := b.subs[subject]
	for _, h := range handlers {
		copyMsg := append([]byte(nil), msg...)
		go h(copyMsg)
	}
	return nil
}

func (b *memoryBus) Subscribe(subject string, handler Handler) (Subscription, error) {
	if handler == nil {
		return nil, errors.New("handler is nil")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[subject]; !ok {
		b.subs[subject] = make(map[int]Handler)
	}
	id := rand.Int()
	b.subs[subject][id] = handler
	return &memorySub{parent: b, subject: subject, id: id}, nil
}

func (s *memorySub) Unsubscribe() error {
	s.parent.mu.Lock()
	defer s.parent.mu.Unlock()
	if subs, ok := s.parent.subs[s.subject]; ok {
		delete(subs, s.id)
	}
	return nil
}
