package changelog_controller

import (
	"context"
	"sync"
	"time"

	rc "github.com/openshift-eng/changelog-controller/pkg/release-controller"
	"k8s.io/klog/v2"
)

// ReleaseWatcher polls release-controller for newly ready releases and enqueues
// changelog generation for each new release paired with its predecessor
type ReleaseWatcher struct {
	rcClient   *rc.MultiClient
	controller *ReleasePayloadChangelogController
	extractor  *ReleaseExtractor

	pollInterval time.Duration

	mu   sync.Mutex
	seen map[string]map[string]bool // endpoint key -> set of release tags already seen
}

// NewReleaseWatcher creates a watcher that polls for ready releases at the given interval.
// If extractor is non-nil, the watcher will extract per-release metadata for each new release
func NewReleaseWatcher(rcClient *rc.MultiClient, controller *ReleasePayloadChangelogController, extractor *ReleaseExtractor, pollInterval time.Duration) *ReleaseWatcher {
	return &ReleaseWatcher{
		rcClient:     rcClient,
		controller:   controller,
		extractor:    extractor,
		pollInterval: pollInterval,
		seen:         make(map[string]map[string]bool),
	}
}

// Run starts the polling loop. It blocks until ctx is cancelled
func (w *ReleaseWatcher) Run(ctx context.Context) {
	klog.Infof("Starting release watcher (poll interval: %v)", w.pollInterval)
	defer klog.Info("Release watcher stopped")

	// Do an initial poll immediately to seed the seen set without enqueuing
	w.seedInitialState(ctx)

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

// seedInitialState fetches the current ready releases and records them as seen
// without enqueuing any changelog work. This prevents a flood of enqueue calls
// on first startup for releases that already existed.
//
// It also triggers background extraction for the latest accepted release in
// each stream so that RPM data is pre-populated for the most recent releases.
func (w *ReleaseWatcher) seedInitialState(ctx context.Context) {
	for key, client := range w.rcClient.GetAllClients() {
		streams, err := client.GetReadyReleaseStreams(ctx)
		if err != nil {
			klog.V(4).Infof("Release watcher: failed to seed from %s: %v", key, err)
			continue
		}

		w.mu.Lock()
		if w.seen[key] == nil {
			w.seen[key] = make(map[string]bool)
		}
		for _, tags := range streams {
			for _, tag := range tags {
				w.seen[key][tag] = true
			}
		}
		w.mu.Unlock()

		klog.V(4).Infof("Release watcher: seeded %d releases from %s", len(w.seen[key]), key)

		// Pre-extract metadata for the latest accepted release per stream.
		// Concurrency is limited by the extractor's global semaphore.
		if w.extractor != nil {
			for streamName := range streams {
				latest, err := client.GetLatestAcceptedForStream(ctx, streamName)
				if err != nil {
					klog.V(4).Infof("Release watcher: failed to get latest accepted for stream %s during seed: %v", streamName, err)
					continue
				}
				pullSpec, err := client.GetPullSpec(ctx, latest.Name)
				if err != nil {
					klog.V(4).Infof("Release watcher: failed to get pull spec for %s during seed: %v", latest.Name, err)
					continue
				}
				go func(tag, ps string) {
					if err := w.extractor.Extract(ctx, ps); err != nil {
						klog.Warningf("Release watcher: seed extraction failed for %s: %v", tag, err)
					} else {
						klog.Infof("Release watcher: seed extraction completed for %s", tag)
					}
				}(latest.Name, pullSpec)
			}
		}
	}
	klog.Info("Release watcher: initial state seeded")
}

type newRelease struct {
	tag        string
	streamName string
}

// poll fetches ready releases from all endpoints and enqueues changelogs for new ones
func (w *ReleaseWatcher) poll(ctx context.Context) {
	for key, client := range w.rcClient.GetAllClients() {
		streams, err := client.GetReadyReleaseStreams(ctx)
		if err != nil {
			klog.V(4).Infof("Release watcher: poll failed for %s: %v", key, err)
			continue
		}

		// Collect new releases under the lock, then release before making network calls
		var detected []newRelease
		w.mu.Lock()
		if w.seen[key] == nil {
			w.seen[key] = make(map[string]bool)
		}
		for streamName, tags := range streams {
			for _, tag := range tags {
				if w.seen[key][tag] {
					continue
				}
				w.seen[key][tag] = true
				klog.Infof("Release watcher: new ready release detected: %s in stream %s (endpoint %s)", tag, streamName, key)
				detected = append(detected, newRelease{tag: tag, streamName: streamName})
			}
		}
		w.mu.Unlock()

		// Look up predecessor via /latest and enqueue outside the lock
		for _, nr := range detected {
			latest, err := client.GetLatestAcceptedForStream(ctx, nr.streamName)
			if err != nil {
				klog.Warningf("Release watcher: failed to get latest accepted for stream %s: %v", nr.streamName, err)
				continue
			}
			if latest.Name == nr.tag {
				klog.V(4).Infof("Release watcher: %s is already the latest accepted in stream %s, no predecessor to diff against", nr.tag, nr.streamName)
				continue
			}
			w.enqueueChangelog(ctx, client, latest.Name, nr.tag)
		}
	}
}

// enqueueChangelog resolves pull specs for from/to and enqueues a changelog request
func (w *ReleaseWatcher) enqueueChangelog(ctx context.Context, client rc.Client, from, to string) {
	fromPullSpec, err := client.GetPullSpec(ctx, from)
	if err != nil {
		klog.Warningf("Release watcher: failed to get pull spec for %s: %v", from, err)
		return
	}

	toPullSpec, err := client.GetPullSpec(ctx, to)
	if err != nil {
		klog.Warningf("Release watcher: failed to get pull spec for %s: %v", to, err)
		return
	}

	if w.extractor != nil {
		go func() {
			if err := w.extractor.Extract(ctx, fromPullSpec); err != nil {
				klog.Warningf("Release watcher: extraction failed for %s: %v", from, err)
			}
		}()
		go func() {
			if err := w.extractor.Extract(ctx, toPullSpec); err != nil {
				klog.Warningf("Release watcher: extraction failed for %s: %v", to, err)
			}
		}()
	}

	req := ChangelogRequest{
		From: fromPullSpec,
		To:   toPullSpec,
	}
	w.controller.Enqueue(req)
	klog.Infof("Release watcher: enqueued changelog generation %s -> %s", from, to)
}
