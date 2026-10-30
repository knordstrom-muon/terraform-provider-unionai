package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/unionai/cloud/gen/pb-go/identity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &AppResource{}
var _ resource.ResourceWithImportState = &AppResource{}

func NewAppResource() resource.Resource {
	return &AppResource{}
}

// AppResource defines the resource implementation.
type AppResource struct {
	conn identity.AppsServiceClient
	org  string
}

// AppResourceModel describes the resource data model.
type AppResourceModel struct {
	Id                      types.String `tfsdk:"id"`
	ClientId                types.String `tfsdk:"client_id"`
	ClientName              types.String `tfsdk:"client_name"`
	ClientUri               types.String `tfsdk:"client_uri"`
	ConsentMethod           types.String `tfsdk:"consent_method"`
	GrantTypes              types.Set    `tfsdk:"grant_types"`
	LogoUri                 types.String `tfsdk:"logo_uri"`
	PolicyUri               types.String `tfsdk:"policy_uri"`
	RedirectUris            types.Set    `tfsdk:"redirect_uris"`
	ResponseTypes           types.Set    `tfsdk:"response_types"`
	TokenEndpointAuthMethod types.String `tfsdk:"token_endpoint_auth_method"`
	TosUri                  types.String `tfsdk:"tos_uri"`
	Secret                  types.String `tfsdk:"secret"`
}

func (r *AppResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application"
}

func (r *AppResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "Application resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server-assigned OAuth client ID of the application",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"client_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Requested client ID. Defaults to `id`. Union may assign a different client ID; use `id` for authentication.",
				DeprecationMessage:  "Union assigns client IDs server-side and may ignore this value. Remove it and use `id`; adding it to an existing resource forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"client_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable name of the application",
			},
			"client_uri": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "URI of the application",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"consent_method": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Consent method used by the application",
			},
			"grant_types": schema.SetAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "List of OAuth 2.0 grant types the application may use",
			},
			"logo_uri": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "URI that references a logo for the application",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"policy_uri": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "URI that the application provides to the end-user to read about how the profile data will be used",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"redirect_uris": schema.SetAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "List of redirect URIs for the application",
				Validators:          []validator.Set{setvalidator.SizeAtLeast(1)},
			},
			"response_types": schema.SetAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "List of OAuth 2.0 response types the application may use",
				Validators:          []validator.Set{setvalidator.SizeAtLeast(1)},
			},
			"token_endpoint_auth_method": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Authentication method for the token endpoint",
			},
			"tos_uri": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "URI that the client provides to the end-user to read about the client's terms of service",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"secret": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Application secret",
			},
		},
	}
}

func (r *AppResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*providerContext)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *providerContext, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.conn = identity.NewAppsServiceClient(client.conn)
	if r.conn == nil {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *identity.AppsServiceClient, got: %T. Please report this issue to the provider developers.", r.conn),
		)
		return
	}
	r.org = client.org
}

func (r *AppResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AppResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if !data.ClientId.IsUnknown() {
		if _, err := r.conn.Get(ctx, &identity.GetAppRequest{
			Organization: r.org,
			ClientId:     data.ClientId.ValueString(),
		}); err == nil {
			resp.Diagnostics.AddError(
				"Application already exists",
				fmt.Sprintf("Application with client ID %s already exists", data.ClientId.ValueString()),
			)
			return
		}
	}

	createRequest := &identity.CreateAppRequest{
		Organization:                 r.org,
		ClientId:                     data.ClientId.ValueString(),
		ClientName:                   data.ClientName.ValueString(),
		ClientUri:                    data.ClientUri.ValueString(),
		LogoUri:                      data.LogoUri.ValueString(),
		PolicyUri:                    data.PolicyUri.ValueString(),
		RedirectUris:                 convertSetToStrings(data.RedirectUris),
		SkipDefaultPolicyAssignments: true,
		TosUri:                       data.TosUri.ValueString(),
	}

	if consent, ok := identity.ConsentMethod_value[strings.ToUpper(data.ConsentMethod.ValueString())]; !ok {
		resp.Diagnostics.AddError(
			"Invalid Consent Method",
			fmt.Sprintf("Invalid consent method: %s", data.ConsentMethod.ValueString()),
		)
		return
	} else {
		createRequest.ConsentMethod = identity.ConsentMethod(consent)
	}

	for _, grantType := range convertSetToStrings(data.GrantTypes) {
		grant, ok := identity.GrantTypes_value[strings.ToUpper(grantType)]
		if !ok {
			resp.Diagnostics.AddError(
				"Invalid Grant Type",
				fmt.Sprintf("Invalid grant type: %s", grantType),
			)
			return
		}
		createRequest.GrantTypes = append(createRequest.GrantTypes, identity.GrantTypes(grant))
	}

	for _, responseType := range convertSetToStrings(data.ResponseTypes) {
		response, ok := identity.ResponseTypes_value[strings.ToUpper(responseType)]
		if !ok {
			resp.Diagnostics.AddError(
				"Invalid Response Type",
				fmt.Sprintf("Invalid response type: %s", responseType),
			)
			return
		}
		createRequest.ResponseTypes = append(createRequest.ResponseTypes, identity.ResponseTypes(response))
	}

	tokenEndpointAuthMethod, ok := identity.TokenEndpointAuthMethod_value[strings.ToUpper(data.TokenEndpointAuthMethod.ValueString())]
	if !ok {
		resp.Diagnostics.AddError(
			"Invalid Token Endpoint Auth Method",
			fmt.Sprintf("Invalid token endpoint auth method: %s", data.TokenEndpointAuthMethod.ValueString()),
		)
		return
	}
	createRequest.TokenEndpointAuthMethod = identity.TokenEndpointAuthMethod(tokenEndpointAuthMethod)

	app, err := r.conn.Create(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("Unable to create oauth app, got error: %s", err),
		)
		return
	}

	data.Id = types.StringValue(app.App.ClientId)
	if data.ClientId.IsUnknown() {
		data.ClientId = data.Id
	}
	data.Secret = types.StringValue(app.App.ClientSecret)

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AppResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data AppResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	app, err := r.conn.Get(ctx, &identity.GetAppRequest{
		Organization: r.org,
		ClientId:     data.Id.ValueString(),
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading UnionAI Application",
			fmt.Sprintf("Error reading UnionAI Application %s: %s", data.Id.ValueString(), err),
		)
		return
	}

	// A requested client_id may differ from the assigned one; overwriting it would force replacement.
	if data.ClientId.IsNull() {
		data.ClientId = types.StringValue(app.App.ClientId)
	}
	// Older versions stored the requested client ID as id; the server still resolves it.
	data.Id = types.StringValue(app.App.ClientId)
	data.ClientName = types.StringValue(app.App.ClientName)
	data.ClientUri = stringOrNull(app.App.ClientUri)
	data.GrantTypes = convertArrayToSetGetter(app.App.GrantTypes, func(g identity.GrantTypes) string {
		return identity.GrantTypes_name[int32(g)]
	})
	data.LogoUri = stringOrNull(app.App.LogoUri)
	data.PolicyUri = stringOrNull(app.App.PolicyUri)
	// Empty server values are stored as null (unset), including over "" / [] written by older versions.
	data.RedirectUris = types.SetNull(types.StringType)
	if len(app.App.RedirectUris) > 0 {
		data.RedirectUris = convertStringsToSet(app.App.RedirectUris)
	}
	data.ResponseTypes = types.SetNull(types.StringType)
	if len(app.App.ResponseTypes) > 0 {
		data.ResponseTypes = convertArrayToSetGetter(app.App.ResponseTypes, func(r identity.ResponseTypes) string {
			return identity.ResponseTypes_name[int32(r)]
		})
	}
	data.TokenEndpointAuthMethod = types.StringValue(identity.TokenEndpointAuthMethod_name[int32(app.App.TokenEndpointAuthMethod)])
	data.TosUri = stringOrNull(app.App.TosUri)

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AppResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AppResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	updateRequest := &identity.UpdateAppRequest{
		Organization: r.org,
		ClientId:     data.Id.ValueString(),
		ClientName:   data.ClientName.ValueString(),
		ClientUri:    data.ClientUri.ValueString(),
		LogoUri:      data.LogoUri.ValueString(),
		PolicyUri:    data.PolicyUri.ValueString(),
		RedirectUris: convertSetToStrings(data.RedirectUris),
		TosUri:       data.TosUri.ValueString(),
	}

	for _, grantType := range convertSetToStrings(data.GrantTypes) {
		grant, ok := identity.GrantTypes_value[strings.ToUpper(grantType)]
		if !ok {
			resp.Diagnostics.AddError(
				"Invalid Grant Type",
				fmt.Sprintf("Invalid grant type: %s", grantType),
			)
			return
		}
		updateRequest.GrantTypes = append(updateRequest.GrantTypes, identity.GrantTypes(grant))
	}

	for _, responseType := range convertSetToStrings(data.ResponseTypes) {
		response, ok := identity.ResponseTypes_value[strings.ToUpper(responseType)]
		if !ok {
			resp.Diagnostics.AddError(
				"Invalid Response Type",
				fmt.Sprintf("Invalid response type: %s", responseType),
			)
			return
		}
		updateRequest.ResponseTypes = append(updateRequest.ResponseTypes, identity.ResponseTypes(response))
	}

	tokenEndpointAuthMethod, ok := identity.TokenEndpointAuthMethod_value[strings.ToUpper(data.TokenEndpointAuthMethod.ValueString())]
	if !ok {
		resp.Diagnostics.AddError(
			"Invalid Token Endpoint Auth Method",
			fmt.Sprintf("Invalid token endpoint auth method: %s", data.TokenEndpointAuthMethod.ValueString()),
		)
		return
	}
	updateRequest.TokenEndpointAuthMethod = identity.TokenEndpointAuthMethod(tokenEndpointAuthMethod)

	updateResp, err := r.conn.Update(ctx, updateRequest)
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("Unable to update oauth app, got error: %s", err),
		)
		return
	}

	// UpdateApp rotates the client secret; keep the prior one only if none is returned.
	if secret := updateResp.GetApp().GetClientSecret(); secret != "" {
		data.Secret = types.StringValue(secret)
	} else {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("secret"), &data.Secret)...)
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AppResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AppResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.conn.Delete(ctx, &identity.DeleteAppRequest{
		Organization: r.org,
		ClientId:     data.Id.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Deleting UnionAI Application",
			fmt.Sprintf("Error deleting UnionAI Application %s: %s", data.Id.ValueString(), err),
		)
		return
	}
}

func (r *AppResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func stringOrNull(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}
