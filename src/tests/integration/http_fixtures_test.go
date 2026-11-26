package integration_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"tubes1_wbd/backend"
	adminbackend "tubes1_wbd_admin/backend"
)

// Backend API tests need the SPA-serving boundary, but do not depend on a
// production frontend bundle. Browser integration tests build the actual SPAs.
func httpApplicationRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "frontend", "dist")
	must(t, os.MkdirAll(directory, 0700))
	must(t, os.WriteFile(filepath.Join(directory, "index.html"), []byte(`<div id="root"></div>`), 0600))
	return root
}
func userHandler(t *testing.T, f *fixture) http.Handler {
	t.Helper()
	handler, err := backend.NewHandlerWithServices(httpApplicationRoot(t), backend.Services{Access: f.userAccess, Databases: f.databases, Migrations: f.migrations})
	must(t, err)
	return handler
}
func adminHandler(t *testing.T, f *fixture) http.Handler {
	t.Helper()
	handler, err := adminbackend.NewHandlerWithServices(httpApplicationRoot(t), adminbackend.Services{Authentication: f.adminAuth, Access: f.adminAuth, Connections: f.connections})
	must(t, err)
	return handler
}
