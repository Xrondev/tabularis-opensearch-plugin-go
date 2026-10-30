package connectioncache

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/opensearch-project/opensearch-go/v5/signer"

	"tabularis-opensearch-plugin-go/internal/awsauth"
	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/opensearch"
)

const (
	maxClients = 16
	idleTTL    = 15 * time.Minute
)

type Cache struct {
	mu       sync.Mutex
	key      [32]byte
	entries  map[string]*entry
	building map[string]chan struct{}
	total    int
	closed   bool
}

type entry struct {
	client      *opensearch.Client
	fingerprint [32]byte
	generation  [32]byte
	lastUsed    time.Time
	refs        int
	retired     bool
}

type Lease struct {
	cache    *Cache
	entry    *entry
	once     sync.Once
	err      error
	released bool
}

func New() (*Cache, error) {
	c := &Cache{entries: make(map[string]*entry), building: make(map[string]chan struct{})}
	if _, err := rand.Read(c.key[:]); err != nil {
		return nil, fmt.Errorf("initialize connection cache: %w", err)
	}
	return c, nil
}

func (c *Cache) Acquire(ctx context.Context, config connection.Config) (*Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fingerprint, err := c.fingerprint(config)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("connection cache is closed")
	}
	if config.ConnectionID == "" {
		c.mu.Unlock()
		client, err := create(ctx, config)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		closed := c.closed
		c.mu.Unlock()
		if err := ctx.Err(); err != nil || closed {
			if closed {
				err = errors.Join(err, errors.New("connection cache is closed"))
			}
			return nil, errors.Join(err, client.Close())
		}
		return &Lease{entry: &entry{client: client}}, nil
	}
	if ready := c.building[config.ConnectionID]; ready != nil {
		c.mu.Unlock()
		select {
		case <-ready:
			return c.Acquire(ctx, config)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	now := time.Now()
	for id, item := range c.entries {
		if item.refs == 0 && now.Sub(item.lastUsed) >= idleTTL {
			delete(c.entries, id)
			c.total--
			if err := item.client.Close(); err != nil {
				c.mu.Unlock()
				return nil, fmt.Errorf("close expired connection: %w", err)
			}
		}
	}
	if item := c.entries[config.ConnectionID]; item != nil {
		if item.fingerprint == fingerprint {
			item.refs++
			item.lastUsed = now
			c.mu.Unlock()
			return &Lease{cache: c, entry: item}, nil
		}
		delete(c.entries, config.ConnectionID)
		item.retired = true
		if item.refs == 0 {
			c.total--
			if err := item.client.Close(); err != nil {
				c.mu.Unlock()
				return nil, fmt.Errorf("close replaced connection: %w", err)
			}
		}
	}
	if c.total >= maxClients {
		var oldestID string
		var oldest *entry
		for id, item := range c.entries {
			if item.refs == 0 && (oldest == nil || item.lastUsed.Before(oldest.lastUsed)) {
				oldestID, oldest = id, item
			}
		}
		if oldest == nil {
			c.mu.Unlock()
			return nil, errors.New("connection cache is at capacity")
		}
		delete(c.entries, oldestID)
		c.total--
		if err := oldest.client.Close(); err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("close evicted connection: %w", err)
		}
	}
	ready := make(chan struct{})
	c.building[config.ConnectionID] = ready
	c.total++
	c.mu.Unlock()
	client, err := create(ctx, config)
	c.mu.Lock()
	delete(c.building, config.ConnectionID)
	close(ready)
	if err != nil {
		c.total--
		c.mu.Unlock()
		return nil, err
	}
	if c.closed || ctx.Err() != nil {
		c.total--
		closed := c.closed
		c.mu.Unlock()
		if closed {
			return nil, errors.Join(errors.New("connection cache is closed"), client.Close())
		}
		return nil, errors.Join(ctx.Err(), client.Close())
	}
	item := &entry{client: client, fingerprint: fingerprint, lastUsed: time.Now(), refs: 1}
	if _, err := rand.Read(item.generation[:]); err != nil {
		c.total--
		c.mu.Unlock()
		return nil, errors.Join(fmt.Errorf("initialize connection generation: %w", err), client.Close())
	}
	c.entries[config.ConnectionID] = item
	c.mu.Unlock()
	return &Lease{cache: c, entry: item}, nil
}

func (l *Lease) Client() *opensearch.Client {
	return l.entry.client
}

func (l *Lease) Generation() [32]byte {
	return l.entry.generation
}

func (l *Lease) Retain() (*Lease, error) {
	if l.cache == nil {
		return nil, errors.New("unsaved connections cannot retain a session")
	}
	l.cache.mu.Lock()
	defer l.cache.mu.Unlock()
	if l.released || l.cache.closed || l.entry.retired {
		return nil, errors.New("connection lease is no longer active")
	}
	l.entry.refs++
	return &Lease{cache: l.cache, entry: l.entry}, nil
}

func (l *Lease) Release() error {
	l.once.Do(func() {
		if l.cache == nil {
			l.err = l.entry.client.Close()
			return
		}
		l.cache.mu.Lock()
		defer l.cache.mu.Unlock()
		l.released = true
		l.entry.refs--
		l.entry.lastUsed = time.Now()
		if l.entry.refs == 0 && l.entry.retired {
			l.cache.total--
			l.err = l.entry.client.Close()
		}
	})
	return l.err
}

func (c *Cache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var errs []error
	for id, item := range c.entries {
		delete(c.entries, id)
		item.retired = true
		if item.refs == 0 {
			c.total--
			if err := item.client.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (c *Cache) fingerprint(config connection.Config) ([32]byte, error) {
	mac := hmac.New(sha256.New, c.key[:])
	if err := json.NewEncoder(mac).Encode(config); err != nil {
		return [32]byte{}, fmt.Errorf("fingerprint connection configuration: %w", err)
	}
	if config.CAPath != "" {
		certificates, err := os.ReadFile(config.CAPath)
		if err != nil {
			return [32]byte{}, fmt.Errorf("read CA certificate: %w", err)
		}
		if _, err := mac.Write(certificates); err != nil {
			return [32]byte{}, fmt.Errorf("fingerprint CA certificate: %w", err)
		}
	}
	var result [32]byte
	copy(result[:], mac.Sum(nil))
	return result, nil
}

func create(ctx context.Context, config connection.Config) (*opensearch.Client, error) {
	var requestSigner signer.Signer
	if strings.HasPrefix(config.AuthMode, "aws_") {
		var err error
		requestSigner, err = awsauth.New(ctx, awsauth.Options{
			Deployment:      config.Deployment,
			Region:          config.Region,
			AuthMode:        config.AuthMode,
			Profile:         config.Profile,
			AccessKeyID:     config.AccessKeyID,
			SecretAccessKey: config.SecretAccessKey,
			SessionToken:    config.SessionToken,
		})
		if err != nil {
			return nil, err
		}
	}
	return opensearch.New(config, requestSigner)
}
