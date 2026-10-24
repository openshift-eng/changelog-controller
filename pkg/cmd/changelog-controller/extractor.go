package changelog_controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openshift-eng/changelog-controller/pkg/storage"
	"k8s.io/klog/v2"
)

// defaultMaxConcurrentExtractions is the default number of oc processes
// allowed to run simultaneously. Each extraction uses ~1-2GB of memory
// (rpmdb is the expensive part), so this prevents OOM under load.
const defaultMaxConcurrentExtractions = 3

// ReleaseExtractor extracts and caches per-release metadata by running
// oc adm release info against a release pull spec to collect its
// component manifest and RPM package list
type ReleaseExtractor struct {
	mu      sync.RWMutex
	cache   map[string]*storage.ReleaseSnapshot
	storage storage.ReleaseSnapshotStorage
	sem     chan struct{}
}

// NewReleaseExtractor creates an extractor backed by the given storage.
// If storage is nil, snapshots are kept in memory only.
// maxConcurrent controls how many oc processes can run simultaneously;
// if <= 0, defaultMaxConcurrentExtractions is used.
func NewReleaseExtractor(snapshotStorage storage.ReleaseSnapshotStorage, maxConcurrent int) *ReleaseExtractor {
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrentExtractions
	}
	return &ReleaseExtractor{
		cache:   make(map[string]*storage.ReleaseSnapshot),
		storage: snapshotStorage,
		sem:     make(chan struct{}, maxConcurrent),
	}
}

// Get returns a cached snapshot for the given pull spec, checking
// in-memory cache first, then the storage backend
func (e *ReleaseExtractor) Get(ctx context.Context, pullSpec string) (*storage.ReleaseSnapshot, bool) {
	e.mu.RLock()
	snap, found := e.cache[pullSpec]
	e.mu.RUnlock()
	if found {
		return snap, true
	}

	if e.storage != nil {
		snap, found, err := e.storage.Get(ctx, pullSpec)
		if err != nil {
			klog.V(4).Infof("Extractor: storage lookup failed for %s: %v", pullSpec, err)
			return nil, false
		}
		if found {
			e.mu.Lock()
			e.cache[pullSpec] = snap
			e.mu.Unlock()
			return snap, true
		}
	}

	return nil, false
}

// extractTimeout is the maximum time allowed for a full extraction
// (manifest + rpmdb). The rpmdb command alone can take ~60s, so this
// budget covers both commands with headroom.
const extractTimeout = 5 * time.Minute

// Extract runs oc adm release info to collect the component manifest and
// RPM package list for the given pull spec. If the snapshot is already
// cached, this is a no-op.
//
// Subprocesses run under a detached context so that short-lived caller
// contexts (e.g. HTTP request contexts) cannot kill a long-running
// oc command mid-extraction.
func (e *ReleaseExtractor) Extract(ctx context.Context, pullSpec string) error {
	if snap, found := e.Get(ctx, pullSpec); found && snap.RPMList != nil {
		klog.V(4).Infof("Extractor: snapshot already cached for %s", pullSpec)
		return nil
	}

	// Acquire a semaphore slot to limit concurrent oc processes.
	// Each extraction uses ~1-2GB of memory, so unbounded concurrency
	// can OOM the pod.
	inUse := len(e.sem)
	capacity := cap(e.sem)
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	default:
		klog.Infof("Extractor: semaphore full (%d/%d), waiting for slot for %s", inUse, capacity, pullSpec)
		select {
		case e.sem <- struct{}{}:
			defer func() { <-e.sem }()
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	klog.Infof("Extractor: acquired semaphore slot (%d/%d) for %s", len(e.sem), capacity, pullSpec)

	// Re-check after acquiring the semaphore — another goroutine may
	// have completed extraction while we were waiting
	if snap, found := e.Get(ctx, pullSpec); found && snap.RPMList != nil {
		klog.Infof("Extractor: snapshot completed by another goroutine while waiting for %s", pullSpec)
		return nil
	}

	klog.Infof("Extractor: starting extraction for %s", pullSpec)

	oc, err := exec.LookPath("oc")
	if err != nil {
		return fmt.Errorf("failed to find oc: %w", err)
	}

	// Use a detached context for subprocesses so HTTP request timeouts
	// or proxy disconnects cannot kill the oc process mid-extraction
	execCtx, cancel := context.WithTimeout(context.Background(), extractTimeout)
	defer cancel()

	// Extract component manifest
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	cmd := exec.CommandContext(execCtx, oc, "adm", "release", "info", "--output=json", pullSpec)
	cmd.Stdout = out
	cmd.Stderr = errOut
	klog.V(4).Infof("Extractor: running %s", cmd.String())

	if err := cmd.Run(); err != nil {
		msg := errOut.String()
		if len(msg) == 0 {
			msg = err.Error()
		}
		return fmt.Errorf("oc adm release info failed for %s: %s", pullSpec, msg)
	}

	if !json.Valid(out.Bytes()) {
		return fmt.Errorf("oc returned invalid JSON for %s", pullSpec)
	}

	snapshot := &storage.ReleaseSnapshot{
		PullSpec:    pullSpec,
		Manifest:    json.RawMessage(out.Bytes()),
		ExtractedAt: time.Now(),
	}

	// Extract RPM package list (best-effort).
	// Detect the machine-OS tag from the manifest so we can pass
	// --rpmdb-image to target the correct image (required for older
	// releases where the default doesn't work).
	rpmArgs := []string{"adm", "release", "info", "--rpmdb-cache=/tmp/rpmdb/", "--output=json", "--rpmdb"}
	if tag := detectMachineOSTag(out.Bytes()); tag != "" {
		rpmArgs = append(rpmArgs, "--rpmdb-image="+tag)
		klog.V(4).Infof("Extractor: using --rpmdb-image=%s for %s", tag, pullSpec)
	}
	rpmArgs = append(rpmArgs, pullSpec)
	rpmOut, rpmErrOut := &bytes.Buffer{}, &bytes.Buffer{}
	rpmCmd := exec.CommandContext(execCtx, oc, rpmArgs...)
	rpmCmd.Stdout = rpmOut
	rpmCmd.Stderr = rpmErrOut
	klog.V(4).Infof("Extractor: running %s", rpmCmd.String())

	if err := rpmCmd.Run(); err != nil {
		msg := rpmErrOut.String()
		if len(msg) == 0 {
			msg = err.Error()
		}
		klog.Warningf("Extractor: rpmdb extraction failed for %s: %s", pullSpec, msg)
	} else {
		// oc --rpmdb outputs a flat {"package": "version"} map, not
		// the nested {"packages": {...}} structure RpmList expects
		var packages map[string]string
		if err := json.Unmarshal(rpmOut.Bytes(), &packages); err != nil {
			klog.Warningf("Extractor: failed to unmarshal rpmdb output for %s: %v", pullSpec, err)
		} else {
			snapshot.RPMList = &storage.RpmList{Packages: packages}
		}
	}

	e.mu.Lock()
	e.cache[pullSpec] = snapshot
	e.mu.Unlock()

	// Persist using the detached context so storage writes aren't
	// cancelled by a short-lived caller context
	if e.storage != nil {
		if err := e.storage.Set(execCtx, pullSpec, snapshot); err != nil {
			klog.Warningf("Extractor: failed to persist snapshot for %s: %v", pullSpec, err)
		}
	}

	klog.Infof("Extractor: stored release snapshot for %s", pullSpec)
	return nil
}

// releaseImageRefs is the subset of `oc adm release info -o json` needed to
// enumerate payload tags and find the machine-OS component.
type releaseImageRefs struct {
	References struct {
		Spec struct {
			Tags []struct {
				Name string `json:"name"`
			} `json:"tags"`
		} `json:"spec"`
	} `json:"references"`
}

// detectMachineOSTag parses the manifest JSON from `oc adm release info -o json`
// and returns the base machine-OS tag name to pass as --rpmdb-image.
//
// Detection follows the same convention as the release-controller: look for
// tags ending in "-extensions" that contain "coreos" (e.g., rhel-coreos-extensions
// -> rhel-coreos). For pre-4.14 payloads that lack coreos extensions, falls back
// to "machine-os-content". Returns empty string if no machine-OS tag is found.
func detectMachineOSTag(manifestJSON []byte) string {
	var ri releaseImageRefs
	if err := json.Unmarshal(manifestJSON, &ri); err != nil {
		klog.V(4).Infof("Extractor: failed to parse manifest for machine-OS tag detection: %v", err)
		return ""
	}

	tagSet := make(map[string]struct{}, len(ri.References.Spec.Tags))
	for _, t := range ri.References.Spec.Tags {
		if t.Name != "" {
			tagSet[t.Name] = struct{}{}
		}
	}

	// Look for *coreos*-extensions tags and derive the base tag.
	// Collect all candidates and sort to ensure deterministic results
	// when multiple coreos images exist (e.g., rhel-coreos + rhel-coreos-10).
	var candidates []string
	for name := range tagSet {
		if !strings.HasSuffix(name, "-extensions") || !strings.Contains(name, "coreos") {
			continue
		}
		base := strings.TrimSuffix(name, "-extensions")
		if _, ok := tagSet[base]; ok {
			candidates = append(candidates, base)
		}
	}
	if len(candidates) > 0 {
		sort.Strings(candidates)
		return candidates[0]
	}

	// Fallback for pre-4.14 payloads
	if _, ok := tagSet["machine-os-content"]; ok {
		return "machine-os-content"
	}

	return ""
}

// WarmCache loads all snapshots from the storage backend into the in-memory cache
func (e *ReleaseExtractor) WarmCache(ctx context.Context) error {
	if e.storage == nil {
		return nil
	}

	lister, ok := e.storage.(storage.ReleaseSnapshotLister)
	if !ok {
		klog.V(4).Info("Extractor: snapshot storage does not support listing, skipping warm")
		return nil
	}

	snapshots, err := lister.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list snapshots: %w", err)
	}

	e.mu.Lock()
	for _, snap := range snapshots {
		e.cache[snap.PullSpec] = snap
	}
	e.mu.Unlock()

	klog.Infof("Extractor: warmed cache with %d snapshots", len(snapshots))
	return nil
}
