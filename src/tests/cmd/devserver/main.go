// devserver explicitly provisions local development and launches the actual
// admin, API and worker binaries. Normal application startup remains read-only.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	fixtures "schakker_tests/fixtures/setup"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Local development:", err)
		os.Exit(1)
	}
}

func run() error {
	rootFlag := flag.String("root", ".", "Repository root")
	check := flag.Bool("check", false, "Verify real login/approval/worker/rollback, then stop")
	stop := flag.Bool("stop", false, "Stop this checkout's development instance")
	flag.Parse()
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		return err
	}
	state := filepath.Join(root, "src", ".schakker", "dev")
	if *stop {
		return requestStop(state)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "go.work")); err != nil {
		return errors.New("-root must point to the Schakker repository")
	}
	if err := availablePorts(); err != nil {
		return err
	}
	if err := os.MkdirAll(state, 0700); err != nil {
		return err
	}
	config, err := readConfiguration(state)
	if err != nil {
		return err
	}
	var runningID string
	defer func() {
		if runningID != "" {
			os.Remove(filepath.Join(state, "running.id"))
		}
	}()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	setupCtx, finishSetup := context.WithTimeout(ctx, 2*time.Minute)
	defer finishSetup()
	fmt.Println("Preparing private development PostgreSQL databases...")
	cluster, err := fixtures.StartLocalCluster(setupCtx, filepath.Join(state, "cluster"), fixtures.LocalOptions{TCPRole: targetRole})
	if err != nil {
		return err
	}
	defer cluster.Close()
	if err := provision(setupCtx, state, cluster, config); err != nil {
		return err
	}
	adminURL, err := roleURL(cluster, "schakker_dev_admin", fixtures.AdminAuthenticationRole)
	if err != nil {
		return err
	}
	userURL, err := roleURL(cluster, "schakker_dev_platform", fixtures.UserRuntimeRole)
	if err != nil {
		return err
	}
	common := map[string]string{
		"SCHAKKER_HTTP_HOST":      "127.0.0.1",
		"SCHAKKER_INTERNAL_TOKEN": config.Token,
		"SCHAKKER_ENV":            "development", "SCHAKKER_ADMIN_ENV": "development",
		"SCHAKKER_ADMIN_INTERNAL_ADDR":     "127.0.0.1:3002",
		"SCHAKKER_ADMIN_INTERNAL_TLS_CERT": "", "SCHAKKER_ADMIN_INTERNAL_TLS_KEY": "",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "", "SCHAKKER_METRICS_TOKEN": "",
	}
	adminEnv := cloneEnv(common)
	adminEnv["SCHAKKER_ADMIN_DATABASE_URL"] = adminURL
	adminEnv["SCHAKKER_ADMIN_MASTER_KEY"] = config.Key
	adminEnv["SCHAKKER_ADMIN_PORT"] = "3001"
	adminEnv["SCHAKKER_METRICS_ADDR"] = "127.0.0.1:9465"
	userEnv := cloneEnv(common)
	userEnv["SCHAKKER_PLATFORM_DATABASE_URL"] = userURL
	userEnv["SCHAKKER_MASTER_KEY"] = config.Key
	userEnv["SCHAKKER_CONTROL_PLANE_URL"] = "http://127.0.0.1:3002"
	userEnv["PORT"] = "3000"
	userEnv["SCHAKKER_METRICS_ADDR"] = "127.0.0.1:9464"
	workerEnv := cloneEnv(userEnv)
	workerEnv["SCHAKKER_METRICS_ADDR"] = "127.0.0.1:9466"
	workerEnv["SCHAKKER_WORKER_CONCURRENCY"] = "2"
	workerEnv["SCHAKKER_WORKER_LEASE_SECONDS"] = "30"
	workerEnv["SCHAKKER_WORKER_TIMEOUT_SECONDS"] = "1800"
	children := []*child{}
	defer func() {
		for i := len(children) - 1; i >= 0; i-- {
			children[i].close()
		}
	}()
	for _, spec := range []struct {
		name, binary, directory, ready string
		env                            map[string]string
	}{
		{"admin", "admin-server", "src-admin", "http://127.0.0.1:3001/api/session", adminEnv},
		{"user", "user-server", "src", "http://127.0.0.1:3000/api/session", userEnv},
		{"worker", "migration-worker", "src", "http://127.0.0.1:9466/metrics", workerEnv},
	} {
		process, err := launch(state, root, spec.name, spec.binary, spec.directory, spec.env)
		if err != nil {
			return err
		}
		children = append(children, process)
		if err := process.waitReady(ctx, spec.ready); err != nil {
			return err
		}
		fmt.Printf("%s ready\n", spec.name)
	}
	if *check {
		if err := smoke(ctx, config); err != nil {
			return err
		}
		fmt.Println("PASS: admin/editor/reader login, real approvals, queued worker execution and rollback.")
		return nil
	}
	runID := rand.Text()
	if err := os.WriteFile(filepath.Join(state, "running.id"), []byte(runID), 0600); err != nil {
		return err
	}
	runningID = runID
	fmt.Println("User app: http://localhost:3000")
	fmt.Println("Admin:    http://localhost:3001")
	fmt.Println("Login accounts and generated password:", filepath.Join(state, "credentials.txt"))
	fmt.Println("Service logs:", state)
	fmt.Println("Press Ctrl+C, or run .\\run-dev.bat -Stop, to stop development.")
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if raw, _ := os.ReadFile(filepath.Join(state, "stop.request")); string(raw) == runID {
				os.Remove(filepath.Join(state, "stop.request"))
				return nil
			}
			for _, process := range children {
				select {
				case <-process.done:
					return fmt.Errorf("%s stopped; inspect %s", process.name, process.log.Name())
				default:
				}
			}
		}
	}
}

func availablePorts() error {
	listeners := []net.Listener{}
	defer func() {
		for _, listener := range listeners {
			listener.Close()
		}
	}()
	for _, port := range []string{"3000", "3001", "3002", "9464", "9465", "9466"} {
		listener, err := net.Listen("tcp", "127.0.0.1:"+port)
		if err != nil {
			return fmt.Errorf("port %s is already in use; stop the previous Schakker instance before starting development", port)
		}
		listeners = append(listeners, listener)
	}
	return nil
}
func cloneEnv(values map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range values {
		result[key] = value
	}
	return result
}
func requestStop(state string) error {
	raw, err := os.ReadFile(filepath.Join(state, "running.id"))
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("Development is not running.")
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(state, "stop.request"), raw, 0600); err != nil {
		return err
	}
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(filepath.Join(state, "running.id")); errors.Is(err, os.ErrNotExist) {
			fmt.Println("Development stopped.")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("development did not acknowledge stop; inspect its terminal")
}
