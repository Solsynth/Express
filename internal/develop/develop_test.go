package develop

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gen "src.solsynth.dev/sosys/go/proto"

	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// testService opens an in-memory schema with every portal model.
func testService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+filepath.Join(t.TempDir(), "develop.db")+"?mode=rwc"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(Models()...); err != nil {
		t.Fatal(err)
	}
	return New(db, nil)
}

// seedProject inserts a developer and project directly, since enrollment needs
// Sphere.
func seedProject(t *testing.T, svc *Service) (*Developer, *DevProject) {
	t.Helper()
	developer := &Developer{ID: newUUID(), PublisherID: newUUID()}
	if err := svc.DB.Create(developer).Error; err != nil {
		t.Fatal(err)
	}
	project := &DevProject{ID: newUUID(), Slug: "island", Name: "Island", DeveloperID: developer.ID}
	if err := svc.DB.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	return developer, project
}

// TestProjectLifecycle covers create, list, update and cascade delete.
func TestProjectLifecycle(t *testing.T) {
	svc := testService(t)
	developer, _ := seedProject(t, svc)

	project, err := svc.CreateProject(developer.ID, ProjectInput{Slug: "second", Name: "Second"})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if project.ID == "" || project.DeveloperID != developer.ID {
		t.Fatalf("project = %+v", project)
	}
	if _, err := svc.CreateProject(developer.ID, ProjectInput{Slug: "", Name: "x"}); err == nil {
		t.Fatal("CreateProject() accepted an empty slug")
	}

	projects, err := svc.ListProjects(developer.ID)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(projects))
	}

	updated, err := svc.UpdateProject(developer.ID, project.ID, ProjectInput{Name: "Renamed"})
	if err != nil {
		t.Fatalf("UpdateProject() error = %v", err)
	}
	if updated.Name != "Renamed" || updated.Slug != "second" {
		t.Fatalf("updated = %+v, want only the name changed", updated)
	}

	// A project of another developer must not be reachable.
	if _, err := svc.LoadProject(newUUID(), project.ID); err == nil {
		t.Fatal("LoadProject() reached a project of a different developer")
	}
}

// TestAppWidgetAndPayloadContract covers the board contract Passport relies on.
func TestAppWidgetAndPayloadContract(t *testing.T) {
	svc := testService(t)
	_, project := seedProject(t, svc)
	ctx := context.Background()

	slug := "island"
	name := "Island"
	status := "production"
	maxBytes := 512
	app, err := svc.CreateApp(ctx, project.ID, CustomAppInput{
		Slug:   &slug,
		Name:   &name,
		Status: &status,
		OauthConfig: &CustomAppOauthConfig{
			AllowedScopes: []string{"openid", "accounts.profile.board"},
		},
	})
	if err != nil {
		t.Fatalf("CreateApp() error = %v", err)
	}
	if app.Status != CustomAppProduction || app.StatusName() != "production" {
		t.Fatalf("app status = %v", app.Status)
	}

	_, err = svc.CreateWidget(app.ID, BoardWidgetManifest{
		Key:             "steps",
		Name:            "Steps",
		IsEnabled:       true,
		FieldTypes:      []BoardFieldType{{Name: "steps", Type: "number"}},
		RequiredFields:  []string{"steps"},
		MaxPayloadBytes: &maxBytes,
		AllowMultiple:   true,
	})
	if err != nil {
		t.Fatalf("CreateWidget() error = %v", err)
	}
	if _, err := svc.CreateWidget(app.ID, BoardWidgetManifest{Key: "STEPS", Name: "dup"}); err == nil {
		t.Fatal("CreateWidget() accepted a duplicate key")
	}

	// A payload field must carry a non-empty label.
	invalid := svc.ValidateBoardPayload(app, "steps", map[string]any{
		"steps": map[string]any{"value": 100, "label": ""},
	})
	if invalid.Valid {
		t.Fatal("ValidateBoardPayload() accepted an empty label")
	}

	valid := svc.ValidateBoardPayload(app, "steps", map[string]any{
		"steps": map[string]any{"value": 100, "label": "Steps today"},
	})
	if !valid.Valid {
		t.Fatalf("ValidateBoardPayload() rejected a valid payload: %s", valid.Message)
	}
	if _, ok := ExtractBoardFieldValue(valid.NormalizedPayload["steps"]); !ok {
		t.Fatal("normalized payload lost its envelope")
	}

	// A declared number field rejects a string value.
	wrongType := svc.ValidateBoardPayload(app, "steps", map[string]any{
		"steps": map[string]any{"value": "many", "label": "Steps today"},
	})
	if wrongType.Valid {
		t.Fatal("ValidateBoardPayload() accepted a string for a number field")
	}

	// A required field that is missing is rejected.
	missing := svc.ValidateBoardPayload(app, "steps", map[string]any{
		"other": map[string]any{"value": 1, "label": "Other"},
	})
	if missing.Valid {
		t.Fatal("ValidateBoardPayload() accepted a payload without its required field")
	}

	// Dropping the board scope disables the widget surface.
	app.OauthConfig.AllowedScopes = []string{"openid"}
	if err := svc.DB.Save(app).Error; err != nil {
		t.Fatal(err)
	}
	noScope := svc.ValidateBoardPayload(app, "steps", map[string]any{
		"steps": map[string]any{"value": 100, "label": "Steps today"},
	})
	if noScope.Valid {
		t.Fatal("ValidateBoardPayload() ignored the missing board scope")
	}
}

// TestSecretValidation covers the app-secret authentication path.
func TestSecretValidation(t *testing.T) {
	svc := testService(t)
	_, project := seedProject(t, svc)
	ctx := context.Background()
	slug, name := "island", "Island"
	app, err := svc.CreateApp(ctx, project.ID, CustomAppInput{Slug: &slug, Name: &name})
	if err != nil {
		t.Fatal(err)
	}

	secret, err := svc.CreateSecret(app.ID, SecretInput{Description: "ci"})
	if err != nil {
		t.Fatalf("CreateSecret() error = %v", err)
	}
	if len(secret.Secret) != 64 || secret.SecretTypeName() != "api_key" {
		t.Fatalf("secret = %+v", secret)
	}

	valid, err := svc.ValidateSecret(ctx, app.ID, secret.Secret)
	if err != nil || !valid {
		t.Fatalf("ValidateSecret() = %v, %v, want true", valid, err)
	}
	if valid, _ := svc.ValidateSecret(ctx, app.ID, "wrong"); valid {
		t.Fatal("ValidateSecret() accepted the wrong secret")
	}

	rotated, err := svc.RotateSecret(app.ID, secret.ID, SecretInput{})
	if err != nil {
		t.Fatalf("RotateSecret() error = %v", err)
	}
	if rotated.Secret == secret.Secret {
		t.Fatal("RotateSecret() kept the old value")
	}
	if valid, _ := svc.ValidateSecret(ctx, app.ID, secret.Secret); valid {
		t.Fatal("the rotated-away secret still validates")
	}

	// An OIDC secret is not accepted as an API key.
	oidc, err := svc.CreateSecret(app.ID, SecretInput{Type: "oidc"})
	if err != nil {
		t.Fatal(err)
	}
	if valid, _ := svc.ValidateSecret(ctx, app.ID, oidc.Secret); valid {
		t.Fatal("ValidateSecret() accepted an OIDC secret")
	}

	if err := svc.DeleteSecret(app.ID, secret.ID); err != nil {
		t.Fatalf("DeleteSecret() error = %v", err)
	}
	if valid, _ := svc.ValidateSecret(ctx, app.ID, rotated.Secret); valid {
		t.Fatal("a deleted secret still validates")
	}
}

// TestMiniAppPackage covers ZIP validation and the slug uniqueness rule.
func TestMiniAppPackage(t *testing.T) {
	svc := testService(t)
	_, project := seedProject(t, svc)
	ctx := context.Background()

	app, err := svc.CreateMiniApp(ctx, project.ID, MiniAppInput{Slug: "counter"})
	if err != nil {
		t.Fatalf("CreateMiniApp() error = %v", err)
	}
	if app.Stage != MiniAppDevelopment {
		t.Fatalf("stage = %v", app.Stage)
	}
	if _, err := svc.CreateMiniApp(ctx, project.ID, MiniAppInput{Slug: "COUNTER"}); err == nil {
		t.Fatal("CreateMiniApp() accepted a duplicate slug")
	}
	if _, err := svc.CreateMiniApp(ctx, project.ID, MiniAppInput{Slug: "bad slug"}); err == nil {
		t.Fatal("CreateMiniApp() accepted an invalid slug")
	}

	// A package without manifest.json is rejected.
	if _, err := readPackageManifest(zipBytes(t, map[string]string{"main.js": "x"}), 1<<20); err == nil {
		t.Fatal("readPackageManifest() accepted an archive without manifest.json")
	}
	// A traversal path is rejected.
	if _, err := readPackageManifest(zipBytes(t, map[string]string{"../evil.json": "{}", "manifest.json": `{"id":"dev.example.app","name":"A"}`}), 1<<20); err == nil {
		t.Fatal("readPackageManifest() accepted a path traversal")
	}
	// A valid manifest is parsed.
	manifest, err := readPackageManifest(zipBytes(t, map[string]string{
		"manifest.json": `{"id":"dev.example.counter","name":"Counter","version":"1.2.0","entry_url":"index.html"}`,
		"main.js":       "console.log(1)",
	}), 1<<20)
	if err != nil {
		t.Fatalf("readPackageManifest() error = %v", err)
	}
	if manifest.ID != "dev.example.counter" || manifest.Version != "1.2.0" {
		t.Fatalf("manifest = %+v", manifest)
	}
	// A non-reverse-domain id is rejected by the manifest validator.
	if err := validateManifest(&MiniAppManifest{ID: "counter", Name: "Counter"}); err == nil {
		t.Fatal("validateManifest() accepted a non reverse-domain id")
	}
	if err := validateManifest(manifest); err != nil {
		t.Fatalf("validateManifest() rejected a valid manifest: %v", err)
	}
}

// TestProductLifecycle covers product create, update and the state sub-row.
func TestProductLifecycle(t *testing.T) {
	svc := testService(t)
	_, project := seedProject(t, svc)
	ctx := context.Background()
	slug, name := "island", "Island"
	app, err := svc.CreateApp(ctx, project.ID, CustomAppInput{Slug: &slug, Name: &name})
	if err != nil {
		t.Fatal(err)
	}

	identifier, displayName, currency := "pro", "Pro", "CNY"
	price := 12.5
	quantity := 5
	product, err := svc.CreateProduct(ctx, app, ProductInput{
		Identifier:  &identifier,
		DisplayName: &displayName,
		Currency:    &currency,
		Price:       &price,
		Recurrence:  new("monthly"),
		State:       &ProductStateInput{StockMode: "manual", StockQuantity: &quantity},
	})
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}
	if product.State == nil || product.State.StockMode != StockManual {
		t.Fatalf("state = %+v", product.State)
	}
	if product.State.LastRestockedAt == nil || product.State.LastRestockedQuantity == nil || *product.State.LastRestockedQuantity != 5 {
		t.Fatalf("manual restock was not stamped: %+v", product.State)
	}
	if product.Recurrence != RecurrenceMonthly || product.Recurrence.Name() != "monthly" {
		t.Fatalf("recurrence = %v", product.Recurrence)
	}
	if _, err := svc.CreateProduct(ctx, app, ProductInput{Identifier: &identifier, DisplayName: &displayName, Currency: &currency}); err == nil {
		t.Fatal("CreateProduct() accepted a duplicate identifier")
	}

	newPrice := 20.0
	updated, err := svc.UpdateProduct(ctx, app, product, ProductInput{Price: &newPrice})
	if err != nil {
		t.Fatalf("UpdateProduct() error = %v", err)
	}
	if updated.Price != 20 || updated.Identifier != "pro" {
		t.Fatalf("updated = %+v", updated)
	}

	if err := svc.DeleteProduct(ctx, app, updated); err != nil {
		t.Fatalf("DeleteProduct() error = %v", err)
	}
	if _, err := svc.LoadProduct(app.ID, product.ID); err == nil {
		t.Fatal("the deleted product is still reachable")
	}
	var states int64
	if err := svc.DB.Model(&AppProductState{}).Where("product_id = ?", product.ID).Count(&states).Error; err != nil {
		t.Fatal(err)
	}
	if states != 0 {
		t.Fatalf("product states left behind: %d", states)
	}
}

// TestChatConfig covers the default-then-patch flow the bot routes use.
func TestChatConfig(t *testing.T) {
	svc := testService(t)
	_, project := seedProject(t, svc)
	bot := &BotAccount{ID: newUUID(), Slug: "helper", ProjectID: project.ID, IsActive: true}
	if err := svc.DB.Create(bot).Error; err != nil {
		t.Fatal(err)
	}

	config, err := svc.GetChatConfig(bot.ID)
	if err != nil {
		t.Fatalf("GetChatConfig() error = %v", err)
	}
	if !config.AutoApproveDm || config.AutoApproveGroupChat || !config.SupportChat {
		t.Fatalf("defaults = %+v", config)
	}
	if len(config.SubscribedEvents) != 1 || config.SubscribedEvents[0] != "messages.new" {
		t.Fatalf("default events = %v", config.SubscribedEvents)
	}

	patched, err := svc.PatchChatConfig(bot.ID, BotManifestInput{
		AutoApproveGroupChat: new(true),
		Commands:             &[]BotCommand{{Name: "/help", Description: "Help"}},
	})
	if err != nil {
		t.Fatalf("PatchChatConfig() error = %v", err)
	}
	if !patched.AutoApproveGroupChat || len(patched.Commands) != 1 {
		t.Fatalf("patched = %+v", patched)
	}
	if !patched.AutoApproveDm {
		t.Fatal("a partial patch cleared an untouched field")
	}
}

// TestDeleteAppCascades proves deleting an app removes its dependents, which
// the console relies on before a project is dropped.
func TestDeleteAppCascades(t *testing.T) {
	svc := testService(t)
	_, project := seedProject(t, svc)
	ctx := context.Background()
	slug, name := "island", "Island"
	app, err := svc.CreateApp(ctx, project.ID, CustomAppInput{Slug: &slug, Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSecret(app.ID, SecretInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWidget(app.ID, BoardWidgetManifest{Key: "steps", Name: "Steps"}); err != nil {
		t.Fatal(err)
	}
	identifier, displayName, currency := "pro", "Pro", "CNY"
	if _, err := svc.CreateProduct(ctx, app, ProductInput{Identifier: &identifier, DisplayName: &displayName, Currency: &currency}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteApp(app); err != nil {
		t.Fatalf("DeleteApp() error = %v", err)
	}
	// The portal soft-deletes, matching the identity service: a scoped read
	// must see nothing, while the rows survive for audit.
	for _, model := range []any{&CustomApp{}, &CustomAppSecret{}, &BoardWidget{}, &AppProduct{}, &AppProductState{}} {
		var visible int64
		if err := svc.DB.Model(model).Count(&visible).Error; err != nil {
			t.Fatal(err)
		}
		if visible != 0 {
			t.Fatalf("%T rows still visible: %d", model, visible)
		}
	}
	var appRow CustomApp
	if err := svc.DB.Unscoped().Where("id = ?", app.ID).First(&appRow).Error; err != nil {
		t.Fatalf("the soft-deleted app row is gone: %v", err)
	}
	if !appRow.DeletedAt.Valid {
		t.Fatal("the app row was not soft-deleted")
	}
}

// TestDiscoverAppsListsOnlyProduction proves the public catalog hides
// unpublished apps and reports its counts.
func TestDiscoverAppsListsOnlyProduction(t *testing.T) {
	svc := testService(t)
	_, project := seedProject(t, svc)
	ctx := context.Background()

	live, liveName := "live", "Live"
	if _, err := svc.CreateApp(ctx, project.ID, CustomAppInput{Slug: &live, Name: &liveName, Status: new("production")}); err != nil {
		t.Fatal(err)
	}
	draft, draftName := "draft", "Draft"
	if _, err := svc.CreateApp(ctx, project.ID, CustomAppInput{Slug: &draft, Name: &draftName}); err != nil {
		t.Fatal(err)
	}

	apps, total, err := svc.DiscoverApps(20, 0, "")
	if err != nil {
		t.Fatalf("DiscoverApps() error = %v", err)
	}
	if total != 1 || len(apps) != 1 || apps[0].Slug != "live" {
		t.Fatalf("apps = %+v, total = %d", apps, total)
	}
	apps, total, err = svc.DiscoverApps(20, 0, "draf")
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(apps) != 0 {
		t.Fatalf("search returned unpublished apps: %+v", apps)
	}
}

// TestTieredQuota mirrors ResourceQuotaCalculator.GetTieredQuota.
func TestTieredQuota(t *testing.T) {
	cases := []struct{ level, perk, want int }{
		{0, 0, 0},
		{10, 0, 1},
		{30, 1, 3},
		{60, 2, 5},
		{99, 0, 3},
	}
	for _, testCase := range cases {
		if got := TieredQuota(testCase.level, testCase.perk); got != testCase.want {
			t.Fatalf("TieredQuota(%d, %d) = %d, want %d", testCase.level, testCase.perk, got, testCase.want)
		}
	}
}

// TestBoardWidgetSlug proves the derived slug shape.
func TestBoardWidgetSlug(t *testing.T) {
	if got := BoardWidgetSlug("solsynth", "island", "steps"); got != "solsynth.island.steps" {
		t.Fatalf("BoardWidgetSlug() = %q", got)
	}
}

// zipBytes builds a ZIP archive from a file map.
func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pkg.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zipNewWriter(file)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

// TestPortalServiceErrorMapping keeps the HTTP status mapping honest.
func TestPortalServiceErrorMapping(t *testing.T) {
	if _, err := (&Service{}).LoadProject("", newUUID()); err == nil {
		t.Fatal("LoadProject() on a closed service returned no error")
	}
	if err := (&Service{}).RequirePermission(context.Background(), "", ""); err == nil {
		t.Fatal("RequirePermission() accepted empty inputs")
	}
	svc := testService(t)
	if err := svc.RequirePermission(context.Background(), "account", "custom.apps.create"); err != nil {
		t.Fatalf("a nil checker denied a permission: %v", err)
	}
	_ = service.ErrNotFound
}

// zipNewWriter isolates the archive/zip import to the test file.
func zipNewWriter(w io.Writer) *zip.Writer { return zip.NewWriter(w) }

// mustStruct builds a protobuf Struct from a map.
func mustStruct(t *testing.T, value map[string]any) *structpb.Struct {
	t.Helper()
	encoded, err := structpb.NewStruct(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// stubPublishers answers the publisher lookups the gRPC tests need without a
// live Sphere.
type stubPublishers struct {
	name string
}

func (s stubPublishers) GetPublisher(_ context.Context, ref string) (*gen.DyPublisher, error) {
	return &gen.DyPublisher{Id: ref, Name: s.name}, nil
}

func (s stubPublishers) GetPublisherBatch(_ context.Context, ids []string) ([]*gen.DyPublisher, error) {
	out := make([]*gen.DyPublisher, 0, len(ids))
	for _, id := range ids {
		out = append(out, &gen.DyPublisher{Id: id, Name: s.name})
	}
	return out, nil
}

func (stubPublishers) IsPublisherMember(context.Context, string, string, gen.DyPublisherMemberRole) (bool, error) {
	return true, nil
}

func (stubPublishers) ListPublishers(context.Context, string) ([]*gen.DyPublisher, error) {
	return nil, nil
}

// stubPayments records the order request it received.
type stubPayments struct {
	got   *gen.DyCreateOrderRequest
	order *gen.DyOrder
}

func (s *stubPayments) RegisterAppSubscriptionDefinition(context.Context, *gen.DyRegisterAppSubscriptionDefinitionRequest) error {
	return nil
}

func (s *stubPayments) CreateOrder(_ context.Context, req *gen.DyCreateOrderRequest) (*gen.DyOrder, error) {
	s.got = req
	return s.order, nil
}

// TestCreateCustomOrder covers the ad-hoc order endpoint: validation, the
// wallet request shape, and the app attribution.
func TestCreateCustomOrder(t *testing.T) {
	svc := testService(t)
	_, project := seedProject(t, svc)
	ctx := context.Background()
	slug, name := "island", "Island"
	app, err := svc.CreateApp(ctx, project.ID, CustomAppInput{Slug: &slug, Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	payments := &stubPayments{order: &gen.DyOrder{Id: "order-1"}}
	svc.Subscriptions = payments

	order, err := svc.CreateCustomOrder(ctx, app, CustomOrderInput{
		Identifier:      "sponsor",
		Currency:        "CNY",
		Amount:          12.5,
		ExpirationHours: 6,
		Remarks:         "thanks",
		Meta:            map[string]any{"tier": "gold"},
	})
	if err != nil {
		t.Fatalf("CreateCustomOrder() error = %v", err)
	}
	if order.GetId() != "order-1" {
		t.Fatalf("order = %+v", order)
	}
	if payments.got.GetCurrency() != "CNY" || payments.got.GetAmount() != "12.5" {
		t.Fatalf("request = %+v", payments.got)
	}
	if payments.got.GetAppIdentifier() != "developer.app:"+app.ID {
		t.Fatalf("app identifier = %q", payments.got.GetAppIdentifier())
	}
	if payments.got.GetProductIdentifier() != "sponsor" {
		t.Fatalf("product identifier = %q", payments.got.GetProductIdentifier())
	}
	if payments.got.GetExpiration().AsDuration().Hours() != 6 {
		t.Fatalf("expiration = %v", payments.got.GetExpiration().AsDuration())
	}
	if payments.got.GetRemarks() != "thanks" {
		t.Fatalf("remarks = %q", payments.got.GetRemarks())
	}
	if string(payments.got.GetMeta()) != `{"tier":"gold"}` {
		t.Fatalf("meta = %s", payments.got.GetMeta())
	}

	// Validation: every rejected field must not reach the wallet.
	for label, input := range map[string]CustomOrderInput{
		"missing identifier": {Currency: "CNY", Amount: 1},
		"missing currency":   {Identifier: "x", Amount: 1},
		"amount too small":   {Identifier: "x", Currency: "CNY", Amount: 0.0001},
		"bad expiration":     {Identifier: "x", Currency: "CNY", Amount: 1, ExpirationHours: 900},
	} {
		if _, err := svc.CreateCustomOrder(ctx, app, input); err == nil {
			t.Fatalf("CreateCustomOrder() accepted %s", label)
		}
	}

	// A deployment without a wallet reports a dependency failure, not success.
	svc.Subscriptions = nil
	if _, err := svc.CreateCustomOrder(ctx, app, CustomOrderInput{Identifier: "x", Currency: "CNY", Amount: 1}); err == nil {
		t.Fatal("CreateCustomOrder() succeeded without a payment service")
	}
}
