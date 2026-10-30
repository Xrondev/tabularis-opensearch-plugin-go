package pagination

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"tabularis-opensearch-plugin-go/internal/connectioncache"
	"tabularis-opensearch-plugin-go/internal/opensearch"
	"tabularis-opensearch-plugin-go/internal/query"
)

const (
	maxSessions     = 32
	maxCheckpoints  = 32
	idleTTL         = time.Minute
	cleanupTimeout  = 5 * time.Second
	shutdownTimeout = 9 * time.Second
)

type Manager struct {
	mu          sync.Mutex
	sessions    map[string]*session
	onError     func(error)
	done        chan struct{}
	stop        sync.Once
	wait        sync.WaitGroup
	closed      bool
	closeDone   chan struct{}
	closeErr    error
	identityKey [32]byte
}

type session struct {
	gate            chan struct{}
	owner           string
	generation      [32]byte
	fingerprint     [32]byte
	snapshot        *opensearch.Snapshot
	lease           *connectioncache.Lease
	checkpoints     map[int][]json.RawMessage
	lastUsed        time.Time
	closed          bool
	backendVerified bool
}

func New(onCleanupError func(error)) (*Manager, error) {
	if onCleanupError == nil {
		return nil, errors.New("cleanup error handler is required")
	}
	m := &Manager{sessions: make(map[string]*session), onError: onCleanupError, done: make(chan struct{}), closeDone: make(chan struct{})}
	if _, err := rand.Read(m.identityKey[:]); err != nil {
		return nil, errors.New("unable to initialize pagination identity")
	}
	m.wait.Add(1)
	go m.sweep()
	return m, nil
}

func (m *Manager) Page(ctx context.Context, lease *connectioncache.Lease, owner string, sessionID string, operation query.Request, page int, verifiedUniqueSort bool) (json.RawMessage, error) {
	return m.page(ctx, lease, owner, sessionID, operation, page, verifiedUniqueSort, false, "")
}

func (m *Manager) VerifiedPage(ctx context.Context, lease *connectioncache.Lease, owner string, sessionID string, operation query.Request, page int, queryIdentity string) (json.RawMessage, error) {
	if queryIdentity == "" {
		return nil, errors.New("PIT pagination requires a query identity")
	}
	return m.page(ctx, lease, owner, sessionID, operation, page, false, true, queryIdentity)
}

func (m *Manager) page(ctx context.Context, lease *connectioncache.Lease, owner string, sessionID string, operation query.Request, page int, verifiedUniqueSort, backendVerified bool, queryIdentity string) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if lease == nil || owner == "" || sessionID == "" || page < 1 {
		return nil, errors.New("PIT pagination requires a connection generation, session, and positive page")
	}
	generation := lease.Generation()
	if generation == [32]byte{} {
		return nil, errors.New("PIT pagination requires a saved connection")
	}
	if err := operation.Validate(); err != nil {
		return nil, err
	}
	var body struct {
		Size int `json:"size"`
	}
	if err := json.Unmarshal(operation.Body(), &body); err != nil || body.Size < 2 || body.Size > 1001 {
		return nil, errors.New("PIT pagination requires a bounded positive page size")
	}
	mac := hmac.New(sha256.New, m.identityKey[:])
	_, _ = mac.Write([]byte(operation.Index()))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(operation.Body())
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(queryIdentity))
	var fingerprint [32]byte
	copy(fingerprint[:], mac.Sum(nil))
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errors.New("pagination manager is closed")
	}
	item := m.sessions[sessionID]
	if item != nil && item.owner != owner {
		m.mu.Unlock()
		return nil, errors.New("session belongs to a different connection")
	}
	if item != nil && item.generation != generation {
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return nil, errors.Join(errors.New("connection changed; restart from page 1"), closeSession(item))
	}
	if item != nil && page != 1 && item.backendVerified != backendVerified {
		m.mu.Unlock()
		return nil, errors.New("pagination session verification mode changed")
	}
	if page == 1 {
		if item == nil && len(m.sessions) >= maxSessions {
			m.mu.Unlock()
			return nil, errors.New("pagination session limit reached")
		}
		old := item
		item = &session{gate: make(chan struct{}, 1), owner: owner, generation: generation, fingerprint: fingerprint, checkpoints: map[int][]json.RawMessage{1: nil}, lastUsed: time.Now(), backendVerified: backendVerified}
		m.sessions[sessionID] = item
		m.mu.Unlock()
		defer m.finish(sessionID, item)
		if old != nil {
			if err := closeSession(old); err != nil {
				item.closed = true
				return nil, err
			}
		}
		retained, err := lease.Retain()
		if err != nil {
			item.closed = true
			return nil, err
		}
		item.lease = retained
		if backendVerified {
			_, item.snapshot, err = retained.Client().OpenSingleShardPIT(ctx, operation)
		} else {
			item.snapshot, err = retained.Client().OpenPIT(ctx, operation, verifiedUniqueSort)
		}
		if err != nil {
			return nil, errors.Join(err, closeHeld(item))
		}
	} else {
		m.mu.Unlock()
		if item == nil {
			return nil, errors.New("pagination session is unavailable; restart from page 1")
		}
		if err := acquire(ctx, item); err != nil {
			return nil, err
		}
		defer m.finish(sessionID, item)
	}
	if !m.current(sessionID, item) || item.closed {
		return nil, errors.Join(errors.New("pagination session is no longer active"), closeHeld(item))
	}
	if time.Since(item.lastUsed) >= idleTTL {
		return nil, errors.Join(errors.New("PIT pagination expired; restart from page 1"), closeHeld(item))
	}
	if item.fingerprint != fingerprint {
		return nil, errors.Join(errors.New("query changed; restart from page 1"), closeHeld(item))
	}
	after, ok := item.checkpoints[page]
	if !ok {
		return nil, errors.New("page checkpoint unavailable; restart or navigate sequentially")
	}
	raw, err := item.snapshot.Search(ctx, after)
	if err != nil {
		return nil, errors.Join(err, closeHeld(item))
	}
	if !m.current(sessionID, item) {
		return nil, errors.Join(errors.New("pagination session is no longer active"), closeHeld(item))
	}
	var result struct {
		TimedOut bool `json:"timed_out"`
		Shards   struct {
			Failed int `json:"failed"`
		} `json:"_shards"`
		Hits struct {
			Hits []struct {
				Sort []json.RawMessage `json:"sort"`
			} `json:"hits"`
		} `json:"hits"`
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil || json.Unmarshal(raw, &fields) != nil || fields["hits"] == nil || fields["_shards"] == nil || fields["timed_out"] == nil || result.TimedOut || result.Shards.Failed != 0 {
		return nil, errors.Join(errors.New("PIT search returned invalid or incomplete results"), closeHeld(item))
	}
	for i, hit := range result.Hits.Hits {
		if len(hit.Sort) == 0 {
			return nil, errors.Join(errors.New("PIT search returned a hit without sort values"), closeHeld(item))
		}
		if i > 0 && sameSort(hit.Sort, result.Hits.Hits[i-1].Sort) {
			return nil, errors.Join(errors.New("PIT search returned non-unique sort values"), closeHeld(item))
		}
	}
	size := body.Size - 1
	if len(result.Hits.Hits) > size {
		item.checkpoints[page+1] = result.Hits.Hits[size-1].Sort
		if len(item.checkpoints) > maxCheckpoints {
			oldest := page + 1
			for number := range item.checkpoints {
				if number != 1 && number < oldest {
					oldest = number
				}
			}
			delete(item.checkpoints, oldest)
		}
	}
	item.lastUsed = time.Now()
	return raw, nil
}

func (m *Manager) Release(sessionID string) error {
	m.mu.Lock()
	item := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	if item == nil {
		return nil
	}
	return closeSession(item)
}

func (m *Manager) ReleaseOwned(sessionID, owner string) error {
	if owner == "" {
		return errors.New("connection owner is required")
	}
	m.mu.Lock()
	item := m.sessions[sessionID]
	if item != nil && item.owner != owner {
		m.mu.Unlock()
		return errors.New("session belongs to a different connection")
	}
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	if item == nil {
		return nil
	}
	return closeSession(item)
}

func (m *Manager) Close() error {
	return m.CloseContext(context.Background())
}

func (m *Manager) CloseContext(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, shutdownTimeout)
	defer cancel()
	m.stop.Do(func() { close(m.done) })
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		select {
		case <-m.closeDone:
			m.mu.Lock()
			defer m.mu.Unlock()
			return m.closeErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.closed = true
	items := make([]*session, 0, len(m.sessions))
	for id, item := range m.sessions {
		delete(m.sessions, id)
		items = append(items, item)
	}
	m.mu.Unlock()
	results := make(chan error, len(items)+1)
	go func() {
		m.wait.Wait()
		results <- nil
	}()
	for _, item := range items {
		go func() {
			results <- closeSessionContext(ctx, item)
		}()
	}
	var failures []error
	for remaining := len(items) + 1; remaining > 0; remaining-- {
		select {
		case err := <-results:
			failures = append(failures, err)
		case <-ctx.Done():
			failures = append(failures, ctx.Err())
			remaining = 1
		}
	}
	err := errors.Join(failures...)
	m.mu.Lock()
	m.closeErr = err
	close(m.closeDone)
	m.mu.Unlock()
	return err
}

func (m *Manager) current(id string, item *session) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.closed && m.sessions[id] == item
}

func (m *Manager) finish(id string, item *session) {
	closed := item.closed
	item.gate <- struct{}{}
	if closed {
		m.mu.Lock()
		if m.sessions[id] == item {
			delete(m.sessions, id)
		}
		m.mu.Unlock()
	}
}

func acquire(ctx context.Context, item *session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-item.gate:
		if err := ctx.Err(); err != nil {
			item.gate <- struct{}{}
			return err
		}
		return nil
	}
}

func (m *Manager) sweep() {
	defer m.wait.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			m.expire()
		}
	}
}

func (m *Manager) expire() {
	m.mu.Lock()
	var expired []*session
	for id, item := range m.sessions {
		select {
		case <-item.gate:
			if time.Since(item.lastUsed) >= idleTTL {
				delete(m.sessions, id)
				expired = append(expired, item)
			} else {
				item.gate <- struct{}{}
			}
		default:
		}
	}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	results := make(chan error, len(expired))
	for _, item := range expired {
		go func() {
			err := closeWithContext(ctx, item)
			item.gate <- struct{}{}
			results <- err
		}()
	}
	for range expired {
		select {
		case err := <-results:
			if err != nil {
				m.onError(err)
			}
		case <-ctx.Done():
			m.onError(ctx.Err())
			return
		}
	}
}

func closeSession(item *session) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	return closeSessionContext(ctx, item)
}

func closeSessionContext(ctx context.Context, item *session) error {
	if err := acquire(ctx, item); err != nil {
		return err
	}
	defer func() { item.gate <- struct{}{} }()
	return closeWithContext(ctx, item)
}

func closeHeld(item *session) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	return closeWithContext(ctx, item)
}

func closeWithContext(ctx context.Context, item *session) error {
	if item.closed {
		return nil
	}
	item.closed = true
	var snapshotErr, leaseErr error
	if item.snapshot != nil {
		snapshotErr = item.snapshot.Close(ctx)
	}
	if item.lease != nil {
		leaseErr = item.lease.Release()
	}
	return errors.Join(snapshotErr, leaseErr)
}

func sameSort(a, b []json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
