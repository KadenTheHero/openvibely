package events

import (
	"sync"

	"github.com/openvibely/openvibely/internal/applog"
)

type ExecutionStreamEventType string

const (
	ExecutionStreamDelta ExecutionStreamEventType = "delta"
	ExecutionStreamDone  ExecutionStreamEventType = "done"
	ExecutionStreamError ExecutionStreamEventType = "error"
)

type ExecutionStreamEvent struct {
	ExecID string
	Type   ExecutionStreamEventType
	Delta  string
	Offset int
	Status string
	Error  string
}

type ExecutionStreamSubscriber chan ExecutionStreamEvent

type executionStreamSubGuard struct {
	mu     sync.Mutex
	closed bool
}

type ExecutionStreamHub struct {
	mu              sync.RWMutex
	subs            map[string]map[ExecutionStreamSubscriber]*executionStreamSubGuard
	subscriberCount int
}

func NewExecutionStreamHub() *ExecutionStreamHub {
	return &ExecutionStreamHub{subs: make(map[string]map[ExecutionStreamSubscriber]*executionStreamSubGuard)}
}

func (h *ExecutionStreamHub) Subscribe(execID string) (ExecutionStreamSubscriber, func(), error) {
	if h == nil {
		return nil, func() {}, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subscriberCount >= MaxSubscribers {
		return nil, nil, ErrMaxSubscribers
	}
	sub := make(ExecutionStreamSubscriber, 128)
	if h.subs[execID] == nil {
		h.subs[execID] = make(map[ExecutionStreamSubscriber]*executionStreamSubGuard)
	}
	h.subs[execID][sub] = &executionStreamSubGuard{}
	h.subscriberCount++
	unsubscribe := func() { h.Unsubscribe(execID, sub) }
	return sub, unsubscribe, nil
}

func (h *ExecutionStreamHub) Unsubscribe(execID string, sub ExecutionStreamSubscriber) {
	if h == nil || sub == nil {
		return
	}
	h.mu.Lock()
	var guard *executionStreamSubGuard
	if byExec := h.subs[execID]; byExec != nil {
		guard = byExec[sub]
		if guard != nil {
			delete(byExec, sub)
			h.subscriberCount--
			if len(byExec) == 0 {
				delete(h.subs, execID)
			}
		}
	}
	h.mu.Unlock()
	if guard != nil {
		guard.mu.Lock()
		if !guard.closed {
			guard.closed = true
			close(sub)
		}
		guard.mu.Unlock()
	}
}

func (h *ExecutionStreamHub) Publish(event ExecutionStreamEvent) {
	if h == nil || event.ExecID == "" {
		return
	}
	h.mu.RLock()
	type entry struct {
		ch    ExecutionStreamSubscriber
		guard *executionStreamSubGuard
	}
	subs := make([]entry, 0, len(h.subs[event.ExecID]))
	for sub, guard := range h.subs[event.ExecID] {
		subs = append(subs, entry{sub, guard})
	}
	h.mu.RUnlock()
	for _, e := range subs {
		e.guard.mu.Lock()
		if !e.guard.closed {
			select {
			case e.ch <- event:
			default:
				applog.Debugf("[events] execution stream subscriber slow exec=%s type=%s offset=%d", event.ExecID, event.Type, event.Offset)
			}
		}
		e.guard.mu.Unlock()
	}
}

func (h *ExecutionStreamHub) Close(execID string, event ExecutionStreamEvent) {
	if h == nil || execID == "" {
		return
	}
	if event.ExecID == "" {
		event.ExecID = execID
	}
	h.mu.Lock()
	byExec := h.subs[execID]
	delete(h.subs, execID)
	h.subscriberCount -= len(byExec)
	if h.subscriberCount < 0 {
		h.subscriberCount = 0
	}
	h.mu.Unlock()
	for sub, guard := range byExec {
		guard.mu.Lock()
		if !guard.closed {
			select {
			case sub <- event:
			default:
				applog.Debugf("[events] execution stream terminal dropped for slow subscriber exec=%s type=%s", execID, event.Type)
			}
			guard.closed = true
			close(sub)
		}
		guard.mu.Unlock()
	}
}

func (h *ExecutionStreamHub) SubscriberCount() int {
	if h == nil {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.subscriberCount
}
