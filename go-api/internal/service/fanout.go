package service

import (
	"context"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/config"
)

// TenantFanout manages fan-out of Redis Stream events to multiple WebSocket subscribers.
// A single XREAD loop feeds all subscriber channels, reducing Redis connections under load.
// TenantFanout delivers Redis pub/sub messages to WebSocket subscribers.
type TenantFanout struct {
	rdb      *goredis.Client
	cfg      *config.Config
	tenantID uuid.UUID

	mu        sync.RWMutex
	subs      map[int]chan StreamEvent
	nextSubID int
	cancel    context.CancelFunc
}

func newTenantFanout(rdb *goredis.Client, cfg *config.Config, tenantID uuid.UUID) *TenantFanout {
	return &TenantFanout{
		rdb:      rdb,
		cfg:      cfg,
		tenantID: tenantID,
		subs:     make(map[int]chan StreamEvent),
	}
}

// Subscribe registers a new subscriber and returns its channel and an unsubscribe function.
// The background XREAD goroutine is started lazily on first subscriber.
func (f *TenantFanout) Subscribe() (<-chan StreamEvent, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()

	subID := f.nextSubID
	f.nextSubID++
	ch := make(chan StreamEvent, 100)
	f.subs[subID] = ch

	// Start reader goroutine on first subscriber
	if len(f.subs) == 1 {
		ctx, cancel := context.WithCancel(context.Background())
		f.cancel = cancel
		go f.readerLoop(ctx)
		slog.Info("Started shared XREAD for tenant", "tenant_id", f.tenantID)
	}

	unsub := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(f.subs, subID)
		close(ch)

		// Stop reader goroutine when last subscriber leaves
		if len(f.subs) == 0 && f.cancel != nil {
			f.cancel()
			f.cancel = nil
			slog.Info("Stopped shared XREAD for tenant (no subscribers)", "tenant_id", f.tenantID)
		}
	}
	return ch, unsub
}

// readerLoop reads from Redis Stream and fans out to all subscriber channels.
// Drop-on-full: disconnect slow subscribers when their buffer is full.
func (f *TenantFanout) readerLoop(ctx context.Context) {
	events := StreamTenantEvents(ctx, f.rdb, f.cfg, f.tenantID, "$")
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			f.fanOut(event)
		}
	}
}

func (f *TenantFanout) fanOut(event StreamEvent) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var dead []int
	for id, ch := range f.subs {
		select {
		case ch <- event:
		default:
			// Subscriber queue full — disconnect it
			slog.Warn("Subscriber queue full, dropping", "tenant_id", f.tenantID, "sub_id", id)
			dead = append(dead, id)
		}
	}

	// Promote to write lock to remove dead subscribers
	if len(dead) > 0 {
		f.mu.RUnlock()
		f.mu.Lock()
		for _, id := range dead {
			if ch, ok := f.subs[id]; ok {
				close(ch)
				delete(f.subs, id)
			}
		}
		f.mu.Unlock()
		f.mu.RLock()
	}
}

// FanoutManager maintains one TenantFanout per active tenant.
// FanoutManager holds per-tenant fan-out goroutines.
type FanoutManager struct {
	mu      sync.Mutex
	fanouts map[uuid.UUID]*TenantFanout
}

// NewFanoutManager creates the global fan-out manager.
func NewFanoutManager() *FanoutManager {
	return &FanoutManager{
		fanouts: make(map[uuid.UUID]*TenantFanout),
	}
}

// GetFanout returns the TenantFanout for the given tenant, creating one if needed.
func (m *FanoutManager) GetFanout(rdb *goredis.Client, cfg *config.Config, tenantID uuid.UUID) *TenantFanout {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.fanouts[tenantID]; ok {
		return f
	}
	f := newTenantFanout(rdb, cfg, tenantID)
	m.fanouts[tenantID] = f
	return f
}
