package develop

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	gen "src.solsynth.dev/sosys/go/proto"
)

// dialGRPC serves DyCustomAppService over an in-memory listener and returns a
// client, so the contract Passport and Wallet call is exercised end to end.
func dialGRPC(t *testing.T, svc *Service) gen.DyCustomAppServiceClient {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	gen.RegisterDyCustomAppServiceServer(server, &CustomAppGRPC{Service: svc})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return gen.NewDyCustomAppServiceClient(conn)
}

// TestCustomAppGRPCResolvesByIdAndSlug proves the lookup Passport uses for
// board rendering answers both query forms.
func TestCustomAppGRPCResolvesByIdAndSlug(t *testing.T) {
	svc := testService(t)
	_, project := seedProject(t, svc)
	ctx := context.Background()
	slug, name := "island", "Island"
	app, err := svc.CreateApp(ctx, project.ID, CustomAppInput{Slug: &slug, Name: &name, Status: new("production")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWidget(app.ID, BoardWidgetManifest{Key: "steps", Name: "Steps", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	client := dialGRPC(t, svc)

	byID, err := client.GetCustomApp(ctx, &gen.DyGetCustomAppRequest{
		Query: &gen.DyGetCustomAppRequest_Id{Id: app.ID},
	})
	if err != nil {
		t.Fatalf("GetCustomApp(by id) error = %v", err)
	}
	if byID.GetApp().GetSlug() != "island" || byID.GetApp().GetStatus() != gen.DyCustomAppStatus_DY_PRODUCTION {
		t.Fatalf("app = %+v", byID.GetApp())
	}
	if len(byID.GetApp().GetBoardWidgets()) != 1 {
		t.Fatalf("board widgets = %d, want 1", len(byID.GetApp().GetBoardWidgets()))
	}

	bySlug, err := client.GetCustomApp(ctx, &gen.DyGetCustomAppRequest{
		Query: &gen.DyGetCustomAppRequest_Slug{Slug: "island"},
	})
	if err != nil {
		t.Fatalf("GetCustomApp(by slug) error = %v", err)
	}
	if bySlug.GetApp().GetId() != app.ID {
		t.Fatalf("slug lookup returned %q", bySlug.GetApp().GetId())
	}

	_, err = client.GetCustomApp(ctx, &gen.DyGetCustomAppRequest{
		Query: &gen.DyGetCustomAppRequest_Id{Id: newUUID()},
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("missing app status = %v, want NotFound", status.Code(err))
	}
	_, err = client.GetCustomApp(ctx, &gen.DyGetCustomAppRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty query status = %v, want InvalidArgument", status.Code(err))
	}
}

// TestCustomAppGRPCSecretCheck proves Stargate's app-secret validation path.
func TestCustomAppGRPCSecretCheck(t *testing.T) {
	svc := testService(t)
	_, project := seedProject(t, svc)
	ctx := context.Background()
	slug, name := "island", "Island"
	app, err := svc.CreateApp(ctx, project.ID, CustomAppInput{Slug: &slug, Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.CreateSecret(app.ID, SecretInput{})
	if err != nil {
		t.Fatal(err)
	}
	client := dialGRPC(t, svc)

	response, err := client.CheckCustomAppSecret(ctx, &gen.DyCheckCustomAppSecretRequest{
		SecretIdentifier: &gen.DyCheckCustomAppSecretRequest_AppId{AppId: app.ID},
		Secret:           secret.Secret,
	})
	if err != nil {
		t.Fatalf("CheckCustomAppSecret() error = %v", err)
	}
	if !response.GetValid() {
		t.Fatal("CheckCustomAppSecret() rejected a valid secret")
	}

	response, err = client.CheckCustomAppSecret(ctx, &gen.DyCheckCustomAppSecretRequest{
		SecretIdentifier: &gen.DyCheckCustomAppSecretRequest_AppId{AppId: app.ID},
		Secret:           "nope",
	})
	if err != nil {
		t.Fatalf("CheckCustomAppSecret(wrong) error = %v", err)
	}
	if response.GetValid() {
		t.Fatal("CheckCustomAppSecret() accepted a wrong secret")
	}

	_, err = client.CheckCustomAppSecret(ctx, &gen.DyCheckCustomAppSecretRequest{
		SecretIdentifier: &gen.DyCheckCustomAppSecretRequest_AppId{AppId: app.ID},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty secret status = %v, want InvalidArgument", status.Code(err))
	}
}

// TestCustomAppGRPCBoardWidget proves the widget lookup and payload validation
// Passport calls when rendering and writing a board item.
func TestCustomAppGRPCBoardWidget(t *testing.T) {
	svc := testService(t)
	_, project := seedProject(t, svc)
	ctx := context.Background()
	slug, name := "island", "Island"
	app, err := svc.CreateApp(ctx, project.ID, CustomAppInput{
		Slug:        &slug,
		Name:        &name,
		OauthConfig: &CustomAppOauthConfig{AllowedScopes: []string{"accounts.profile.board"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWidget(app.ID, BoardWidgetManifest{
		Key:            "steps",
		Name:           "Steps",
		IsEnabled:      true,
		FieldTypes:     []BoardFieldType{{Name: "steps", Type: "number"}},
		RequiredFields: []string{"steps"},
	}); err != nil {
		t.Fatal(err)
	}
	client := dialGRPC(t, svc)

	widget, err := client.GetBoardWidget(ctx, &gen.DyGetBoardWidgetRequest{AppId: app.ID, WidgetKey: "steps"})
	if err != nil {
		t.Fatalf("GetBoardWidget() error = %v", err)
	}
	if widget.GetWidget().GetKey() != "steps" || widget.GetWidget().GetFieldTypes()["steps"] != "number" {
		t.Fatalf("widget = %+v", widget.GetWidget())
	}

	payload := mustStruct(t, map[string]any{
		"steps": map[string]any{"value": 1200, "label": "Steps today"},
	})
	validation, err := client.ValidateBoardWidgetPayload(ctx, &gen.DyValidateBoardWidgetPayloadRequest{
		AppId:     app.ID,
		WidgetKey: "steps",
		Payload:   payload,
	})
	if err != nil {
		t.Fatalf("ValidateBoardWidgetPayload() error = %v", err)
	}
	if !validation.GetValid() {
		t.Fatalf("valid payload rejected: %s", validation.GetMessage())
	}
	if validation.GetNormalizedPayload().GetFields()["steps"] == nil {
		t.Fatal("normalized payload is empty")
	}

	badPayload := mustStruct(t, map[string]any{"steps": map[string]any{"value": 1}})
	validation, err = client.ValidateBoardWidgetPayload(ctx, &gen.DyValidateBoardWidgetPayloadRequest{
		AppId:     app.ID,
		WidgetKey: "steps",
		Payload:   badPayload,
	})
	if err != nil {
		t.Fatalf("ValidateBoardWidgetPayload(bad) error = %v", err)
	}
	if validation.GetValid() {
		t.Fatal("a payload without a label was accepted")
	}

	_, err = client.GetBoardWidget(ctx, &gen.DyGetBoardWidgetRequest{AppId: app.ID, WidgetKey: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("missing widget status = %v, want NotFound", status.Code(err))
	}
}

// TestCustomAppGRPCProductIdentifier proves Wallet's qualified-identifier
// lookup, including a SKU that itself contains dots.
func TestCustomAppGRPCProductIdentifier(t *testing.T) {
	svc := testService(t)
	developer, project := seedProject(t, svc)
	ctx := context.Background()
	slug, name := "island", "Island"
	app, err := svc.CreateApp(ctx, project.ID, CustomAppInput{Slug: &slug, Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	identifier, displayName, currency := "pro.yearly", "Pro Yearly", "CNY"
	product, err := svc.CreateProduct(ctx, app, ProductInput{
		Identifier:  &identifier,
		DisplayName: &displayName,
		Currency:    &currency,
		Recurrence:  new("yearly"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The qualified identifier is "<publisher>.<app>.<sku>"; resolve the
	// publisher name from the enrolled developer row.
	// The stub resolves a publisher name to itself as the id, so point the
	// enrolled developer at that id.
	publisherName := "solsynth"
	if err := svc.DB.Model(&Developer{}).Where("id = ?", developer.ID).Update("publisher_id", publisherName).Error; err != nil {
		t.Fatal(err)
	}
	svc.Publishers = stubPublishers{name: publisherName}
	client := dialGRPC(t, svc)

	response, err := client.GetAppProduct(ctx, &gen.DyGetAppProductRequest{
		Identifier: publisherName + ".island.pro.yearly",
	})
	if err != nil {
		t.Fatalf("GetAppProduct() error = %v", err)
	}
	if response.GetProduct().GetId() != product.ID {
		t.Fatalf("product = %+v", response.GetProduct())
	}
	if response.GetProduct().GetRecurrence() != "yearly" {
		t.Fatalf("recurrence = %q", response.GetProduct().GetRecurrence())
	}

	_, err = client.GetAppProduct(ctx, &gen.DyGetAppProductRequest{Identifier: "too.short"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("short identifier status = %v, want InvalidArgument", status.Code(err))
	}
	_, err = client.GetAppProduct(ctx, &gen.DyGetAppProductRequest{Identifier: publisherName + ".island.missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("missing product status = %v, want NotFound", status.Code(err))
	}
}

// TestCustomAppGRPCDeveloper proves the attribution Wallet and Stargate read
// when resolving who owns an app.
func TestCustomAppGRPCDeveloper(t *testing.T) {
	svc := testService(t)
	developer, project := seedProject(t, svc)
	ctx := context.Background()
	slug, name := "island", "Island"
	app, err := svc.CreateApp(ctx, project.ID, CustomAppInput{Slug: &slug, Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	svc.Publishers = stubPublishers{name: "solsynth"}
	client := dialGRPC(t, svc)

	response, err := client.GetAppDeveloper(ctx, &gen.DyGetAppDeveloperRequest{AppId: app.ID})
	if err != nil {
		t.Fatalf("GetAppDeveloper() error = %v", err)
	}
	if response.GetApp().GetId() != app.ID {
		t.Fatalf("app = %+v", response.GetApp())
	}
	if response.GetDeveloper().GetId() != developer.ID {
		t.Fatalf("developer = %+v, want %s", response.GetDeveloper(), developer.ID)
	}
	if response.GetDeveloper().GetPublisherName() != "solsynth" {
		t.Fatalf("publisher name = %q", response.GetDeveloper().GetPublisherName())
	}

	_, err = client.GetAppDeveloper(ctx, &gen.DyGetAppDeveloperRequest{AppId: newUUID()})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("missing app status = %v, want NotFound", status.Code(err))
	}
	_, err = client.GetAppDeveloper(ctx, &gen.DyGetAppDeveloperRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty app_id status = %v, want InvalidArgument", status.Code(err))
	}
}
