package backend

import (
	"errors"
	"net/http"

	"schakker_shared/webapp"

	"tubes1_wbd_admin/backend/api"
)

type managedHandler struct {
	http.Handler
	runtime *runtime
}

func (h *managedHandler) Close() error { return h.runtime.Close() }
func NewHandler(root string) (http.Handler, error) {
	services, runtime, err := servicesFromEnvironment(root)
	if err != nil {
		return nil, err
	}
	handler, err := NewHandlerWithServices(root, services)
	if err != nil {
		runtime.Close()
		return nil, err
	}
	return &managedHandler{Handler: handler, runtime: runtime}, nil
}
func NewHandlerWithServices(root string, services Services) (http.Handler, error) {
	if services.Authentication == nil || services.Access == nil || services.Connections == nil {
		return nil, errors.New("access and connection-configuration services are required")
	}
	return webapp.New(webapp.Config{
		Root: root, BuildWorkspace: "schakker-admin-frontend",
		API:     api.NewHandler(services.Authentication, services.Access, services.Connections, services.Production),
		HasPage: api.HasPage,
	})
}
