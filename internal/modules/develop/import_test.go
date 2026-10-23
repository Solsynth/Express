package developmodule

import (
	"context"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"src.solsynth.dev/solsynth/distribution/internal/module"
)

// openImportDB opens a SQLite database for the import tests.
func openImportDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+filepath.Join(t.TempDir(), name)+"?mode=rwc"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// seedLegacyDevelop creates the Develop-shaped source tables with one row per
// table, so the importer's queries and scans are exercised against real SQL.
func seedLegacyDevelop(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE developers (id text primary key, publisher_id text not null)`,
		`CREATE TABLE dev_projects (
			id text primary key, slug text, name text, description text,
			developer_id text, created_at datetime, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE custom_apps (
			id text primary key, slug text, name text, description text, status integer,
			picture text, background text, verification text, oauth_config text, links text,
			payment_wallet_id text, project_id text,
			created_at datetime, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE custom_app_secrets (
			id text primary key, secret text, description text, expired_at datetime, is_oidc boolean,
			app_id text, created_at datetime, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE board_widgets (
			id text primary key, app_id text, key text, name text, description text,
			is_enabled boolean, renderer_type text, payload_type text,
			field_types text, required_fields text, max_payload_bytes integer, allow_multiple boolean,
			created_at datetime, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE bot_accounts (
			id text primary key, slug text, is_active boolean, project_id text,
			created_at datetime, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE bot_chat_configs (
			id text primary key, commands text, auto_approve_dm boolean,
			auto_approve_group_chat boolean, support_chat boolean, subscribed_events text,
			created_at datetime, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE mini_apps (
			id text primary key, slug text, plugin_id text, name text, version text,
			author text, description text, entry_url text, icon text, background text,
			homepage text, package_url text, package_storage_key text, package_sha256 text,
			package_size integer, download_count integer, stage integer, manifest text, project_id text,
			created_at datetime, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE app_products (
			id text primary key, identifier text, display_name text, description text,
			currency text, price numeric, picture text, background text, fulfillment text,
			app_id text, recurrence integer, group_identifier text,
			created_at datetime, updated_at datetime, deleted_at datetime)`,
		`CREATE TABLE app_product_states (
			id text primary key, is_enabled boolean, stock_mode integer, stock_quantity integer,
			last_restocked_at datetime, last_restocked_quantity integer, product_id text,
			created_at datetime, updated_at datetime, deleted_at datetime)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create source table: %v", err)
		}
	}

	now := "2026-01-02 03:04:05"
	inserts := []string{
		`INSERT INTO developers VALUES ('dev-1', 'pub-1')`,
		`INSERT INTO dev_projects VALUES ('proj-1','island','Island','A project','dev-1','` + now + `','` + now + `',NULL)`,
		// The JSON payloads below are written the way the legacy service's own
		// serializer writes them: PascalCase keys, and enums as their ordinal.
		// A fixture in the portal's own snake_case would hide the key-matching
		// bug that silently drops every oauth scope.
		`INSERT INTO custom_apps VALUES ('app-1','island','Island','An app',2,
			'{"Id":"file-1","Name":"icon.png","FileMeta":{},"UserMeta":{}}',
			NULL,
			'{"Type":5,"Title":"Verified","Description":"A verified app","VerifiedBy":"admin"}',
			'{"ClientUri":"https://example.test","RedirectUris":["https://example.test/cb"],"AllowedScopes":["openid","accounts.profile.board"],"AllowedGrantTypes":["authorization_code"],"RequirePkce":true,"AllowOfflineAccess":false,"IsPublicClient":false}',
			'{"HomePage":"https://example.test"}',
			'wallet-1','proj-1','` + now + `','` + now + `',NULL)`,
		`INSERT INTO custom_app_secrets VALUES ('sec-1','s3cr3t','ci',NULL,0,'app-1','` + now + `','` + now + `',NULL)`,
		`INSERT INTO board_widgets VALUES ('wid-1','app-1','steps','Steps','Step count',
			1,'data','object',
			'[{"Name":"steps","Type":"number","Label":"Steps","Required":true}]',
			'["steps"]',512,1,'` + now + `','` + now + `',NULL)`,
		`INSERT INTO bot_accounts VALUES ('bot-1','helper',1,'proj-1','` + now + `','` + now + `',NULL)`,
		`INSERT INTO bot_chat_configs VALUES ('bot-1',
			'[{"Name":"/help","Description":"Help","Parameters":[{"Name":"topic","Required":true,"Type":"string"}]}]',1,0,1,'["messages.new"]',
			'` + now + `','` + now + `',NULL)`,
		`INSERT INTO mini_apps VALUES ('mini-1','counter','dev.example.counter','Counter','1.2.0',
			'Example','A counter','index.html',
			'{"Id":"icon-1","Name":"icon.png","FileMeta":{},"UserMeta":{}}',NULL,NULL,
			'https://cdn.test/counter.zip','plugins/mini-1/pkg.zip','abc123',2048,7,2,
			'{"Id":"dev.example.counter","Name":"Counter","Version":"1.2.0","Entry":"main.js","Permissions":[],"Background":false,"EntryUrl":"index.html"}',
			'proj-1','` + now + `','` + now + `',NULL)`,
		`INSERT INTO app_products VALUES ('prod-1','pro','Pro','The pro tier','CNY',12.5,
			NULL,NULL,'{"IsAddressRequired":false,"RequiredScopes":["pro"]}',
			'app-1',2,'group-1','` + now + `','` + now + `',NULL)`,
		`INSERT INTO app_product_states VALUES ('state-1',1,5,10,NULL,NULL,'prod-1','` + now + `','` + now + `',NULL)`,
	}
	for _, statement := range inserts {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed source: %v", err)
		}
	}
}

// TestImportAllModules copies a full Develop deployment and asserts every row
// landed with its relationships and JSON payloads intact.
func TestImportAllModules(t *testing.T) {
	source := openImportDB(t, "source.db")
	seedLegacyDevelop(t, source)
	target := openImportDB(t, "target.db")

	ctx := context.Background()
	factories := Factories()
	// Prepare the target exactly as the command does before importing.
	if err := module.PrepareImportSchema(ctx, target, factories); err != nil {
		t.Fatalf("prepare schema: %v", err)
	}

	for _, id := range module.ImportOrder(factories) {
		importer, ok := module.ImporterFor(factories, id)
		if !ok {
			t.Fatalf("module %q has no importer", id)
		}
		stats, err := importer.Import(ctx, source, target, module.ImportOptions{BatchSize: 2})
		if err != nil {
			t.Fatalf("%s import: %v", id, err)
		}
		if stats.Skipped != 0 {
			t.Fatalf("%s skipped %d rows, want 0: %+v", id, stats.Skipped, stats.Tables)
		}
		if stats.Written == 0 {
			t.Fatalf("%s wrote no rows", id)
		}
	}

	// The import order must cover every table, so nothing is left behind.
	for table, want := range map[string]int64{
		"developers": 1, "dev_projects": 1, "custom_apps": 1, "custom_app_secrets": 1,
		"board_widgets": 1, "bot_accounts": 1, "bot_chat_configs": 1, "mini_apps": 1,
		"app_products": 1, "app_product_states": 1,
	} {
		var got int64
		if err := target.Table(table).Count(&got).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s rows = %d, want %d", table, got, want)
		}
	}

	// JSON columns must survive the round trip, including the board widget
	// contract Passport depends on.
	var app struct {
		OauthConfig   string
		Picture       string
		PaymentWallet *string
		Status        int
	}
	if err := target.Raw(`SELECT oauth_config, picture, payment_wallet_id AS payment_wallet, status
		FROM custom_apps WHERE id = 'app-1'`).Scan(&app).Error; err != nil {
		t.Fatal(err)
	}
	if app.Status != 2 {
		t.Fatalf("app status = %d, want 2 (production)", app.Status)
	}
	if app.PaymentWallet == nil || *app.PaymentWallet != "wallet-1" {
		t.Fatalf("payment wallet = %v", app.PaymentWallet)
	}
	if !contains(app.OauthConfig, "accounts.profile.board") {
		t.Fatalf("oauth config = %q", app.OauthConfig)
	}
	if !contains(app.Picture, "file-1") {
		t.Fatalf("picture = %q", app.Picture)
	}

	// The source wrote these columns with PascalCase keys and an ordinal enum.
	// Asserting the decoded values, not just that a value exists, is what makes
	// a silent key mismatch fail here instead of in production.
	if !contains(app.OauthConfig, `"client_uri":"https://example.test"`) ||
		!contains(app.OauthConfig, `"require_pkce":true`) ||
		!contains(app.OauthConfig, `"redirect_uris":["https://example.test/cb"]`) {
		t.Fatalf("oauth config lost its values: %q", app.OauthConfig)
	}

	var verification struct {
		Verification string
	}
	if err := target.Raw(`SELECT verification FROM custom_apps WHERE id = 'app-1'`).
		Scan(&verification).Error; err != nil {
		t.Fatal(err)
	}
	if !contains(verification.Verification, `"type":"developer"`) ||
		!contains(verification.Verification, `"title":"Verified"`) ||
		!contains(verification.Verification, `"verified_by":"admin"`) {
		t.Fatalf("verification = %q", verification.Verification)
	}

	var widget struct {
		FieldTypes      string
		RequiredFields  string
		MaxPayloadBytes int
	}
	if err := target.Raw(`SELECT field_types, required_fields, max_payload_bytes FROM board_widgets WHERE id = 'wid-1'`).
		Scan(&widget).Error; err != nil {
		t.Fatal(err)
	}
	if widget.MaxPayloadBytes != 512 {
		t.Fatalf("max payload bytes = %d", widget.MaxPayloadBytes)
	}
	if !contains(widget.FieldTypes, `"steps"`) || !contains(widget.RequiredFields, `"steps"`) {
		t.Fatalf("widget JSON = %q / %q", widget.FieldTypes, widget.RequiredFields)
	}

	var mini struct {
		PluginID      string
		DownloadCount int64
		Stage         int
		Manifest      string
	}
	if err := target.Raw(`SELECT plugin_id, download_count, stage, manifest FROM mini_apps WHERE id = 'mini-1'`).
		Scan(&mini).Error; err != nil {
		t.Fatal(err)
	}
	if mini.PluginID != "dev.example.counter" || mini.DownloadCount != 7 || mini.Stage != 2 {
		t.Fatalf("mini app = %+v", mini)
	}
	// The manifest arrived with PascalCase keys and must still carry its values.
	if !contains(mini.Manifest, `"id":"dev.example.counter"`) ||
		!contains(mini.Manifest, `"entry_url":"index.html"`) {
		t.Fatalf("mini app manifest = %q", mini.Manifest)
	}

	var product struct {
		Price       float64
		Recurrence  int
		Fulfillment string
	}
	if err := target.Raw(`SELECT price, recurrence, fulfillment FROM app_products WHERE id = 'prod-1'`).
		Scan(&product).Error; err != nil {
		t.Fatal(err)
	}
	if product.Price != 12.5 || product.Recurrence != 2 {
		t.Fatalf("product = %+v", product)
	}
	if !contains(product.Fulfillment, `"required_scopes":["pro"]`) ||
		!contains(product.Fulfillment, `"is_address_required":false`) {
		t.Fatalf("fulfillment = %q", product.Fulfillment)
	}
}

// TestImportIsIdempotent proves a re-run writes nothing and reports the rows it
// skipped, which is what makes the script safe to repeat.
func TestImportIsIdempotent(t *testing.T) {
	source := openImportDB(t, "source.db")
	seedLegacyDevelop(t, source)
	target := openImportDB(t, "target.db")

	ctx := context.Background()
	factories := Factories()
	if err := module.PrepareImportSchema(ctx, target, factories); err != nil {
		t.Fatal(err)
	}
	importAll := func() map[string]module.ImportStats {
		out := map[string]module.ImportStats{}
		for _, id := range module.ImportOrder(factories) {
			importer, _ := module.ImporterFor(factories, id)
			stats, err := importer.Import(ctx, source, target, module.ImportOptions{})
			if err != nil {
				t.Fatalf("%s import: %v", id, err)
			}
			out[id] = stats
		}
		return out
	}

	first := importAll()
	for id, stats := range first {
		if stats.Written == 0 {
			t.Fatalf("%s wrote nothing on the first run", id)
		}
	}

	second := importAll()
	for id, stats := range second {
		if stats.Written != 0 {
			t.Fatalf("%s rewrote %d rows on the second run", id, stats.Written)
		}
		if stats.Skipped != stats.Read {
			t.Fatalf("%s skipped %d of %d rows; every row should be a duplicate", id, stats.Skipped, stats.Read)
		}
	}
}

// TestImportDryRunWritesNothing proves --dry-run validates without persisting.
func TestImportDryRunWritesNothing(t *testing.T) {
	source := openImportDB(t, "source.db")
	seedLegacyDevelop(t, source)
	target := openImportDB(t, "target.db")

	ctx := context.Background()
	factories := Factories()
	if err := module.PrepareImportSchema(ctx, target, factories); err != nil {
		t.Fatal(err)
	}
	for _, id := range module.ImportOrder(factories) {
		importer, _ := module.ImporterFor(factories, id)
		stats, err := importer.Import(ctx, source, target, module.ImportOptions{DryRun: true})
		if err != nil {
			t.Fatalf("%s dry run: %v", id, err)
		}
		if stats.Read == 0 {
			t.Fatalf("%s read nothing in a dry run", id)
		}
	}
	for table, want := range map[string]int64{
		"developers": 0, "dev_projects": 0, "custom_apps": 0, "custom_app_secrets": 0,
		"board_widgets": 0, "bot_accounts": 0, "bot_chat_configs": 0, "mini_apps": 0,
		"app_products": 0, "app_product_states": 0,
	} {
		var got int64
		if err := target.Table(table).Count(&got).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s rows = %d after a dry run, want %d", table, got, want)
		}
	}
}

// TestImportSkipsOrphanChildren proves a child whose parent is absent is
// skipped and counted, never imported as a dangling row.
func TestImportSkipsOrphanChildren(t *testing.T) {
	source := openImportDB(t, "source.db")
	seedLegacyDevelop(t, source)
	// Drop the developer, so the project (and everything under it) cannot
	// satisfy the target's relationships.
	if err := source.Exec(`DELETE FROM developers`).Error; err != nil {
		t.Fatal(err)
	}
	target := openImportDB(t, "target.db")
	ctx := context.Background()
	factories := Factories()
	if err := module.PrepareImportSchema(ctx, target, factories); err != nil {
		t.Fatal(err)
	}

	for _, id := range module.ImportOrder(factories) {
		importer, _ := module.ImporterFor(factories, id)
		stats, err := importer.Import(ctx, source, target, module.ImportOptions{})
		if err != nil {
			t.Fatalf("%s import: %v", id, err)
		}
		if stats.Written != 0 {
			t.Fatalf("%s imported %d orphan rows", id, stats.Written)
		}
		if stats.Read > 0 && stats.Skipped == 0 {
			t.Fatalf("%s read %d rows but skipped none", id, stats.Read)
		}
	}
	for table, want := range map[string]int64{
		"developers": 0, "dev_projects": 0, "custom_apps": 0, "board_widgets": 0, "mini_apps": 0,
	} {
		var got int64
		if err := target.Table(table).Count(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s rows = %d, want %d", table, got, want)
		}
	}
}

// TestImportRecordsState proves the run is recorded in module_import_state.
func TestImportRecordsState(t *testing.T) {
	source := openImportDB(t, "source.db")
	seedLegacyDevelop(t, source)
	target := openImportDB(t, "target.db")
	ctx := context.Background()
	factories := Factories()
	if err := module.PrepareImportSchema(ctx, target, factories); err != nil {
		t.Fatal(err)
	}
	importer, _ := module.ImporterFor(factories, ModuleApps)
	if _, err := importer.Import(ctx, source, target, module.ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	var count int64
	if err := target.Raw(`SELECT COUNT(*) FROM module_import_state WHERE module = ?`, ModuleApps).Scan(&count).Error; err != nil {
		t.Fatalf("read import state: %v", err)
	}
	if count == 0 {
		t.Fatal("no import state recorded")
	}
}

// TestImportMissingSourceTable proves a deployment that never used a feature
// imports cleanly rather than failing.
func TestImportMissingSourceTable(t *testing.T) {
	source := openImportDB(t, "source.db")
	target := openImportDB(t, "target.db")
	ctx := context.Background()
	factories := Factories()
	if err := module.PrepareImportSchema(ctx, target, factories); err != nil {
		t.Fatal(err)
	}
	importer, _ := module.ImporterFor(factories, ModuleMiniApps)
	stats, err := importer.Import(ctx, source, target, module.ImportOptions{})
	if err != nil {
		t.Fatalf("import without a source table: %v", err)
	}
	if stats.Written != 0 || stats.Read != 0 {
		t.Fatalf("stats = %+v, want an untouched run", stats)
	}
}

// contains reports whether a JSON blob carries a substring.
func contains(haystack, needle string) bool {
	return len(haystack) > 0 && len(needle) > 0 &&
		(len(haystack) >= len(needle)) &&
		indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return index
		}
	}
	return -1
}
