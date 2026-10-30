package provider

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/unionai/cloud/gen/pb-go/identity"
)

func testApiKeySchema(t *testing.T) resource.SchemaResponse {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewApiKeyResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema() returned errors: %v", resp.Diagnostics.Errors())
	}
	return *resp
}

func newApiKeyState(t *testing.T, m ApiKeyResourceModel) tfsdk.State {
	t.Helper()
	state := tfsdk.State{Schema: testApiKeySchema(t).Schema}
	if diags := state.Set(context.Background(), &m); diags.HasError() {
		t.Fatalf("state.Set() returned errors: %v", diags.Errors())
	}
	return state
}

func TestApiKeyResource_Create_StoresServerAssignedClientId(t *testing.T) {
	mock := &mockAppsClient{
		createFn: func(ctx context.Context, req *identity.CreateAppRequest) (*identity.CreateAppResponse, error) {
			return &identity.CreateAppResponse{App: &identity.App{ClientId: "123456", ClientSecret: "s3cret"}}, nil
		},
	}
	r := &ApiKeyResource{conn: mock, org: "test-org", host: "dns:///test.union.ai"}
	schema := testApiKeySchema(t)
	plan := tfsdk.Plan{Schema: schema.Schema}
	if diags := plan.Set(context.Background(), &ApiKeyResourceModel{Id: types.StringValue("my-key"), ClientId: types.StringUnknown(), Secret: types.StringUnknown()}); diags.HasError() {
		t.Fatalf("plan.Set() returned errors: %v", diags.Errors())
	}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: schema.Schema}}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create() returned errors: %v", resp.Diagnostics.Errors())
	}
	var data ApiKeyResourceModel
	resp.State.Get(context.Background(), &data)
	if data.Id.ValueString() != "my-key" || data.ClientId.ValueString() != "123456" {
		t.Errorf("Expected id 'my-key' and client_id '123456', got '%s' and '%s'", data.Id.ValueString(), data.ClientId.ValueString())
	}
	secret, _ := base64.StdEncoding.DecodeString(data.Secret.ValueString())
	if string(secret) != "dns:///test.union.ai:123456:s3cret:None" {
		t.Errorf("Unexpected decoded secret '%s'", secret)
	}
}

func TestApiKeyResource_ReadDelete_UseClientId(t *testing.T) {
	tests := []struct {
		name     string
		clientId types.String
		secret   string
		expected string
	}{
		{"server-assigned client_id", types.StringValue("123456"), "x", "123456"},
		{"state without client_id uses the ID encoded in secret", types.StringNull(),
			base64.StdEncoding.EncodeToString([]byte("example.com:999999:s3cret:None")), "999999"},
		{"state without client_id or decodable secret falls back to id", types.StringNull(), "x", "my-key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var deleted string
			mock := &mockAppsClient{
				getFn: func(ctx context.Context, req *identity.GetAppRequest) (*identity.GetAppResponse, error) {
					if req.ClientId != tt.expected {
						t.Errorf("Expected Get by '%s', got '%s'", tt.expected, req.ClientId)
					}
					return &identity.GetAppResponse{App: &identity.App{ClientId: req.ClientId}}, nil
				},
				deleteFn: func(ctx context.Context, req *identity.DeleteAppRequest) (*identity.DeleteAppResponse, error) {
					deleted = req.ClientId
					return &identity.DeleteAppResponse{}, nil
				},
			}
			r := &ApiKeyResource{conn: mock, org: "test-org"}
			state := newApiKeyState(t, ApiKeyResourceModel{Id: types.StringValue("my-key"), ClientId: tt.clientId, Secret: types.StringValue(tt.secret)})

			readResp := &resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, readResp)
			if readResp.Diagnostics.HasError() {
				t.Fatalf("Read() returned errors: %v", readResp.Diagnostics.Errors())
			}
			var data ApiKeyResourceModel
			readResp.State.Get(context.Background(), &data)
			if data.ClientId.ValueString() != tt.expected {
				t.Errorf("Expected client_id '%s' after Read, got '%s'", tt.expected, data.ClientId.ValueString())
			}

			deleteResp := &resource.DeleteResponse{State: state}
			r.Delete(context.Background(), resource.DeleteRequest{State: state}, deleteResp)
			if deleteResp.Diagnostics.HasError() {
				t.Fatalf("Delete() returned errors: %v", deleteResp.Diagnostics.Errors())
			}
			if deleted != tt.expected {
				t.Errorf("Expected Delete by '%s', got '%s'", tt.expected, deleted)
			}
		})
	}
}

func TestApiKeyResource_Read_NotFound(t *testing.T) {
	r := &ApiKeyResource{conn: &mockAppsClient{getFn: notFoundGet}, org: "test-org"}
	state := newApiKeyState(t, ApiKeyResourceModel{Id: types.StringValue("my-key"), ClientId: types.StringValue("123456"), Secret: types.StringValue("x")})
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read() should not return errors on NotFound, got: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsNull() {
		t.Error("Expected state to be removed on NotFound")
	}
}
