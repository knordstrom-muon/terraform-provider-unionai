package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/unionai/cloud/gen/pb-go/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mockAppsClient implements the subset of identity.AppsServiceClient used by AppResource and ApiKeyResource.
type mockAppsClient struct {
	identity.AppsServiceClient
	createFn func(ctx context.Context, req *identity.CreateAppRequest) (*identity.CreateAppResponse, error)
	getFn    func(ctx context.Context, req *identity.GetAppRequest) (*identity.GetAppResponse, error)
	updateFn func(ctx context.Context, req *identity.UpdateAppRequest) (*identity.UpdateAppResponse, error)
	deleteFn func(ctx context.Context, req *identity.DeleteAppRequest) (*identity.DeleteAppResponse, error)
}

func (m *mockAppsClient) Create(ctx context.Context, in *identity.CreateAppRequest, opts ...grpc.CallOption) (*identity.CreateAppResponse, error) {
	return m.createFn(ctx, in)
}

func (m *mockAppsClient) Get(ctx context.Context, in *identity.GetAppRequest, opts ...grpc.CallOption) (*identity.GetAppResponse, error) {
	return m.getFn(ctx, in)
}

func (m *mockAppsClient) Update(ctx context.Context, in *identity.UpdateAppRequest, opts ...grpc.CallOption) (*identity.UpdateAppResponse, error) {
	return m.updateFn(ctx, in)
}

func (m *mockAppsClient) Delete(ctx context.Context, in *identity.DeleteAppRequest, opts ...grpc.CallOption) (*identity.DeleteAppResponse, error) {
	return m.deleteFn(ctx, in)
}

func notFoundGet(ctx context.Context, req *identity.GetAppRequest) (*identity.GetAppResponse, error) {
	return nil, status.Error(codes.NotFound, "not found")
}

func testAppSchema(t *testing.T) resource.SchemaResponse {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewAppResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema() returned errors: %v", resp.Diagnostics.Errors())
	}
	return *resp
}

func testAppModel(id, clientId types.String) AppResourceModel {
	return AppResourceModel{
		Id:                      id,
		ClientId:                clientId,
		ClientName:              types.StringValue("fire-products-ci"),
		ClientUri:               types.StringNull(),
		ConsentMethod:           types.StringValue("CONSENT_METHOD_REQUIRED"),
		GrantTypes:              convertStringsToSet([]string{"CLIENT_CREDENTIALS"}),
		LogoUri:                 types.StringNull(),
		PolicyUri:               types.StringNull(),
		RedirectUris:            convertStringsToSet([]string{"http://localhost:8080/authorization-code/callback"}),
		ResponseTypes:           convertStringsToSet([]string{"CODE"}),
		TokenEndpointAuthMethod: types.StringValue("CLIENT_SECRET_BASIC"),
		TosUri:                  types.StringNull(),
		Secret:                  types.StringUnknown(),
	}
}

func newAppPlan(t *testing.T, m AppResourceModel) tfsdk.Plan {
	t.Helper()
	plan := tfsdk.Plan{Schema: testAppSchema(t).Schema}
	if diags := plan.Set(context.Background(), &m); diags.HasError() {
		t.Fatalf("plan.Set() returned errors: %v", diags.Errors())
	}
	return plan
}

func newAppState(t *testing.T, m AppResourceModel) tfsdk.State {
	t.Helper()
	state := tfsdk.State{Schema: testAppSchema(t).Schema}
	if diags := state.Set(context.Background(), &m); diags.HasError() {
		t.Fatalf("state.Set() returned errors: %v", diags.Errors())
	}
	return state
}

func runAppCreate(t *testing.T, mock *mockAppsClient, clientId types.String) (AppResourceModel, *resource.CreateResponse) {
	t.Helper()
	r := &AppResource{conn: mock, org: "test-org"}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: testAppSchema(t).Schema}}
	r.Create(context.Background(), resource.CreateRequest{Plan: newAppPlan(t, testAppModel(types.StringUnknown(), clientId))}, resp)
	var data AppResourceModel
	if !resp.Diagnostics.HasError() {
		resp.State.Get(context.Background(), &data)
	}
	return data, resp
}

func TestAppResource_Create_UsesServerAssignedClientId(t *testing.T) {
	var capturedReq *identity.CreateAppRequest
	mock := &mockAppsClient{
		getFn: func(ctx context.Context, req *identity.GetAppRequest) (*identity.GetAppResponse, error) {
			t.Error("Get should not be called when client_id is not configured")
			return nil, status.Error(codes.NotFound, "not found")
		},
		createFn: func(ctx context.Context, req *identity.CreateAppRequest) (*identity.CreateAppResponse, error) {
			capturedReq = req
			return &identity.CreateAppResponse{App: &identity.App{ClientId: "123456", ClientSecret: "s3cret"}}, nil
		},
	}

	data, resp := runAppCreate(t, mock, types.StringUnknown())

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create() returned errors: %v", resp.Diagnostics.Errors())
	}
	if capturedReq.ClientId != "" {
		t.Errorf("Expected empty requested client ID, got '%s'", capturedReq.ClientId)
	}
	if data.Id.ValueString() != "123456" || data.ClientId.ValueString() != "123456" {
		t.Errorf("Expected id and client_id '123456', got '%s' and '%s'", data.Id.ValueString(), data.ClientId.ValueString())
	}
	if data.Secret.ValueString() != "s3cret" {
		t.Errorf("Expected secret 's3cret', got '%s'", data.Secret.ValueString())
	}
}

func TestAppResource_Create_RequestedClientIdIgnored(t *testing.T) {
	var capturedReq *identity.CreateAppRequest
	mock := &mockAppsClient{
		getFn: notFoundGet,
		createFn: func(ctx context.Context, req *identity.CreateAppRequest) (*identity.CreateAppResponse, error) {
			capturedReq = req
			return &identity.CreateAppResponse{App: &identity.App{ClientId: "123456", ClientSecret: "s3cret"}}, nil
		},
	}

	data, resp := runAppCreate(t, mock, types.StringValue("fire-products-ci"))

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create() returned errors: %v", resp.Diagnostics.Errors())
	}
	if capturedReq.ClientId != "fire-products-ci" {
		t.Errorf("Expected requested client ID 'fire-products-ci', got '%s'", capturedReq.ClientId)
	}
	if data.Id.ValueString() != "123456" {
		t.Errorf("Expected id '123456', got '%s'", data.Id.ValueString())
	}
	if data.ClientId.ValueString() != "fire-products-ci" {
		t.Errorf("Expected client_id to keep configured value, got '%s'", data.ClientId.ValueString())
	}
}

func TestAppResource_Create_AlreadyExists(t *testing.T) {
	mock := &mockAppsClient{
		getFn: func(ctx context.Context, req *identity.GetAppRequest) (*identity.GetAppResponse, error) {
			return &identity.GetAppResponse{App: &identity.App{ClientId: req.ClientId}}, nil
		},
		createFn: func(ctx context.Context, req *identity.CreateAppRequest) (*identity.CreateAppResponse, error) {
			t.Error("Create should not be called when the app already exists")
			return nil, status.Error(codes.AlreadyExists, "exists")
		},
	}

	_, resp := runAppCreate(t, mock, types.StringValue("fire-products-ci"))

	if !resp.Diagnostics.HasError() {
		t.Fatal("Expected Create() to return an error")
	}
}

func TestAppResource_Read_UsesId(t *testing.T) {
	tests := []struct {
		name             string
		clientId         types.String
		expectedClientId string
	}{
		{"keeps requested client_id", types.StringValue("fire-products-ci"), "fire-products-ci"},
		{"fills client_id after import", types.StringNull(), "123456"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAppsClient{
				getFn: func(ctx context.Context, req *identity.GetAppRequest) (*identity.GetAppResponse, error) {
					if req.ClientId != "123456" {
						t.Errorf("Expected Get by id '123456', got '%s'", req.ClientId)
					}
					return &identity.GetAppResponse{App: &identity.App{ClientId: "123456", ClientName: "fire-products-ci"}}, nil
				},
			}
			r := &AppResource{conn: mock, org: "test-org"}
			state := newAppState(t, testAppModel(types.StringValue("123456"), tt.clientId))
			resp := &resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Read() returned errors: %v", resp.Diagnostics.Errors())
			}
			var data AppResourceModel
			resp.State.Get(context.Background(), &data)
			if data.Id.ValueString() != "123456" {
				t.Errorf("Expected id '123456', got '%s'", data.Id.ValueString())
			}
			if data.ClientId.ValueString() != tt.expectedClientId {
				t.Errorf("Expected client_id '%s', got '%s'", tt.expectedClientId, data.ClientId.ValueString())
			}
		})
	}
}

func TestAppResource_Read_StoresServerAssignedId(t *testing.T) {
	mock := &mockAppsClient{
		getFn: func(ctx context.Context, req *identity.GetAppRequest) (*identity.GetAppResponse, error) {
			return &identity.GetAppResponse{App: &identity.App{ClientId: "123456", ClientName: "fire-products-ci"}}, nil
		},
	}
	r := &AppResource{conn: mock, org: "test-org"}
	state := newAppState(t, testAppModel(types.StringValue("fire-products-ci"), types.StringValue("fire-products-ci")))
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read() returned errors: %v", resp.Diagnostics.Errors())
	}
	var data AppResourceModel
	resp.State.Get(context.Background(), &data)
	if data.Id.ValueString() != "123456" {
		t.Errorf("Expected id '123456', got '%s'", data.Id.ValueString())
	}
	if data.ClientId.ValueString() != "fire-products-ci" {
		t.Errorf("Expected client_id 'fire-products-ci', got '%s'", data.ClientId.ValueString())
	}
}

func TestAppResource_Read_KeepsUnsetOptionalNull(t *testing.T) {
	empty := types.SetValueMust(types.StringType, []attr.Value{})
	tests := []struct {
		name     string
		priorStr types.String
		priorSet types.Set
	}{
		{"prior null", types.StringNull(), types.SetNull(types.StringType)},
		{"prior empty from older versions", types.StringValue(""), empty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAppsClient{
				getFn: func(ctx context.Context, req *identity.GetAppRequest) (*identity.GetAppResponse, error) {
					return &identity.GetAppResponse{App: &identity.App{ClientId: "123456", ClientName: "fire-products-ci"}}, nil
				},
			}
			r := &AppResource{conn: mock, org: "test-org"}
			m := testAppModel(types.StringValue("123456"), types.StringValue("123456"))
			m.ClientUri, m.LogoUri, m.PolicyUri, m.TosUri = tt.priorStr, tt.priorStr, tt.priorStr, tt.priorStr
			m.RedirectUris, m.ResponseTypes = tt.priorSet, tt.priorSet
			state := newAppState(t, m)
			resp := &resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Read() returned errors: %v", resp.Diagnostics.Errors())
			}
			var data AppResourceModel
			resp.State.Get(context.Background(), &data)
			for name, isNull := range map[string]bool{
				"client_uri":     data.ClientUri.IsNull(),
				"logo_uri":       data.LogoUri.IsNull(),
				"policy_uri":     data.PolicyUri.IsNull(),
				"tos_uri":        data.TosUri.IsNull(),
				"redirect_uris":  data.RedirectUris.IsNull(),
				"response_types": data.ResponseTypes.IsNull(),
			} {
				if !isNull {
					t.Errorf("Expected %s to be null", name)
				}
			}
		})
	}
}

// Read stores empty values as null, so configured "" / [] would never converge.
func TestAppResource_Schema_RejectsEmptyOptionalValues(t *testing.T) {
	attrs := testAppSchema(t).Schema.Attributes
	for _, name := range []string{"client_uri", "logo_uri", "policy_uri", "tos_uri"} {
		resp := &validator.StringResponse{}
		for _, v := range attrs[name].(schema.StringAttribute).Validators {
			v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root(name), ConfigValue: types.StringValue("")}, resp)
		}
		if !resp.Diagnostics.HasError() {
			t.Errorf("Expected %s = \"\" to be rejected", name)
		}
	}
	for _, name := range []string{"redirect_uris", "response_types"} {
		resp := &validator.SetResponse{}
		for _, v := range attrs[name].(schema.SetAttribute).Validators {
			v.ValidateSet(context.Background(), validator.SetRequest{Path: path.Root(name), ConfigValue: types.SetValueMust(types.StringType, nil)}, resp)
		}
		if !resp.Diagnostics.HasError() {
			t.Errorf("Expected %s = [] to be rejected", name)
		}
	}
}

func TestAppResource_Read_NotFound(t *testing.T) {
	r := &AppResource{conn: &mockAppsClient{getFn: notFoundGet}, org: "test-org"}
	state := newAppState(t, testAppModel(types.StringValue("123456"), types.StringValue("123456")))
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read() should not return errors on NotFound, got: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsNull() {
		t.Error("Expected state to be removed on NotFound")
	}
}

func TestAppResource_Update_UsesId(t *testing.T) {
	var capturedReq *identity.UpdateAppRequest
	mock := &mockAppsClient{
		updateFn: func(ctx context.Context, req *identity.UpdateAppRequest) (*identity.UpdateAppResponse, error) {
			capturedReq = req
			return &identity.UpdateAppResponse{}, nil
		},
	}
	r := &AppResource{conn: mock, org: "test-org"}
	m := testAppModel(types.StringValue("123456"), types.StringValue("fire-products-ci"))
	m.Secret = types.StringValue("s3cret")
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: testAppSchema(t).Schema}}
	r.Update(context.Background(), resource.UpdateRequest{Plan: newAppPlan(t, m), State: newAppState(t, m)}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update() returned errors: %v", resp.Diagnostics.Errors())
	}
	if capturedReq.ClientId != "123456" {
		t.Errorf("Expected Update by id '123456', got '%s'", capturedReq.ClientId)
	}
	var data AppResourceModel
	resp.State.Get(context.Background(), &data)
	if data.Id.ValueString() != "123456" || data.ClientId.ValueString() != "fire-products-ci" {
		t.Errorf("Expected id '123456' and client_id 'fire-products-ci', got '%s' and '%s'", data.Id.ValueString(), data.ClientId.ValueString())
	}
}

func TestAppResource_Update_Secret(t *testing.T) {
	tests := []struct {
		name           string
		returnedSecret string
		expectedSecret string
	}{
		{"rotated secret is stored", "new-s3cret", "new-s3cret"},
		{"prior secret kept when none returned", "", "old-s3cret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAppsClient{
				updateFn: func(ctx context.Context, req *identity.UpdateAppRequest) (*identity.UpdateAppResponse, error) {
					return &identity.UpdateAppResponse{App: &identity.App{ClientId: "123456", ClientSecret: tt.returnedSecret}}, nil
				},
			}
			r := &AppResource{conn: mock, org: "test-org"}
			prior := testAppModel(types.StringValue("123456"), types.StringValue("123456"))
			prior.Secret = types.StringValue("old-s3cret")
			planned := prior
			planned.Secret = types.StringUnknown()
			resp := &resource.UpdateResponse{State: tfsdk.State{Schema: testAppSchema(t).Schema}}
			r.Update(context.Background(), resource.UpdateRequest{Plan: newAppPlan(t, planned), State: newAppState(t, prior)}, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Update() returned errors: %v", resp.Diagnostics.Errors())
			}
			var data AppResourceModel
			resp.State.Get(context.Background(), &data)
			if data.Secret.ValueString() != tt.expectedSecret {
				t.Errorf("Expected secret '%s', got '%s'", tt.expectedSecret, data.Secret.ValueString())
			}
		})
	}
}

func TestAppResource_Delete_UsesId(t *testing.T) {
	var capturedReq *identity.DeleteAppRequest
	mock := &mockAppsClient{
		deleteFn: func(ctx context.Context, req *identity.DeleteAppRequest) (*identity.DeleteAppResponse, error) {
			capturedReq = req
			return &identity.DeleteAppResponse{}, nil
		},
	}
	r := &AppResource{conn: mock, org: "test-org"}
	state := newAppState(t, testAppModel(types.StringValue("123456"), types.StringValue("fire-products-ci")))
	resp := &resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete() returned errors: %v", resp.Diagnostics.Errors())
	}
	if capturedReq.ClientId != "123456" {
		t.Errorf("Expected Delete by id '123456', got '%s'", capturedReq.ClientId)
	}
}
