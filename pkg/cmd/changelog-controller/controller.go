package changelog_controller

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/openshift-eng/changelog-controller/pkg/storage"
	"github.com/openshift/library-go/pkg/operator/events"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

// ChangelogRequest represents a request to generate a changelog
type ChangelogRequest struct {
	From string
	To   string
}

// QueueItemState represents the processing state of a queue item
type QueueItemState int

const (
	QueueItemPending QueueItemState = iota
	QueueItemProcessing
)

func (s QueueItemState) String() string {
	switch s {
	case QueueItemPending:
		return "Pending"
	case QueueItemProcessing:
		return "Processing"
	default:
		return "Unknown"
	}
}

// QueueItem tracks a single item's state and timing in the queue
type QueueItem struct {
	Request    ChangelogRequest
	State      QueueItemState
	EnqueuedAt time.Time
	StartedAt  time.Time
}

// QueueStats contains a point-in-time snapshot of queue state
type QueueStats struct {
	Depth      int
	Pending    int
	Processing int
	Items      []QueueItem
}

// ChangelogCache provides thread-safe caching of generated changelogs
// with optional persistent storage backends
type ChangelogCache struct {
	mu       sync.RWMutex
	cache    map[string]string
	storages []storage.ChangelogStorage

	// Statistics
	hits      int64
	misses    int64
	sizeBytes int64
}

// CacheStats contains statistics about the cache
type CacheStats struct {
	MemoryEntries   int
	MemorySizeBytes int64
	Hits            int64
	Misses          int64
	HitRate         float64
	StorageBackends []StorageBackendStats
}

// StorageBackendStats contains statistics about a storage backend
type StorageBackendStats struct {
	Type    string
	Entries int
	Error   string
}

// NewChangelogCache creates a new changelog cache
func NewChangelogCache(storages ...storage.ChangelogStorage) *ChangelogCache {
	return &ChangelogCache{
		cache:    make(map[string]string),
		storages: storages,
	}
}

// Get retrieves a cached changelog result
// Checks in-memory cache first, then queries storage backends
func (c *ChangelogCache) Get(ctx context.Context, from, to string) (string, bool) {
	// Check in-memory cache first
	c.mu.RLock()
	key := fmt.Sprintf("%s->%s", from, to)
	result, found := c.cache[key]
	c.mu.RUnlock()

	if found {
		c.mu.Lock()
		c.hits++
		c.mu.Unlock()
		return result, true
	}

	// Check storage backends
	for _, s := range c.storages {
		result, found, err := s.Get(ctx, from, to)
		if err != nil {
			klog.Warningf("Error querying storage backend: %v", err)
			continue
		}
		if found {
			// Populate in-memory cache
			c.mu.Lock()
			c.cache[key] = result
			c.hits++
			c.mu.Unlock()
			return result, true
		}
	}

	c.mu.Lock()
	c.misses++
	c.mu.Unlock()
	return "", false
}

// Set stores a changelog result in the cache and all storage backends
func (c *ChangelogCache) Set(ctx context.Context, from, to, changelog string) {
	c.mu.Lock()
	c.setMemoryLocked(from, to, changelog)
	c.mu.Unlock()

	// Store in all storage backends
	for _, s := range c.storages {
		if err := s.Set(ctx, from, to, changelog); err != nil {
			klog.Warningf("Error storing to storage backend: %v", err)
		}
	}
}

// Seed stores a changelog in the in-memory cache only, without writing it to
// the storage backends. It is intended for cache warming, where the entry was
// just read from storage and re-persisting it would be redundant.
func (c *ChangelogCache) Seed(from, to, changelog string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setMemoryLocked(from, to, changelog)
}

// setMemoryLocked stores a changelog in the in-memory cache and updates the
// tracked size. The caller must hold c.mu.
func (c *ChangelogCache) setMemoryLocked(from, to, changelog string) {
	key := fmt.Sprintf("%s->%s", from, to)

	// If replacing an existing entry, subtract its size first
	if oldValue, exists := c.cache[key]; exists {
		c.sizeBytes -= int64(len(key) + len(oldValue))
	}

	c.cache[key] = changelog
	// Add size of key and value (approximate memory usage)
	c.sizeBytes += int64(len(key) + len(changelog))
}

// Stats returns cache statistics including storage backend information
func (c *ChangelogCache) Stats(ctx context.Context) CacheStats {
	c.mu.RLock()
	memoryEntries := len(c.cache)
	memorySizeBytes := c.sizeBytes
	hits := c.hits
	misses := c.misses
	c.mu.RUnlock()

	total := hits + misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100.0
	}

	stats := CacheStats{
		MemoryEntries:   memoryEntries,
		MemorySizeBytes: memorySizeBytes,
		Hits:            hits,
		Misses:          misses,
		HitRate:         hitRate,
	}

	// Query storage backends for their entry counts
	for _, s := range c.storages {
		backendStats := StorageBackendStats{
			Type: fmt.Sprintf("%T", s),
		}

		// Simplify type name (e.g., *bigquery.BigQueryStorage -> BigQuery)
		typeName := backendStats.Type
		if len(typeName) > 0 && typeName[0] == '*' {
			typeName = typeName[1:]
		}
		// Extract just the package and type name
		if idx := len(typeName) - 1; idx >= 0 {
			for i := idx; i >= 0; i-- {
				if typeName[i] == '.' {
					backendStats.Type = typeName[i+1:]
					break
				}
			}
		}

		// Prefer backend-native counts so diagnostics do not fetch changelog contents.
		if counter, ok := s.(storage.ChangelogCounter); ok {
			entries, err := counter.Count(ctx)
			if err != nil {
				backendStats.Error = err.Error()
			} else {
				backendStats.Entries = entries
			}
		} else if lister, ok := s.(storage.ChangelogLister); ok {
			entries, err := lister.List(ctx)
			if err != nil {
				backendStats.Error = err.Error()
			} else {
				backendStats.Entries = len(entries)
			}
		} else {
			backendStats.Error = "backend does not support listing"
		}

		stats.StorageBackends = append(stats.StorageBackends, backendStats)
	}

	return stats
}

// Close closes all storage backends
func (c *ChangelogCache) Close() error {
	var errs []error
	for _, s := range c.storages {
		if err := s.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing storage backends: %v", errs)
	}
	return nil
}

type ReleasePayloadChangelogController struct {
	name string

	eventRecorder events.Recorder

	cachesToSync []cache.InformerSynced

	queue workqueue.TypedRateLimitingInterface[ChangelogRequest]

	cache *ChangelogCache

	queueMu    sync.Mutex
	queueItems map[ChangelogRequest]QueueItem
}

func NewReleasePayloadChangelogController(eventRecorder events.Recorder, cache *ChangelogCache) *ReleasePayloadChangelogController {
	c := &ReleasePayloadChangelogController{
		name:          "Release Payload Changelog Controller",
		eventRecorder: eventRecorder.WithComponentSuffix("release-payload-changelog-controller"),
		queue:         workqueue.NewTypedRateLimitingQueueWithConfig(workqueue.DefaultTypedControllerRateLimiter[ChangelogRequest](), workqueue.TypedRateLimitingQueueConfig[ChangelogRequest]{Name: "ReleasePayloadChangelogController"}),
		cache:         cache,
		queueItems:    make(map[ChangelogRequest]QueueItem),
	}
	return c
}

func (c *ReleasePayloadChangelogController) Enqueue(req ChangelogRequest) {
	c.queue.Add(req)
	c.queueMu.Lock()
	if _, exists := c.queueItems[req]; !exists {
		c.queueItems[req] = QueueItem{
			Request:    req,
			State:      QueueItemPending,
			EnqueuedAt: time.Now(),
		}
	}
	c.queueMu.Unlock()
}

// QueueStats returns a point-in-time snapshot of the queue state
func (c *ReleasePayloadChangelogController) QueueStats() QueueStats {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	stats := QueueStats{
		Depth: c.queue.Len(),
		Items: make([]QueueItem, 0, len(c.queueItems)),
	}
	for _, item := range c.queueItems {
		switch item.State {
		case QueueItemPending:
			stats.Pending++
		case QueueItemProcessing:
			stats.Processing++
		}
		stats.Items = append(stats.Items, item)
	}
	return stats
}

func (c *ReleasePayloadChangelogController) RunWorkers(ctx context.Context, workers int) {
	defer utilruntime.HandleCrash()

	klog.Infof("Starting %s", c.name)
	defer func() {
		klog.Infof("Shutting down %s", c.name)
		c.queue.ShutDown()
		klog.Infof("%s shut down", c.name)
	}()

	if !cache.WaitForNamedCacheSync(c.name, ctx.Done(), c.cachesToSync...) {
		return
	}

	for range workers {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	<-ctx.Done()
}

func (c *ReleasePayloadChangelogController) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *ReleasePayloadChangelogController) processNextItem(ctx context.Context) bool {
	req, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(req)

	c.queueMu.Lock()
	item := c.queueItems[req]
	item.State = QueueItemProcessing
	item.StartedAt = time.Now()
	c.queueItems[req] = item
	c.queueMu.Unlock()

	err := c.sync(ctx, req)

	if err == nil {
		c.queue.Forget(req)
		c.queueMu.Lock()
		delete(c.queueItems, req)
		c.queueMu.Unlock()
		return true
	}

	utilruntime.HandleError(fmt.Errorf("%v failed with : %w", req, err))
	c.queue.AddRateLimited(req)

	c.queueMu.Lock()
	item = c.queueItems[req]
	item.State = QueueItemPending
	item.StartedAt = time.Time{}
	c.queueItems[req] = item
	c.queueMu.Unlock()

	return true
}

func (c *ReleasePayloadChangelogController) sync(ctx context.Context, req ChangelogRequest) error {
	klog.V(4).Infof("Starting ReleasePayloadChangelogController sync")
	defer klog.V(4).Infof("ReleasePayloadChangelogController sync done")

	klog.V(4).Infof("Generating changelog from %s to %s", req.From, req.To)

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	oc, err := exec.LookPath("oc")
	if err != nil {
		return fmt.Errorf("failed to find oc: %w", err)
	}

	cmd := exec.CommandContext(ctx, oc, "adm", "release", "info", "--changelog=/tmp/git/", req.From, req.To, "--output=json")
	cmd.Stdout = out
	cmd.Stderr = errOut
	klog.V(4).Infof("Running changelog command: %s", cmd.String())

	if err := cmd.Run(); err != nil {
		msg := errOut.String()
		if len(msg) == 0 {
			msg = err.Error()
		}
		if strings.Contains(msg, "Could not load commits for") {
			klog.Warningf("Generated changelog with missing commits:\n$ %s\n%s", cmd.String(), errOut.String())
		} else {
			return fmt.Errorf("could not generate changelog: %s", msg)
		}
	}

	klog.V(4).Infof("Finished running changelog command: %s", cmd.String())

	if out.Len() == 0 {
		return fmt.Errorf("oc produced no output for %s -> %s", req.From, req.To)
	}

	c.cache.Set(ctx, req.From, req.To, out.String())
	return nil
}
