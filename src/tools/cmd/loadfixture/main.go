// loadfixture provisions isolated databases and REAL services for k6. It is part
// of the tools module and is never linked into either production server.
package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	adminschema "schakker_schemas/admin"
	"schakker_shared/observability"
	fixtures "schakker_tests/fixtures/setup"
	"tubes1_wbd/backend"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/api"
	"tubes1_wbd/backend/controlplane"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/backend/migration"
	"tubes1_wbd/backend/migration/engines"
	"tubes1_wbd/backend/worker"
	"tubes1_wbd/database/dao"
	adminaccess "tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/connectionconfig"
	"tubes1_wbd_admin/backend/internalapi"
	admindao "tubes1_wbd_admin/database/dao"
)

type target struct {
	id       int64
	lease    chan struct{}
	mu       sync.Mutex
	review   int64
	reserved time.Time
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	listen := flag.String("listen", "127.0.0.1:3300", "Loopback-only test API listener")
	manifest := flag.String("manifest", "", "Private output manifest for k6")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || *manifest == "" {
		return errors.New("loadfixture requires a loopback listener and -manifest path")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdown, err := observability.Configure("schakker-load-fixture", nil, nil)
	if err != nil {
		return err
	}
	defer func() { c, s := context.WithTimeout(context.Background(), 10*time.Second); defer s(); shutdown(c) }()
	directory, err := os.MkdirTemp("", "schakker-k6-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	factory, err := fixtures.Open(ctx, directory)
	if err != nil {
		return err
	}
	defer factory.Close()
	dbs, err := factory.Clone(ctx, "load")
	if err != nil {
		return err
	}
	defer dbs.Close(context.Background())
	const adminName = "schakker_load_admin"
	if _, err = factory.Cluster.EnsureDatabase(ctx, adminName, "", ""); err != nil {
		return err
	}
	adminDB, err := factory.Cluster.Open(ctx, adminName)
	if err != nil {
		return err
	}
	defer adminDB.Close()
	if err = adminschema.Initialize(ctx, adminDB); err != nil {
		return err
	}
	adminStore, err := admindao.New(adminDB)
	if err != nil {
		return err
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return err
	}
	credential := make([]byte, 32)
	if _, err = rand.Read(credential); err != nil {
		return err
	}
	controlToken := hex.EncodeToString(credential)
	authority, err := adminaccess.NewService(adminStore, adminaccess.DefaultConfig(key))
	if err != nil {
		return err
	}
	password := "Fixture-" + hex.EncodeToString(key[:16]) + "!"
	hash, err := adminaccess.HashPassword(password)
	if err != nil {
		return err
	}
	if err = adminStore.CreateAdmin(ctx, "load.admin", "Load fixture administrator", hash); err != nil {
		return err
	}
	admin, err := adminStore.AdminCredentials(ctx, "load.admin")
	if err != nil {
		return err
	}
	if err = authority.CreateUser(ctx, admin.ID, "load.editor", "Load fixture editor", password); err != nil {
		return err
	}
	editor, err := adminStore.Credentials(ctx, "load.editor")
	if err != nil {
		return err
	}
	if err = authority.SaveGroup(ctx, admin.ID, 0, "Load editors", "Disposable load-test group"); err != nil {
		return err
	}
	if err = authority.SaveGroup(ctx, admin.ID, 0, "Load auditors", "View Only approval group"); err != nil {
		return err
	}
	groups, err := adminStore.Groups(ctx)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if err = authority.SetMembership(ctx, admin.ID, editor.ID, g.ID, true); err != nil {
			return err
		}
		if err = authority.SetGroupLeader(ctx, admin.ID, g.ID, editor.ID); err != nil {
			return err
		}
	}
	privateHandler, err := internalapi.NewHandler(adminStore, key, controlToken)
	if err != nil {
		return err
	}
	private := httptest.NewServer(privateHandler)
	defer private.Close()
	client, err := controlplane.New(private.URL, controlToken, false)
	if err != nil {
		return err
	}
	store, err := dao.New(dbs.Website)
	if err != nil {
		return err
	}
	if err = store.RememberUser(ctx, dao.User{ID: editor.ID, Username: editor.Username, DisplayName: editor.DisplayName, Active: true}); err != nil {
		return err
	}
	accessConfig := access.DefaultConfig(key)
	accessConfig.Authority = client
	auth, err := access.NewService(store, accessConfig)
	if err != nil {
		return err
	}
	management, err := dbmanagement.NewService(store, auth, dbmanagement.DefaultConfig(key, false))
	if err != nil {
		return err
	}
	connections, err := connectionconfig.NewService(adminStore, authority, connectionconfig.DefaultConfig(key, false))
	if err != nil {
		return err
	}
	migrations, err := migration.NewService(store, auth, management)
	if err != nil {
		return err
	}
	jobs := backend.ConfigureExecution(store, auth, management, migrations)
	policy := migrations.Workflow()
	targets := make([]*target, 0, 3)
	for i := 0; i < 3; i++ {
		managed := dbs
		if i > 0 {
			managed, err = factory.Clone(ctx, "load"+strconv.Itoa(i))
			if err != nil {
				return err
			}
			defer managed.Close(context.Background())
		}
		if _, err = managed.Managed.ExecContext(ctx, `CREATE TABLE public.load_counters(id bigint PRIMARY KEY,value bigint NOT NULL); INSERT INTO public.load_counters SELECT n,0 FROM generate_series(1,1000) n`); err != nil {
			return err
		}
		input := managed.Connection()
		db, err := connections.SaveConnection(ctx, admin.ID, 0, connectionconfig.ConnectionInput{DBMS: input.DBMS, Name: input.Name + strconv.Itoa(i), Host: input.Host, Port: input.Port, DatabaseName: input.DatabaseName, Username: input.Username, Password: input.Password, SSLMode: input.SSLMode, Active: true})
		if err != nil {
			return err
		}
		if err = store.RememberDatabase(ctx, db.ID); err != nil {
			return err
		}
		for n, g := range groups {
			permission := admindao.ViewEdit
			if n > 0 {
				permission = admindao.ViewOnly
			}
			if err = authority.SetGrant(ctx, admin.ID, g.ID, db.ID, permission); err != nil {
				return err
			}
		}
		t := &target{id: db.ID, lease: make(chan struct{}, 1)}
		t.lease <- struct{}{}
		targets = append(targets, t)
	}
	stopPool := observability.DefaultMetrics.RegisterPool("platform", dbs.Website)
	defer stopPool()
	stopAdminPool := observability.DefaultMetrics.RegisterPool("admin", adminDB)
	defer stopAdminPool()
	stopQueue := observability.DefaultMetrics.RegisterQueue(store.QueueMetrics)
	defer stopQueue()
	metricsAddress := os.Getenv("SCHAKKER_METRICS_ADDR")
	if metricsAddress == "" {
		metricsAddress = "127.0.0.1:9464"
	}
	stopMetrics, err := observability.StartMetrics(ctx, metricsAddress)
	if err != nil {
		return err
	}
	defer stopMetrics()
	runner := worker.New(store, policy, management, "load-fixture-worker")
	runner.Concurrency = 4
	workerDone := make(chan error, 1)
	workerCtx, cancelWorker := context.WithCancel(ctx)
	go func() { workerDone <- runner.Run(workerCtx) }()
	defer func() { cancelWorker(); <-workerDone }()
	var sequence atomic.Uint64
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", api.NewHandler(auth, management, migrations, false, jobs)))
	mux.HandleFunc("/fixture/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	// A per-target fixture lease prepares fresh approved content at the current
	// generation. This does not bypass production approval or execution policy.
	mux.HandleFunc("/fixture/next", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		if subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), []byte(controlToken)) != 1 {
			w.WriteHeader(401)
			return
		}
		index, e := strconv.Atoi(r.URL.Query().Get("target"))
		if e != nil || index < 0 || index >= len(targets) {
			http.Error(w, "invalid fixture target", 400)
			return
		}
		t := targets[index]
		select {
		case <-t.lease:
		default:
			http.Error(w, "target reserved", 409)
			return
		}
		reserved := false
		defer func() {
			if !reserved {
				t.lease <- struct{}{}
			}
		}()
		n := sequence.Add(1)
		up := fmt.Sprintf("CREATE TABLE public.load_%d(id bigint)", n)
		down := fmt.Sprintf("DROP TABLE public.load_%d", n)
		if n%3 == 0 {
			up, down, e = engines.BatchSQL(engines.BatchSpec{Schema: "public", Table: "load_counters", Key: "id", Column: "value", Delta: 1, Size: 100})
			if e != nil {
				http.Error(w, "template unavailable", 500)
				return
			}
		}
		revision, e := policy.CreateDefinition(r.Context(), editor.ID, t.id, migration.DefinitionInput{Title: fmt.Sprintf("Load migration %d", n), UpSQL: up, DownSQL: down})
		if e != nil {
			http.Error(w, "definition unavailable", 503)
			return
		}
		state, e := policy.State(r.Context(), editor.ID, t.id)
		if e != nil {
			http.Error(w, "state unavailable", 503)
			return
		}
		review, e := policy.SubmitReview(r.Context(), editor.ID, migration.ReviewInput{RevisionID: revision.ID, Action: "up", ExpectedGeneration: state.Generation})
		if e != nil {
			http.Error(w, "review unavailable", 503)
			return
		}
		for _, g := range groups {
			review, e = policy.Decide(r.Context(), editor.ID, review.ID, g.ID, "approve", "Disposable k6 fixture approval")
			if e != nil {
				http.Error(w, "approval unavailable", 503)
				return
			}
		}
		t.mu.Lock()
		t.review = review.ID
		t.reserved = time.Now()
		t.mu.Unlock()
		reserved = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"review_id": review.ID, "database_id": t.id, "revision_id": revision.ID})
	})
	sweepDone := make(chan struct{})
	sweepCtx, cancelSweep := context.WithCancel(ctx)
	go func() {
		defer close(sweepDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-sweepCtx.Done():
				return
			case <-ticker.C:
			}
			for _, t := range targets {
				t.mu.Lock()
				id, at := t.review, t.reserved
				t.mu.Unlock()
				if id == 0 {
					continue
				}
				review, e := store.Review(sweepCtx, id, false)
				if e != nil {
					continue
				}
				release := review.QueuedJobID == nil && time.Since(at) > 30*time.Second
				if review.QueuedJobID != nil {
					j, e := store.Job(sweepCtx, *review.QueuedJobID)
					release = e == nil && (j.Status == "succeeded" || j.Status == "failed" || j.Status == "reverted")
				}
				if release {
					t.mu.Lock()
					if t.review == id {
						t.review = 0
						t.lease <- struct{}{}
					}
					t.mu.Unlock()
				}
			}
		}
	}()
	defer func() { cancelSweep(); <-sweepDone }()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: observability.Middleware(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second}
	defer func() {
		c, s := context.WithTimeout(context.Background(), 10*time.Second)
		defer s()
		server.Shutdown(c)
	}()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	ids := []int64{}
	for _, t := range targets {
		ids = append(ids, t.id)
	}
	raw, err := json.MarshalIndent(map[string]any{"base_url": "http://" + listener.Addr().String(), "username": editor.Username, "password": password, "control_token": controlToken, "database_ids": ids}, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(*manifest), 0700); err != nil {
		return err
	}
	if err = os.WriteFile(*manifest, raw, 0600); err != nil {
		return err
	}
	defer os.Remove(*manifest)
	observability.Event(ctx, "load_fixture.ready", nil, "target_count", len(targets))
	select {
	case <-ctx.Done():
		return nil
	case err = <-serverDone:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
