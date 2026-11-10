package integration_test

import (
	"testing"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/controlplane"
)

func TestControlPlaneOutageFailsClosedWithoutStalePermissions(t *testing.T) {
	f := newFixture(t, true)
	target := f.register(t)
	f.grant(t, f.editor, target.ID, access.ViewEdit)
	_, raw := f.login(t, "it.editor")
	_, err := f.databases.Schemas(f.ctx, f.editor, target.ID)
	must(t, err)
	f.private.Close()
	_, _, err = f.userAccess.Authenticate(f.ctx, raw)
	wantError(t, err, controlplane.ErrUnavailable)
	_, err = f.databases.Schemas(f.ctx, f.editor, target.ID)
	wantError(t, err, controlplane.ErrUnavailable)
	response := apiRequest(t, userHandler(t, f), "GET", "/dashboard", raw, nil)
	if response.Code != 503 {
		t.Fatalf("authority outage returned %d", response.Code)
	}
}
