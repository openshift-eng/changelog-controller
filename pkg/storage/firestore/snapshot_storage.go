package firestore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	gcs "cloud.google.com/go/storage"
	"github.com/openshift-eng/changelog-controller/pkg/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

// SnapshotStorage implements per-release snapshot storage using Firestore as
// an index and GCS as the content store. Each snapshot is stored as a JSON
// object in GCS, with a lightweight Firestore document pointing to it.
type SnapshotStorage struct {
	collectionID    string
	bucketName      string
	fsClient        *firestore.Client
	gcsClient       *gcs.Client
	reader          gcsObjectReader
	listConcurrency int
}

// snapshotIndexDoc represents the Firestore index document for a snapshot
type snapshotIndexDoc struct {
	PullSpec    string    `firestore:"pullSpec"`
	ObjectPath  string    `firestore:"objectPath"`
	ExtractedAt time.Time `firestore:"extractedAt"`
	HasRPMList  bool      `firestore:"hasRpmList"`
}

// SnapshotConfig holds configuration for Firestore + GCS snapshot storage
type SnapshotConfig struct {
	ProjectID    string
	DatabaseID   string
	CollectionID string
	BucketName   string
	// ListConcurrency is the maximum number of GCS objects read in parallel
	// when listing snapshots for cache warming. Defaults to 32 when <= 0.
	ListConcurrency int
}

var _ storage.ReleaseSnapshotStorage = (*SnapshotStorage)(nil)
var _ storage.ReleaseSnapshotLister = (*SnapshotStorage)(nil)

// NewSnapshotStorage creates a new Firestore + GCS snapshot storage backend
func NewSnapshotStorage(ctx context.Context, cfg SnapshotConfig) (*SnapshotStorage, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("projectID is required")
	}
	if cfg.CollectionID == "" {
		return nil, fmt.Errorf("collectionID is required")
	}
	if cfg.BucketName == "" {
		return nil, fmt.Errorf("bucketName is required")
	}

	var fsClient *firestore.Client
	var err error
	if cfg.DatabaseID != "" {
		fsClient, err = firestore.NewClientWithDatabase(ctx, cfg.ProjectID, cfg.DatabaseID)
	} else {
		fsClient, err = firestore.NewClient(ctx, cfg.ProjectID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create Firestore client: %w", err)
	}

	gcsClient, err := gcs.NewClient(ctx)
	if err != nil {
		fsClient.Close()
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	return &SnapshotStorage{
		collectionID:    cfg.CollectionID,
		bucketName:      cfg.BucketName,
		fsClient:        fsClient,
		gcsClient:       gcsClient,
		reader:          &gcsClientReader{client: gcsClient, bucketName: cfg.BucketName},
		listConcurrency: normalizeListConcurrency(cfg.ListConcurrency),
	}, nil
}

// pullSpecHash returns a truncated hex-encoded SHA-256 of the pull spec,
// used as both the Firestore document ID and GCS object key component
func pullSpecHash(pullSpec string) string {
	hash := sha256.Sum256([]byte(pullSpec))
	return fmt.Sprintf("%x", hash[:16])
}

// snapshotObjectPath generates a deterministic GCS object path for a snapshot
func snapshotObjectPath(pullSpec string) string {
	return fmt.Sprintf("snapshots/%s.json", pullSpecHash(pullSpec))
}

// Get retrieves a snapshot by looking up its GCS location in Firestore
func (s *SnapshotStorage) Get(ctx context.Context, pullSpec string) (*storage.ReleaseSnapshot, bool, error) {
	snap, err := s.fsClient.Collection(s.collectionID).Doc(pullSpecHash(pullSpec)).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get snapshot index from Firestore: %w", err)
	}

	var doc snapshotIndexDoc
	if err := snap.DataTo(&doc); err != nil {
		return nil, false, fmt.Errorf("failed to decode Firestore snapshot document: %w", err)
	}

	data, err := s.readFromGCS(ctx, doc.ObjectPath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read snapshot from GCS: %w", err)
	}

	var snapshot storage.ReleaseSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return &snapshot, true, nil
}

// Set stores a snapshot in GCS and writes the index entry to Firestore
func (s *SnapshotStorage) Set(ctx context.Context, pullSpec string, snapshot *storage.ReleaseSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	path := snapshotObjectPath(pullSpec)

	if err := s.writeToGCS(ctx, path, data); err != nil {
		return fmt.Errorf("failed to write snapshot to GCS: %w", err)
	}

	doc := snapshotIndexDoc{
		PullSpec:    pullSpec,
		ObjectPath:  path,
		ExtractedAt: snapshot.ExtractedAt,
		HasRPMList:  snapshot.RPMList != nil,
	}

	if _, err := s.fsClient.Collection(s.collectionID).Doc(pullSpecHash(pullSpec)).Set(ctx, doc); err != nil {
		klog.Warningf("Firestore snapshot index write failed after GCS write succeeded (orphaned object: %s): %v", path, err)
		return fmt.Errorf("failed to write snapshot index to Firestore: %w", err)
	}

	return nil
}

// List returns all snapshots by reading the index from Firestore and fetching
// their contents from GCS in parallel
func (s *SnapshotStorage) List(ctx context.Context) ([]*storage.ReleaseSnapshot, error) {
	it := s.fsClient.Collection(s.collectionID).Documents(ctx)
	defer it.Stop()

	var docs []snapshotIndexDoc
	for {
		snap, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate Firestore snapshot documents: %w", err)
		}

		var doc snapshotIndexDoc
		if err := snap.DataTo(&doc); err != nil {
			klog.Warningf("Skipping malformed snapshot index document %s: %v", snap.Ref.ID, err)
			continue
		}

		docs = append(docs, doc)
	}

	paths := make([]string, len(docs))
	for i := range docs {
		paths[i] = docs[i].ObjectPath
	}

	contents, readErrs := readObjectsInParallel(ctx, s.reader, paths, s.listConcurrency)

	snapshots := make([]*storage.ReleaseSnapshot, 0, len(docs))
	for i, doc := range docs {
		if readErrs[i] != nil {
			klog.Warningf("Skipping snapshot %s (GCS read failed): %v", doc.PullSpec, readErrs[i])
			continue
		}

		var snapshot storage.ReleaseSnapshot
		if err := json.Unmarshal(contents[i], &snapshot); err != nil {
			klog.Warningf("Skipping snapshot %s (unmarshal failed): %v", doc.PullSpec, err)
			continue
		}

		snapshots = append(snapshots, &snapshot)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return snapshots, nil
}

// Close closes both the Firestore and GCS clients
func (s *SnapshotStorage) Close() error {
	var firstErr error
	if s.fsClient != nil {
		if err := s.fsClient.Close(); err != nil {
			firstErr = err
		}
	}
	if s.gcsClient != nil {
		if err := s.gcsClient.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *SnapshotStorage) writeToGCS(ctx context.Context, path string, data []byte) error {
	w := s.gcsClient.Bucket(s.bucketName).Object(path).NewWriter(ctx)
	w.ContentType = "application/json"
	_, writeErr := w.Write(data)
	closeErr := w.Close()
	if writeErr != nil {
		return fmt.Errorf("%s: %w", path, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("%s: %w", path, closeErr)
	}
	return nil
}

func (s *SnapshotStorage) readFromGCS(ctx context.Context, path string) ([]byte, error) {
	return s.reader.ReadObject(ctx, path)
}
