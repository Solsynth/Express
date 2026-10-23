package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/service"
	gen "src.solsynth.dev/sosys/go/proto"
)

// consoleTestDirectory is a Sphere stand-in that reports one publisher
// membership, so the console session route has something to enumerate.
type consoleTestDirectory struct {
	accountID    string
	publisher    *gen.DyPublisher
	membership   gen.DyPublisherMemberRole
	superuser    bool
	enumerations int
}

func (d *consoleTestDirectory) Authenticate(_ context.Context, token string) (string, error) {
	if token != "sphere-token" {
		return "", service.ErrUnauthorized
	}
	return d.accountID, nil
}
func (d *consoleTestDirectory) AuthenticateAccount(ctx context.Context, token string) (service.AuthenticatedAccount, error) {
	accountID, err := d.Authenticate(ctx, token)
	if err != nil {
		return service.AuthenticatedAccount{}, err
	}
	return service.AuthenticatedAccount{
		ID:          accountID,
		IsSuperuser: d.superuser,
		Name:        "alice",
		Nick:        "Alice",
		PictureURL:  "https://cdn.example.test/alice.png",
	}, nil
}
func (d *consoleTestDirectory) GetPublisher(context.Context, string) (*gen.DyPublisher, error) {
	return d.publisher, nil
}
func (d *consoleTestDirectory) IsPublisherMember(context.Context, string, string, gen.DyPublisherMemberRole) (bool, error) {
	return true, nil
}
func (d *consoleTestDirectory) PublisherMemberships(context.Context, string) ([]service.PublisherMembership, error) {
	d.enumerations++
	return []service.PublisherMembership{{Publisher: d.publisher, Role: d.membership}}, nil
}

// grantedPermissions answers only for the nodes it lists.
type grantedPermissions map[string]bool

func (g grantedPermissions) HasPermission(_ context.Context, _ string, key string) (bool, error) {
	return g[key], nil
}

func newConsoleServer(t *testing.T, granted grantedPermissions) (*Server, *consoleTestDirectory) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:http-console-test-"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&database.Product{}, &database.Channel{}, &database.Release{}, &database.ReleaseArtifact{}, &database.ClientCheck{}, &database.Localization{}); err != nil {
		t.Fatal(err)
	}
	directory := &consoleTestDirectory{
		accountID:  uuid.NewString(),
		publisher:  &gen.DyPublisher{Id: uuid.NewString(), Name: "acme", Nick: "Acme"},
		membership: gen.DyPublisherMemberRole_DY_EDITOR,
	}
	releases := service.NewPublisherReleaseService(db, directory, nil, nil)
	releases.SetPermissionChecker(granted)
	server := New(config.Default())
	RegisterPublisherRoutes(server.Engine, releases, directory, config.Default())
	return server, directory
}

func readConsoleSession(t *testing.T, server *Server, token string) (consoleSessionView, int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/console/session", nil)
	if token != "" {
		request.AddCookie(&http.Cookie{Name: "AuthToken", Value: token})
	}
	response := httptest.NewRecorder()
	server.Engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		return consoleSessionView{}, response.Code
	}
	var view consoleSessionView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return view, response.Code
}

func TestConsoleSessionReportsAccountPublishersAndPermissions(t *testing.T) {
	server, directory := newConsoleServer(t, grantedPermissions{
		service.PermissionReleasesManage:  true,
		service.PermissionArtifactsUpload: true,
	})

	view, status := readConsoleSession(t, server, "sphere-token")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if view.Account.ID != directory.accountID || view.Account.Name != "alice" || view.Account.Nick != "Alice" {
		t.Fatalf("account = %+v", view.Account)
	}
	if view.Account.Picture != "https://cdn.example.test/alice.png" {
		t.Fatalf("picture = %q", view.Account.Picture)
	}
	if view.Account.IsSuperuser {
		t.Fatal("a regular account was reported as superuser")
	}
	if len(view.Publishers) != 1 {
		t.Fatalf("publishers = %+v", view.Publishers)
	}
	if view.Publishers[0].Name != "acme" || view.Publishers[0].Nick != "Acme" || view.Publishers[0].Role != "editor" {
		t.Fatalf("publisher = %+v", view.Publishers[0])
	}
	// Only the granted nodes are advertised: the console hides a control it
	// cannot operate rather than letting it fail with 403.
	if len(view.Permissions) != 2 {
		t.Fatalf("permissions = %v", view.Permissions)
	}
	for _, permission := range view.Permissions {
		if permission != service.PermissionReleasesManage && permission != service.PermissionArtifactsUpload {
			t.Fatalf("unexpected permission %q", permission)
		}
	}
	if directory.enumerations != 1 {
		t.Fatalf("membership enumerations = %d, want 1", directory.enumerations)
	}
}

func TestConsoleSessionSuperuserReceivesEveryNode(t *testing.T) {
	server, directory := newConsoleServer(t, grantedPermissions{})
	directory.superuser = true

	view, status := readConsoleSession(t, server, "sphere-token")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !view.Account.IsSuperuser {
		t.Fatal("superuser flag was dropped")
	}
	// The server bypasses the permission check for a superuser, so an empty
	// grant list must not hide the console's controls.
	if len(view.Permissions) != len(consolePermissions) {
		t.Fatalf("permissions = %v, want all %d", view.Permissions, len(consolePermissions))
	}
}

func TestConsoleSessionRejectsAnonymousCallers(t *testing.T) {
	server, _ := newConsoleServer(t, grantedPermissions{})

	view, status := readConsoleSession(t, server, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
	if view.Account.ID != "" {
		t.Fatalf("anonymous response leaked an account: %+v", view.Account)
	}

	if _, status := readConsoleSession(t, server, "wrong-token"); status != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d, want 401", status)
	}
}
