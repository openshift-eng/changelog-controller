package changelog_controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/blang/semver/v4"
	"github.com/openshift-eng/changelog-controller/pkg/changelog"
	"github.com/openshift-eng/changelog-controller/pkg/format"
	rc "github.com/openshift-eng/changelog-controller/pkg/release-controller"
	"github.com/openshift-eng/changelog-controller/pkg/storage"
	"github.com/openshift-eng/changelog-controller/pkg/storage/bigquery"
	"github.com/openshift-eng/changelog-controller/pkg/storage/filesystem"
	"github.com/openshift-eng/changelog-controller/pkg/storage/firestore"
	"github.com/openshift-eng/changelog-controller/pkg/storage/sqlite"
	"github.com/openshift-eng/changelog-controller/pkg/version"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

type Options struct {
	controllerContext *controllercmd.ControllerContext
	controller        *ReleasePayloadChangelogController
	cache             *ChangelogCache
	extractor         *ReleaseExtractor
	rcClient          *rc.MultiClient

	// HTTP server configuration
	httpPort int

	// Storage backend configurations
	// BigQuery storage
	bigqueryEnabled   bool
	bigqueryProjectID string
	bigqueryDatasetID string
	bigqueryTableID   string

	// SQLite storage
	sqliteEnabled      bool
	sqliteDatabasePath string

	// Firestore + GCS storage
	firestoreEnabled              bool
	firestoreProjectID            string
	firestoreDatabaseID           string
	firestoreCollectionID         string
	firestoreSnapshotCollectionID string
	firestoreGCSBucket            string

	// Filesystem storage
	filesystemEnabled bool
	filesystemBaseDir string

	// Cache warming
	cacheWarmingEnabled     bool
	cacheWarmingConcurrency int

	// Release watcher
	watcherEnabled      bool
	watcherPollInterval time.Duration

	// Worker count
	workerCount int

	// Maximum concurrent extractions
	maxConcurrentExtractions int
}

func NewReleasePayloadChangelogControllerCommand(name string) *cobra.Command {
	o := &Options{}

	ccc := controllercmd.NewControllerCommandConfig("release-payload-changelog-controller", version.Get(), func(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
		o.controllerContext = controllerContext

		err := o.Validate(ctx)
		if err != nil {
			return err
		}

		err = o.Run(ctx)
		if err != nil {
			return err
		}

		return nil
	}, clock.RealClock{})

	cmd := ccc.NewCommandWithContext(context.Background())
	cmd.Use = name
	cmd.Short = "Start the release payload changelog controller"

	o.AddFlags(cmd.Flags())

	return cmd
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	// HTTP server flags
	fs.IntVar(&o.httpPort, "http-port", getEnvInt("HTTP_PORT", 8080),
		"Port for the changelog HTTP server (can also use HTTP_PORT env var)")

	// BigQuery storage flags
	fs.BoolVar(&o.bigqueryEnabled, "bigquery-enabled", getEnvBool("BIGQUERY_ENABLED", false),
		"Enable BigQuery storage backend")
	fs.StringVar(&o.bigqueryProjectID, "bigquery-project-id", getEnv("BIGQUERY_PROJECT_ID", ""),
		"BigQuery project ID (can also use BIGQUERY_PROJECT_ID env var)")
	fs.StringVar(&o.bigqueryDatasetID, "bigquery-dataset-id", getEnv("BIGQUERY_DATASET_ID", "changelogs"),
		"BigQuery dataset ID (can also use BIGQUERY_DATASET_ID env var)")
	fs.StringVar(&o.bigqueryTableID, "bigquery-table-id", getEnv("BIGQUERY_TABLE_ID", "release_changelogs"),
		"BigQuery table ID (can also use BIGQUERY_TABLE_ID env var)")

	// SQLite storage flags
	fs.BoolVar(&o.sqliteEnabled, "sqlite-enabled", getEnvBool("SQLITE_ENABLED", false),
		"Enable SQLite storage backend")
	fs.StringVar(&o.sqliteDatabasePath, "sqlite-database-path", getEnv("SQLITE_DATABASE_PATH", "/var/lib/changelogs.db"),
		"SQLite database file path (can also use SQLITE_DATABASE_PATH env var)")

	// Firestore storage flags
	fs.BoolVar(&o.firestoreEnabled, "firestore-enabled", getEnvBool("FIRESTORE_ENABLED", false),
		"Enable Firestore storage backend")
	fs.StringVar(&o.firestoreProjectID, "firestore-project-id", getEnv("FIRESTORE_PROJECT_ID", ""),
		"Firestore project ID (can also use FIRESTORE_PROJECT_ID env var)")
	fs.StringVar(&o.firestoreDatabaseID, "firestore-database-id", getEnv("FIRESTORE_DATABASE_ID", ""),
		"Firestore database ID (can also use FIRESTORE_DATABASE_ID env var, defaults to the (default) database)")
	fs.StringVar(&o.firestoreCollectionID, "firestore-collection-id", getEnv("FIRESTORE_COLLECTION_ID", "changelogs"),
		"Firestore collection ID for changelogs (can also use FIRESTORE_COLLECTION_ID env var)")
	fs.StringVar(&o.firestoreSnapshotCollectionID, "firestore-snapshot-collection-id", getEnv("FIRESTORE_SNAPSHOT_COLLECTION_ID", "release_snapshots"),
		"Firestore collection ID for release snapshots (can also use FIRESTORE_SNAPSHOT_COLLECTION_ID env var)")
	fs.StringVar(&o.firestoreGCSBucket, "firestore-gcs-bucket", getEnv("FIRESTORE_GCS_BUCKET", ""),
		"GCS bucket for changelog and snapshot content storage (can also use FIRESTORE_GCS_BUCKET env var)")

	// Filesystem storage flags
	fs.BoolVar(&o.filesystemEnabled, "filesystem-enabled", getEnvBool("FILESYSTEM_ENABLED", true),
		"Enable filesystem storage backend (enabled by default)")
	fs.StringVar(&o.filesystemBaseDir, "filesystem-base-dir", getEnv("FILESYSTEM_BASE_DIR", "/tmp/changelog-storage"),
		"Filesystem storage base directory (can also use FILESYSTEM_BASE_DIR env var)")

	// Cache warming flag
	fs.BoolVar(&o.cacheWarmingEnabled, "cache-warming-enabled", getEnvBool("CACHE_WARMING_ENABLED", true),
		"Enable cache warming from storage backends on startup (enabled by default)")
	fs.IntVar(&o.cacheWarmingConcurrency, "cache-warming-concurrency", getEnvInt("CACHE_WARMING_CONCURRENCY", 32),
		"Maximum number of GCS objects read in parallel during cache warming (can also use CACHE_WARMING_CONCURRENCY env var)")

	// Release watcher flags
	fs.BoolVar(&o.watcherEnabled, "watcher-enabled", getEnvBool("WATCHER_ENABLED", true),
		"Enable release watcher that polls for newly ready releases (enabled by default)")
	fs.DurationVar(&o.watcherPollInterval, "watcher-poll-interval", getEnvDuration("WATCHER_POLL_INTERVAL", 2*time.Minute),
		"How often the release watcher polls for new ready releases (can also use WATCHER_POLL_INTERVAL env var)")

	// Worker count flag
	fs.IntVar(&o.workerCount, "workers", getEnvInt("WORKERS", 10),
		"Number of changelog generation workers (can also use WORKERS env var)")

	// Extraction concurrency flag
	fs.IntVar(&o.maxConcurrentExtractions, "max-concurrent-extractions", getEnvInt("MAX_CONCURRENT_EXTRACTIONS", 3),
		"Maximum number of concurrent oc extraction processes (can also use MAX_CONCURRENT_EXTRACTIONS env var)")
}

// getEnv returns the value of an environment variable or a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBool returns the boolean value of an environment variable or a default value
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}

// getEnvDuration returns the duration value of an environment variable or a default value
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		d, err := time.ParseDuration(value)
		if err == nil {
			return d
		}
	}
	return defaultValue
}

// getEnvInt returns the integer value of an environment variable or a default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intValue int
		if _, err := fmt.Sscanf(value, "%d", &intValue); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func (o *Options) Validate(ctx context.Context) error {
	// Validate HTTP port
	if o.httpPort < 1 || o.httpPort > 65535 {
		return fmt.Errorf("http-port must be between 1 and 65535, got %d", o.httpPort)
	}

	// Validate worker count
	if o.workerCount < 1 {
		return fmt.Errorf("workers must be at least 1, got %d", o.workerCount)
	}

	// Validate BigQuery configuration if enabled
	if o.bigqueryEnabled {
		if o.bigqueryProjectID == "" {
			return fmt.Errorf("bigquery-project-id is required when BigQuery storage is enabled")
		}
		if o.bigqueryDatasetID == "" {
			return fmt.Errorf("bigquery-dataset-id is required when BigQuery storage is enabled")
		}
		if o.bigqueryTableID == "" {
			return fmt.Errorf("bigquery-table-id is required when BigQuery storage is enabled")
		}
	}

	// Validate SQLite configuration if enabled
	if o.sqliteEnabled {
		if o.sqliteDatabasePath == "" {
			return fmt.Errorf("sqlite-database-path is required when SQLite storage is enabled")
		}
	}

	// Validate Firestore configuration if enabled
	if o.firestoreEnabled {
		if o.firestoreProjectID == "" {
			return fmt.Errorf("firestore-project-id is required when Firestore storage is enabled")
		}
		if o.firestoreCollectionID == "" {
			return fmt.Errorf("firestore-collection-id is required when Firestore storage is enabled")
		}
		if o.firestoreSnapshotCollectionID == "" {
			return fmt.Errorf("firestore-snapshot-collection-id is required when Firestore storage is enabled")
		}
		if o.firestoreGCSBucket == "" {
			return fmt.Errorf("firestore-gcs-bucket is required when Firestore storage is enabled")
		}
	}

	// Validate Filesystem configuration if enabled
	if o.filesystemEnabled {
		if o.filesystemBaseDir == "" {
			return fmt.Errorf("filesystem-base-dir is required when Filesystem storage is enabled")
		}
	}

	return nil
}

func (o *Options) initializeStorageBackends(ctx context.Context) ([]storage.ChangelogStorage, []func(), error) {
	var storages []storage.ChangelogStorage
	var cleanupFuncs []func()

	// Initialize BigQuery storage if enabled
	if o.bigqueryEnabled {
		klog.Infof("Initializing BigQuery storage (project=%s, dataset=%s, table=%s)",
			o.bigqueryProjectID, o.bigqueryDatasetID, o.bigqueryTableID)
		bqStorage, err := bigquery.NewBigQueryStorage(ctx, bigquery.Config{
			ProjectID: o.bigqueryProjectID,
			DatasetID: o.bigqueryDatasetID,
			TableID:   o.bigqueryTableID,
		})
		if err != nil {
			klog.Warningf("Failed to initialize BigQuery storage: %v", err)
		} else {
			storages = append(storages, bqStorage)
			cleanupFuncs = append(cleanupFuncs, func() {
				if err := bqStorage.Close(); err != nil {
					klog.Warningf("Failed to close BigQuery storage: %v", err)
				}
			})
			klog.Infof("BigQuery storage initialized successfully")
		}
	}

	// Initialize SQLite storage if enabled
	if o.sqliteEnabled {
		klog.Infof("Initializing SQLite storage (path=%s)", o.sqliteDatabasePath)
		sqliteStorage, err := sqlite.NewSQLiteStorage(ctx, sqlite.Config{
			DatabasePath: o.sqliteDatabasePath,
		})
		if err != nil {
			klog.Warningf("Failed to initialize SQLite storage: %v", err)
		} else {
			storages = append(storages, sqliteStorage)
			cleanupFuncs = append(cleanupFuncs, func() {
				if err := sqliteStorage.Close(); err != nil {
					klog.Warningf("Failed to close SQLite storage: %v", err)
				}
			})
			klog.Infof("SQLite storage initialized successfully")
		}
	}

	// Initialize Firestore storage if enabled
	if o.firestoreEnabled {
		klog.Infof("Initializing Firestore storage (project=%s, database=%s, collection=%s, bucket=%s)",
			o.firestoreProjectID, o.firestoreDatabaseID, o.firestoreCollectionID, o.firestoreGCSBucket)
		firestoreStorage, err := firestore.NewFirestoreStorage(ctx, firestore.Config{
			ProjectID:       o.firestoreProjectID,
			DatabaseID:      o.firestoreDatabaseID,
			CollectionID:    o.firestoreCollectionID,
			BucketName:      o.firestoreGCSBucket,
			ListConcurrency: o.cacheWarmingConcurrency,
		})
		if err != nil {
			klog.Warningf("Failed to initialize Firestore storage: %v", err)
		} else {
			storages = append(storages, firestoreStorage)
			cleanupFuncs = append(cleanupFuncs, func() {
				if err := firestoreStorage.Close(); err != nil {
					klog.Warningf("Failed to close Firestore storage: %v", err)
				}
			})
			klog.Infof("Firestore storage initialized successfully")
		}
	}

	// Initialize Filesystem storage if enabled
	if o.filesystemEnabled {
		klog.Infof("Initializing Filesystem storage (base-dir=%s)", o.filesystemBaseDir)
		fsStorage, err := filesystem.NewFilesystemStorage(ctx, filesystem.Config{
			BaseDir: o.filesystemBaseDir,
		})
		if err != nil {
			klog.Warningf("Failed to initialize Filesystem storage: %v", err)
		} else {
			storages = append(storages, fsStorage)
			cleanupFuncs = append(cleanupFuncs, func() {
				if err := fsStorage.Close(); err != nil {
					klog.Warningf("Failed to close Filesystem storage: %v", err)
				}
			})
			klog.Infof("Filesystem storage initialized successfully")
		}
	}

	if len(storages) == 0 {
		klog.Warning("No storage backends enabled - changelogs will only be stored in memory cache")
	} else {
		klog.Infof("Initialized %d storage backend(s)", len(storages))
	}

	return storages, cleanupFuncs, nil
}

func (o *Options) warmCache(ctx context.Context, storages []storage.ChangelogStorage) error {
	klog.Info("Starting cache warming from storage backends")
	startTime := time.Now()
	totalEntries := 0
	var errors []error

	for i, s := range storages {
		// Check if storage backend supports listing
		lister, ok := s.(storage.ChangelogLister)
		if !ok {
			klog.V(4).Infof("Storage backend %d does not support listing, skipping", i)
			continue
		}

		klog.Infof("Warming cache from storage backend %d", i)
		entries, err := lister.List(ctx)
		if err != nil {
			klog.Warningf("Failed to list entries from storage backend %d: %v", i, err)
			errors = append(errors, fmt.Errorf("backend %d: %w", i, err))
			continue
		}

		// Load entries into cache
		for _, entry := range entries {
			o.cache.Seed(entry.FromVersion, entry.ToVersion, entry.Changelog)
			totalEntries++
		}

		klog.Infof("Loaded %d entries from storage backend %d", len(entries), i)
	}

	duration := time.Since(startTime)
	if totalEntries > 0 {
		klog.Infof("Cache warming completed: loaded %d entries in %v", totalEntries, duration)
	} else {
		klog.Info("Cache warming completed: no entries found in storage backends")
	}

	if len(errors) > 0 {
		return fmt.Errorf("cache warming encountered %d error(s): %v", len(errors), errors)
	}

	return nil
}

func (o *Options) createHTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", o.handleRoot)
	mux.HandleFunc("/changelog", o.handleChangelog)
	mux.HandleFunc("/kernel", o.handleKernel)
	mux.HandleFunc("/status", o.handleStatus)
	return mux
}

func (o *Options) runHTTPServer(ctx context.Context) error {
	handler := o.createHTTPHandler()

	addr := fmt.Sprintf(":%d", o.httpPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	server := &http.Server{
		Handler: handler,
	}

	serverErrors := make(chan error, 1)
	go func() {
		klog.Infof("Starting changelog HTTP server on %s", addr)
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			serverErrors <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		klog.Info("Shutting down changelog HTTP server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("HTTP server shutdown error: %w", err)
		}
		klog.Info("Changelog HTTP server shut down successfully")
		return nil
	case err := <-serverErrors:
		return err
	}
}

func (o *Options) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Release Payload Changelog Controller\n\n")
	fmt.Fprintf(w, "Changelog HTTP Server (Port %d)\n\n", o.httpPort)
	fmt.Fprintf(w, "Available Endpoints:\n\n")
	fmt.Fprintf(w, "  GET /\n")
	fmt.Fprintf(w, "    Description: API documentation (this page)\n\n")
	fmt.Fprintf(w, "  GET /changelog\n")
	fmt.Fprintf(w, "    Description: Generate changelog between two release versions\n")
	fmt.Fprintf(w, "    Required Parameters:\n")
	fmt.Fprintf(w, "      - from: Source release version (semantic version)\n")
	fmt.Fprintf(w, "      - to: Target release version (semantic version)\n")
	fmt.Fprintf(w, "      - arch: Architecture (amd64, arm64, s390x, ppc64le, or multi)\n")
	fmt.Fprintf(w, "    Optional Parameters:\n")
	fmt.Fprintf(w, "      - format: Response format ('json', 'md', or 'html', default: 'json')\n")
	fmt.Fprintf(w, "      - product: Product variant ('ocp' or 'origin', default: 'ocp')\n")
	fmt.Fprintf(w, "    Notes:\n")
	fmt.Fprintf(w, "      JSON responses include an 'rpmDiff' field when RPM package\n")
	fmt.Fprintf(w, "      data has been pre-extracted for both releases\n")
	fmt.Fprintf(w, "    Examples:\n")
	fmt.Fprintf(w, "      /changelog?from=4.15.0&to=4.16.0&arch=amd64\n")
	fmt.Fprintf(w, "      /changelog?from=4.15.0&to=4.16.0&arch=arm64&format=md\n")
	fmt.Fprintf(w, "      /changelog?from=4.15.0&to=4.16.0&arch=amd64&product=origin\n\n")
	fmt.Fprintf(w, "  GET /kernel\n")
	fmt.Fprintf(w, "    Description: Look up kernel version for a specific release\n")
	fmt.Fprintf(w, "    Required Parameters:\n")
	fmt.Fprintf(w, "      - release: Release version (semantic version)\n")
	fmt.Fprintf(w, "      - arch: Architecture (amd64, arm64, s390x, ppc64le, or multi)\n")
	fmt.Fprintf(w, "    Optional Parameters:\n")
	fmt.Fprintf(w, "      - product: Product variant ('ocp' or 'origin', default: 'ocp')\n")
	fmt.Fprintf(w, "    Response Fields:\n")
	fmt.Fprintf(w, "      - kernel: Kernel package version\n")
	fmt.Fprintf(w, "      - kernelRT: Real-time kernel package version (if present)\n")
	fmt.Fprintf(w, "    Examples:\n")
	fmt.Fprintf(w, "      /kernel?release=4.22.6&arch=amd64\n")
	fmt.Fprintf(w, "      /kernel?release=4.22.6&arch=amd64&product=origin\n\n")
	fmt.Fprintf(w, "  GET /status\n")
	fmt.Fprintf(w, "    Description: Cache and storage backend status information\n")
}

func parseProduct(param string) (rc.Product, bool) {
	switch param {
	case "ocp", "":
		return rc.ProductOCP, true
	case "origin":
		return rc.ProductOrigin, true
	default:
		return "", false
	}
}

func isValidArch(arch string) bool {
	for _, a := range rc.AllArchitectures() {
		if arch == string(a) {
			return true
		}
	}
	return false
}

func (o *Options) handleChangelog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	query := r.URL.Query()
	from := query.Get("from")
	to := query.Get("to")
	arch := query.Get("arch")
	responseFormat := query.Get("format")
	product, ok := parseProduct(query.Get("product"))
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error": "invalid product '%s': must be 'ocp' or 'origin'"}`+"\n", query.Get("product"))
		return
	}

	if responseFormat == "" {
		responseFormat = "json"
	}

	if responseFormat != "json" && responseFormat != "md" && responseFormat != "html" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error": "invalid format parameter: must be 'json', 'md', or 'html'"}`+"\n")
		return
	}

	// Validate required parameters
	if from == "" || to == "" || arch == "" {
		w.WriteHeader(http.StatusBadRequest)
		if from == "" && to == "" && arch == "" {
			fmt.Fprintf(w, `{"error": "missing required query parameters 'from', 'to', and 'arch'"}`+"\n")
		} else if from == "" && to == "" {
			fmt.Fprintf(w, `{"error": "missing required query parameters 'from' and 'to'"}`+"\n")
		} else if from == "" && arch == "" {
			fmt.Fprintf(w, `{"error": "missing required query parameters 'from' and 'arch'"}`+"\n")
		} else if to == "" && arch == "" {
			fmt.Fprintf(w, `{"error": "missing required query parameters 'to' and 'arch'"}`+"\n")
		} else if from == "" {
			fmt.Fprintf(w, `{"error": "missing required query parameter 'from'"}`+"\n")
		} else if to == "" {
			fmt.Fprintf(w, `{"error": "missing required query parameter 'to'"}`+"\n")
		} else {
			fmt.Fprintf(w, `{"error": "missing required query parameter 'arch'"}`+"\n")
		}
		return
	}

	if !isValidArch(arch) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error": "invalid architecture '%s': must be one of 'amd64', 'arm64', 's390x', 'ppc64le', or 'multi'"}`+"\n", arch)
		return
	}

	// Validate semver format
	if _, err := semver.Parse(from); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error": "'from' parameter is not a valid semantic version: %s"}`+"\n", from)
		return
	}
	if _, err := semver.Parse(to); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error": "'to' parameter is not a valid semantic version: %s"}`+"\n", to)
		return
	}

	// Fetch PullSpecs BEFORE checking cache - these will be used as cache keys
	klog.V(4).Infof("Fetching PullSpecs for from=%s, to=%s, arch=%s, product=%s", from, to, arch, string(product))
	fromPullSpec, toPullSpec := o.fetchPullSpecs(r.Context(), from, to, product, rc.Architecture(arch))

	// If we couldn't get PullSpecs, return an error
	if fromPullSpec == "" || toPullSpec == "" {
		klog.Warningf("Failed to fetch PullSpecs for from=%s (pullSpec=%s), to=%s (pullSpec=%s)",
			from, fromPullSpec, to, toPullSpec)
		w.WriteHeader(http.StatusNotFound)
		if fromPullSpec == "" && toPullSpec == "" {
			fmt.Fprintf(w, `{"error": "Could not find PullSpecs for releases '%s' and '%s'. Releases may not exist in any release-controller endpoint."}`+"\n", from, to)
		} else if fromPullSpec == "" {
			fmt.Fprintf(w, `{"error": "Could not find PullSpec for release '%s'. Release may not exist in any release-controller endpoint."}`+"\n", from)
		} else {
			fmt.Fprintf(w, `{"error": "Could not find PullSpec for release '%s'. Release may not exist in any release-controller endpoint."}`+"\n", to)
		}
		return
	}

	klog.V(4).Infof("Using PullSpecs as cache keys: from=%s, to=%s", fromPullSpec, toPullSpec)

	// Check cache using PullSpecs as keys
	if o.cache != nil {
		if cached, found := o.cache.Get(r.Context(), fromPullSpec, toPullSpec); found {
			klog.V(4).Infof("Cache hit for %s -> %s", fromPullSpec, toPullSpec)
			rpmDiff, rpmDiffStatus := o.lookupRpmDiff(r.Context(), fromPullSpec, toPullSpec)
			o.writeChangelogResponse(w, from, to, cached, responseFormat, arch, fromPullSpec, toPullSpec, rpmDiff, rpmDiffStatus)
			return
		}
		klog.V(4).Infof("Cache miss for %s -> %s", fromPullSpec, toPullSpec)
	}

	// Cache miss - enqueue the changelog request for generation with PullSpecs
	if o.controller != nil {
		req := ChangelogRequest{
			From: fromPullSpec,
			To:   toPullSpec,
		}
		o.controller.Enqueue(req)

		w.WriteHeader(http.StatusAccepted)
		accepted := struct {
			Status       string `json:"status"`
			Message      string `json:"message"`
			From         string `json:"from"`
			To           string `json:"to"`
			FromPullSpec string `json:"fromPullSpec"`
			ToPullSpec   string `json:"toPullSpec"`
		}{
			Status:       "accepted",
			Message:      "Changelog request queued for generation",
			From:         from,
			To:           to,
			FromPullSpec: fromPullSpec,
			ToPullSpec:   toPullSpec,
		}
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.Encode(accepted)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error": "controller not initialized"}`+"\n")
	}
}

func (o *Options) writeChangelogResponse(w http.ResponseWriter, from, to, changelogData, responseFormat, arch, fromPullSpec, toPullSpec string, rpmDiff *changelog.RpmDiff, rpmDiffStatus string) {
	// Unmarshal into typed struct; fall back to raw JSON if it doesn't match
	var cl changelog.ChangeLog
	var changelogField any
	structured := false
	if err := json.Unmarshal([]byte(changelogData), &cl); err == nil && cl.From.Name != "" && cl.To.Name != "" {
		enrichCoreOSComponents(&cl, arch)
		changelogField = &cl
		structured = true
	} else if json.Valid([]byte(changelogData)) {
		changelogField = json.RawMessage(changelogData)
	} else {
		changelogField = changelogData
	}

	if responseFormat == "md" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		if structured {
			fmt.Fprint(w, format.ChangeLogMarkdown(&cl))
		} else {
			fmt.Fprint(w, changelogData)
		}

		if rpmDiff != nil {
			fmt.Fprint(w, format.RpmDiffMarkdown(rpmDiff))
		}
		return
	}

	if responseFormat == "html" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		if structured {
			if html, err := format.ChangeLogHTML(&cl); err == nil {
				fmt.Fprint(w, html)
			} else {
				klog.Warningf("Failed to render HTML: %v", err)
				fmt.Fprint(w, changelogData)
			}
		} else {
			fmt.Fprint(w, changelogData)
		}

		if rpmDiff != nil {
			if html, err := format.RpmDiffHTML(rpmDiff); err == nil {
				fmt.Fprint(w, html)
			} else {
				klog.Warningf("Failed to render RPM diff HTML: %v", err)
			}
		}
		return
	}

	response := struct {
		From          string             `json:"from"`
		To            string             `json:"to"`
		FromPullSpec  string             `json:"fromPullSpec,omitempty"`
		ToPullSpec    string             `json:"toPullSpec,omitempty"`
		Changelog     any                `json:"changelog"`
		RpmDiff       *changelog.RpmDiff `json:"rpmDiff,omitempty"`
		RpmDiffStatus string             `json:"rpmDiffStatus,omitempty"`
	}{
		From:          from,
		To:            to,
		FromPullSpec:  fromPullSpec,
		ToPullSpec:    toPullSpec,
		Changelog:     changelogField,
		RpmDiff:       rpmDiff,
		RpmDiffStatus: rpmDiffStatus,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(response); err != nil {
		klog.Errorf("Failed to marshal changelog response: %v", err)
	}
}

// lookupRpmDiff computes an RPM diff between two releases using cached
// snapshot data. If RPM data is missing for either release, background
// extraction is triggered and a status message is returned to inform the caller.
func (o *Options) lookupRpmDiff(ctx context.Context, fromPullSpec, toPullSpec string) (*changelog.RpmDiff, string) {
	if o.extractor == nil {
		return nil, ""
	}

	fromSnap, fromOK := o.extractor.Get(ctx, fromPullSpec)
	toSnap, toOK := o.extractor.Get(ctx, toPullSpec)

	fromMissing := !fromOK || fromSnap.RPMList == nil
	toMissing := !toOK || toSnap.RPMList == nil

	// Trigger background extraction for any release missing RPM data
	if fromMissing {
		go func() {
			if err := o.extractor.Extract(context.Background(), fromPullSpec); err != nil {
				klog.Warningf("Background extraction failed for %s: %v", fromPullSpec, err)
			}
		}()
	}
	if toMissing {
		go func() {
			if err := o.extractor.Extract(context.Background(), toPullSpec); err != nil {
				klog.Warningf("Background extraction failed for %s: %v", toPullSpec, err)
			}
		}()
	}

	if fromMissing || toMissing {
		return nil, "RPM data extraction in progress, retry in ~60 seconds for rpmDiff"
	}

	return changelog.ComputeRpmDiff(fromSnap.RPMList.Packages, toSnap.RPMList.Packages), ""
}

// fetchPullSpecs fetches PullSpecs for the given release versions
// Returns empty strings if the PullSpecs cannot be found
func (o *Options) fetchPullSpecs(ctx context.Context, from, to string, product rc.Product, arch rc.Architecture) (fromPullSpec, toPullSpec string) {
	if o.rcClient == nil {
		klog.V(4).Info("Release controller client not initialized, skipping PullSpec fetch")
		return "", ""
	}

	client, err := o.rcClient.GetClient(product, arch)
	if err != nil {
		klog.V(4).Infof("Failed to get client for %s/%s: %v", product, arch, err)
		return "", ""
	}

	// Fetch PullSpec for "from" version
	fromPS, err := client.GetPullSpec(ctx, from)
	if err != nil {
		klog.V(4).Infof("Failed to get PullSpec for from version %s (arch=%s): %v", from, arch, err)
	} else {
		fromPullSpec = fromPS
	}

	// Fetch PullSpec for "to" version
	toPS, err := client.GetPullSpec(ctx, to)
	if err != nil {
		klog.V(4).Infof("Failed to get PullSpec for to version %s (arch=%s): %v", to, arch, err)
	} else {
		toPullSpec = toPS
	}

	return fromPullSpec, toPullSpec
}

// formatBytes converts bytes to a human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func (o *Options) handleKernel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	query := r.URL.Query()
	release := query.Get("release")
	arch := query.Get("arch")
	product, ok := parseProduct(query.Get("product"))
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error": "invalid product '%s': must be 'ocp' or 'origin'"}`+"\n", query.Get("product"))
		return
	}

	if release == "" || arch == "" {
		w.WriteHeader(http.StatusBadRequest)
		if release == "" && arch == "" {
			fmt.Fprintf(w, `{"error": "missing required query parameters 'release' and 'arch'"}`+"\n")
		} else if release == "" {
			fmt.Fprintf(w, `{"error": "missing required query parameter 'release'"}`+"\n")
		} else {
			fmt.Fprintf(w, `{"error": "missing required query parameter 'arch'"}`+"\n")
		}
		return
	}

	if !isValidArch(arch) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error": "invalid architecture '%s': must be one of 'amd64', 'arm64', 's390x', 'ppc64le', or 'multi'"}`+"\n", arch)
		return
	}

	if _, err := semver.Parse(release); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error": "'release' parameter is not a valid semantic version: %s"}`+"\n", release)
		return
	}

	if o.rcClient == nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error": "release controller client not initialized"}`+"\n")
		return
	}

	client, err := o.rcClient.GetClient(product, rc.Architecture(arch))
	if err != nil {
		klog.V(4).Infof("Failed to get client for %s/%s: %v", string(product), arch, err)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error": "unsupported product/architecture combination '%s/%s'"}`+"\n", string(product), arch)
		return
	}

	pullSpec, err := client.GetPullSpec(r.Context(), release)
	if err != nil || pullSpec == "" {
		klog.V(4).Infof("Failed to get PullSpec for release %s (product=%s, arch=%s): %v", release, string(product), arch, err)
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"error": "could not find release '%s' for product '%s' and architecture '%s'"}`+"\n", release, string(product), arch)
		return
	}

	if o.extractor == nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error": "release extractor not initialized"}`+"\n")
		return
	}

	snap, ok := o.extractor.Get(r.Context(), pullSpec)
	if !ok || snap.RPMList == nil {
		// Extraction takes ~60s (rpmdb is slow), which exceeds the default
		// HAProxy timeout (30s). Start it in the background so the client
		// gets an immediate 202 instead of a 504 Gateway Timeout.
		go func() {
			if err := o.extractor.Extract(context.Background(), pullSpec); err != nil {
				klog.Warningf("Background extraction failed for %s: %v", pullSpec, err)
			}
		}()

		w.WriteHeader(http.StatusAccepted)
		accepted := struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Release string `json:"release"`
			Arch    string `json:"arch"`
		}{
			Status:  "accepted",
			Message: "RPM data extraction in progress, please retry in ~60 seconds",
			Release: release,
			Arch:    arch,
		}
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.Encode(accepted)
		return
	}

	kernel := snap.RPMList.Packages["kernel"]
	kernelRT := snap.RPMList.Packages["kernel-rt"]
	if kernelRT == "" && snap.RPMList.Extensions != nil {
		kernelRT = snap.RPMList.Extensions["kernel-rt"]
	}

	if kernel == "" && kernelRT == "" {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"error": "no kernel package found in RPM data for release '%s'"}`+"\n", release)
		return
	}

	response := struct {
		Release  string `json:"release"`
		Arch     string `json:"arch"`
		Product  string `json:"product"`
		Kernel   string `json:"kernel,omitempty"`
		KernelRT string `json:"kernelRT,omitempty"`
	}{
		Release:  release,
		Arch:     arch,
		Product:  string(product),
		Kernel:   kernel,
		KernelRT: kernelRT,
	}

	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(response); err != nil {
		klog.Errorf("Failed to encode kernel response: %v", err)
	}
}

func (o *Options) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "Release Payload Changelog Controller Status\n")
	fmt.Fprintf(w, "==========================================\n\n")

	if o.cache == nil {
		fmt.Fprintf(w, "Cache: Not initialized\n")
		return
	}

	// Get cache statistics
	stats := o.cache.Stats(r.Context())

	fmt.Fprintf(w, "In-Memory Cache:\n")
	fmt.Fprintf(w, "  Entries: %d\n", stats.MemoryEntries)
	fmt.Fprintf(w, "  Size: %s (%d bytes)\n", formatBytes(stats.MemorySizeBytes), stats.MemorySizeBytes)
	fmt.Fprintf(w, "  Hits: %d\n", stats.Hits)
	fmt.Fprintf(w, "  Misses: %d\n", stats.Misses)
	fmt.Fprintf(w, "  Hit Rate: %.2f%%\n", stats.HitRate)
	fmt.Fprintf(w, "\n")

	if len(stats.StorageBackends) > 0 {
		fmt.Fprintf(w, "Storage Backends:\n")
		for i, backend := range stats.StorageBackends {
			fmt.Fprintf(w, "  Backend %d:\n", i+1)
			fmt.Fprintf(w, "    Type: %s\n", backend.Type)
			if backend.Error != "" {
				fmt.Fprintf(w, "    Status: Error - %s\n", backend.Error)
			} else {
				fmt.Fprintf(w, "    Entries: %d\n", backend.Entries)
			}
		}
	} else {
		fmt.Fprintf(w, "Storage Backends: None configured\n")
	}

	fmt.Fprintf(w, "\n")

	if o.controller != nil {
		queueStats := o.controller.QueueStats()
		fmt.Fprintf(w, "Work Queue:\n")
		fmt.Fprintf(w, "  Depth: %d\n", queueStats.Depth)
		fmt.Fprintf(w, "  Pending: %d\n", queueStats.Pending)
		fmt.Fprintf(w, "  Processing: %d\n", queueStats.Processing)

		if len(queueStats.Items) > 0 {
			now := time.Now()
			fmt.Fprintf(w, "\n  Items:\n")
			for i, item := range queueStats.Items {
				fmt.Fprintf(w, "    %d. [%s] %s -> %s\n", i+1, item.State, item.Request.From, item.Request.To)
				fmt.Fprintf(w, "       Enqueued: %s ago\n", now.Sub(item.EnqueuedAt).Truncate(time.Second))
				if item.State == QueueItemProcessing {
					fmt.Fprintf(w, "       Processing for: %s\n", now.Sub(item.StartedAt).Truncate(time.Second))
				}
			}
		}
	} else {
		fmt.Fprintf(w, "Work Queue: Not initialized\n")
	}
}

func (o *Options) Run(ctx context.Context) error {
	inClusterConfig := o.controllerContext.KubeConfig

	kubeClient, err := kubernetes.NewForConfig(inClusterConfig)
	if err != nil {
		return fmt.Errorf("can't build kubernetes client: %w", err)
	}

	// Batch Job Informers
	kubeFactory := informers.NewSharedInformerFactory(kubeClient, controllerDefaultResyncDuration)

	// Initialize storage backends based on configuration
	storages, cleanupFuncs, err := o.initializeStorageBackends(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize storage backends: %w", err)
	}
	// Ensure cleanup of storage backends on exit
	defer func() {
		for _, cleanup := range cleanupFuncs {
			cleanup()
		}
	}()

	o.cache = NewChangelogCache(storages...)

	// Initialize release-controller client
	klog.Info("Initializing release-controller client")
	rcMultiClient, err := rc.NewMultiClient(rc.DefaultClientConfig())
	if err != nil {
		klog.Warningf("Failed to initialize release-controller client: %v", err)
		klog.Warning("PullSpec information will not be available in changelog responses")
	} else {
		o.rcClient = rcMultiClient
		klog.Info("Release-controller client initialized successfully")
		// Ensure cleanup on exit
		defer func() {
			if err := o.rcClient.Close(); err != nil {
				klog.Warningf("Failed to close release-controller client: %v", err)
			}
		}()
	}

	// Initialize snapshot storage for per-release extraction.
	// Firestore+GCS is preferred when enabled; filesystem is the fallback.
	var snapshotStorage storage.ReleaseSnapshotStorage
	if o.firestoreEnabled {
		klog.Infof("Initializing Firestore+GCS snapshot storage (project=%s, collection=%s, bucket=%s)",
			o.firestoreProjectID, o.firestoreSnapshotCollectionID, o.firestoreGCSBucket)
		ss, err := firestore.NewSnapshotStorage(ctx, firestore.SnapshotConfig{
			ProjectID:       o.firestoreProjectID,
			DatabaseID:      o.firestoreDatabaseID,
			CollectionID:    o.firestoreSnapshotCollectionID,
			BucketName:      o.firestoreGCSBucket,
			ListConcurrency: o.cacheWarmingConcurrency,
		})
		if err != nil {
			klog.Warningf("Failed to initialize Firestore+GCS snapshot storage: %v", err)
		} else {
			snapshotStorage = ss
			cleanupFuncs = append(cleanupFuncs, func() {
				if err := ss.Close(); err != nil {
					klog.Warningf("Failed to close Firestore+GCS snapshot storage: %v", err)
				}
			})
			klog.Info("Firestore+GCS snapshot storage initialized successfully")
		}
	}
	if snapshotStorage == nil && o.filesystemEnabled {
		snapshotDir := filepath.Join(o.filesystemBaseDir, "snapshots")
		ss, err := filesystem.NewSnapshotStorage(ctx, snapshotDir)
		if err != nil {
			klog.Warningf("Failed to initialize filesystem snapshot storage: %v", err)
		} else {
			snapshotStorage = ss
			klog.Infof("Filesystem snapshot storage initialized at %s", snapshotDir)
		}
	}

	o.extractor = NewReleaseExtractor(snapshotStorage, o.maxConcurrentExtractions)

	// Release Payload Changelog Controller
	releasePayloadChangelogController := NewReleasePayloadChangelogController(o.controllerContext.EventRecorder, o.cache)
	o.controller = releasePayloadChangelogController

	// Start the HTTP server before cache warming so API requests during
	// warming can fall through to storage backends. Health probes target
	// port 8443 (library-go's GenericAPIServer), which starts even earlier.
	httpServerErrors := make(chan error, 1)
	go func() {
		if err := o.runHTTPServer(ctx); err != nil {
			httpServerErrors <- err
		}
	}()

	// Warm cache from storage backends if enabled
	if o.cacheWarmingEnabled {
		if err := o.warmCache(ctx, storages); err != nil {
			klog.Warningf("Failed to warm cache: %v", err)
		}
		if err := o.extractor.WarmCache(ctx); err != nil {
			klog.Warningf("Failed to warm snapshot cache: %v", err)
		}
	}

	// Start the informers
	kubeFactory.Start(ctx.Done())

	// Run the Controllers
	go releasePayloadChangelogController.RunWorkers(ctx, o.workerCount)

	// Start the release watcher if enabled and client is available
	if o.watcherEnabled && o.rcClient != nil {
		watcher := NewReleaseWatcher(o.rcClient, releasePayloadChangelogController, o.extractor, o.watcherPollInterval)
		go watcher.Run(ctx)
	} else if o.watcherEnabled {
		klog.Warning("Release watcher enabled but release-controller client not available, skipping")
	}

	// Wait for context cancellation or HTTP server error
	select {
	case <-ctx.Done():
		klog.Info("Context cancelled, shutting down...")
		return nil
	case err := <-httpServerErrors:
		return fmt.Errorf("HTTP server failed: %w", err)
	}
}
