package container

import (
	"context"
	"strings"
	"sync"

	"github.com/distribution/reference"
)

type imagePullKey struct {
	reference string
	platform  string
}

func newImagePullKey(image, platform string) imagePullKey {
	normalized := strings.TrimSpace(image)
	if named, err := reference.ParseNormalizedNamed(normalized); err == nil {
		normalized = reference.FamiliarString(reference.TagNameOnly(named))
	}
	return imagePullKey{
		reference: normalized,
		platform:  strings.ToLower(strings.TrimSpace(platform)),
	}
}

func imageDescription(image, platform string) string {
	if platform == "" {
		return image
	}
	return image + " (platform " + platform + ")"
}

type imagePullHooks struct {
	Queued  func()
	Waiting func()
}

type imagePullCall struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	err     error
	waiters int
}

type imagePullCoordinator struct {
	mu       sync.Mutex
	inflight map[imagePullKey]*imagePullCall
	active   int
	limit    int
	changed  chan struct{}
}

func newImagePullCoordinator() *imagePullCoordinator {
	return &imagePullCoordinator{
		inflight: make(map[imagePullKey]*imagePullCall),
		changed:  make(chan struct{}),
	}
}

func (c *imagePullCoordinator) Do(
	ctx context.Context,
	key imagePullKey,
	hooks imagePullHooks,
	pull func(context.Context) error,
) error {
	call, leader := c.join(key)
	if leader {
		go c.run(key, call, hooks.Queued, pull)
	} else if hooks.Waiting != nil {
		hooks.Waiting()
	}
	return c.wait(ctx, key, call)
}

func (c *imagePullCoordinator) join(key imagePullKey) (*imagePullCall, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if call, ok := c.inflight[key]; ok {
		call.waiters++
		return call, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	call := &imagePullCall{ctx: ctx, cancel: cancel, done: make(chan struct{}), waiters: 1}
	c.inflight[key] = call
	return call, true
}

func (c *imagePullCoordinator) run(key imagePullKey, call *imagePullCall, queued func(), pull func(context.Context) error) {
	err := c.acquire(call.ctx, queued)
	if err == nil {
		err = pull(call.ctx)
		c.release()
	}
	c.finish(key, call, err)
}

func (c *imagePullCoordinator) acquire(ctx context.Context, queued func()) error {
	queuedOnce := false
	for {
		c.mu.Lock()
		if c.limit == 0 || c.active < c.limit {
			c.active++
			c.mu.Unlock()
			return nil
		}
		changed := c.changed
		c.mu.Unlock()

		if !queuedOnce && queued != nil {
			queued()
			queuedOnce = true
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *imagePullCoordinator) SetLimit(limit int) {
	c.mu.Lock()
	c.limit = limit
	close(c.changed)
	c.changed = make(chan struct{})
	c.mu.Unlock()
}

func (c *imagePullCoordinator) wait(ctx context.Context, key imagePullKey, call *imagePullCall) error {
	select {
	case <-call.done:
		return call.err
	case <-ctx.Done():
		c.leave(key, call)
		return ctx.Err()
	}
}

func (c *imagePullCoordinator) leave(key imagePullKey, call *imagePullCall) {
	c.mu.Lock()
	call.waiters--
	if call.waiters == 0 {
		if c.inflight[key] == call {
			delete(c.inflight, key)
		}
		call.cancel()
	}
	c.mu.Unlock()
}

func (c *imagePullCoordinator) release() {
	c.mu.Lock()
	c.active--
	close(c.changed)
	c.changed = make(chan struct{})
	c.mu.Unlock()
}

func (c *imagePullCoordinator) finish(key imagePullKey, call *imagePullCall, err error) {
	c.mu.Lock()
	call.err = err
	if c.inflight[key] == call {
		delete(c.inflight, key)
	}
	close(call.done)
	call.cancel()
	c.mu.Unlock()
}
