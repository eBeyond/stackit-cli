package list

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	sdkConfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	intake "github.com/stackitcloud/stackit-sdk-go/services/intake/v1betaapi"

	"github.com/stackitcloud/stackit-cli/internal/pkg/testparams"

	"github.com/stackitcloud/stackit-cli/internal/pkg/globalflags"
	"github.com/stackitcloud/stackit-cli/internal/pkg/print"
	"github.com/stackitcloud/stackit-cli/internal/pkg/testutils"
	"github.com/stackitcloud/stackit-cli/internal/pkg/utils"
)

type testCtxKey struct{}

const (
	testRegion        = "eu01"
	testNextPageToken = "next-page-token-123"
)

var (
	testCtx    = context.WithValue(context.Background(), testCtxKey{}, "foo")
	testClient = &intake.APIClient{
		DefaultAPI: &intake.DefaultAPIService{},
	}
	testProjectId = uuid.NewString()
	testIntakeId  = uuid.NewString()
	testLimit     = int64(5)
)

func fixtureFlagValues(mods ...func(flagValues map[string]string)) map[string]string {
	flagValues := map[string]string{
		globalflags.ProjectIdFlag: testProjectId,
		globalflags.RegionFlag:    testRegion,
		intakeIdFlag:              testIntakeId,
	}
	for _, mod := range mods {
		mod(flagValues)
	}
	return flagValues
}

func fixtureInputModel(mods ...func(model *inputModel)) *inputModel {
	model := &inputModel{
		GlobalFlagModel: &globalflags.GlobalFlagModel{
			ProjectId: testProjectId,
			Region:    testRegion,
			Verbosity: globalflags.VerbosityDefault,
		},
		IntakeId: utils.Ptr(testIntakeId),
	}
	for _, mod := range mods {
		mod(model)
	}
	return model
}

func fixtureRequest(mods ...func(request *intake.ApiListIntakeUsersRequest)) intake.ApiListIntakeUsersRequest {
	request := testClient.DefaultAPI.ListIntakeUsers(testCtx, testProjectId, testRegion, testIntakeId)
	request = request.PageSize(maxPageSize)
	for _, mod := range mods {
		mod(&request)
	}
	return request
}

func TestParseInput(t *testing.T) {
	tests := []struct {
		description   string
		flagValues    map[string]string
		isValid       bool
		expectedModel *inputModel
	}{
		{
			description:   "base",
			flagValues:    fixtureFlagValues(),
			isValid:       true,
			expectedModel: fixtureInputModel(),
		},
		{
			description: "with limit",
			flagValues: fixtureFlagValues(func(flagValues map[string]string) {
				flagValues[limitFlag] = strconv.FormatInt(testLimit, 10)
			}),
			isValid: true,
			expectedModel: fixtureInputModel(func(model *inputModel) {
				model.Limit = utils.Ptr(testLimit)
			}),
		},
		{
			description: "project id missing",
			flagValues: fixtureFlagValues(func(flagValues map[string]string) {
				delete(flagValues, globalflags.ProjectIdFlag)
			}),
			isValid: false,
		},
		{
			description: "intake id missing",
			flagValues: fixtureFlagValues(func(flagValues map[string]string) {
				delete(flagValues, intakeIdFlag)
			}),
			isValid: false,
		},
		{
			description: "intake id invalid",
			flagValues: fixtureFlagValues(func(flagValues map[string]string) {
				flagValues[intakeIdFlag] = "invalid-uuid"
			}),
			isValid: false,
		},
		{
			description: "limit is zero",
			flagValues: fixtureFlagValues(func(flagValues map[string]string) {
				flagValues[limitFlag] = "0"
			}),
			isValid: false,
		},
		{
			description: "limit is negative",
			flagValues: fixtureFlagValues(func(flagValues map[string]string) {
				flagValues[limitFlag] = "-1"
			}),
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			testutils.TestParseInput(t, NewCmd, func(p *print.Printer, cmd *cobra.Command, _ []string) (*inputModel, error) {
				return parseInput(p, cmd)
			}, tt.expectedModel, nil, tt.flagValues, tt.isValid)
		})
	}
}

func TestBuildRequest(t *testing.T) {
	tests := []struct {
		description     string
		model           *inputModel
		expectedRequest intake.ApiListIntakeUsersRequest
	}{
		{
			description:     "base",
			model:           fixtureInputModel(),
			expectedRequest: fixtureRequest(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			request := buildRequest(testCtx, tt.model, testClient, "", maxPageSize)

			diff := cmp.Diff(request, tt.expectedRequest,
				cmp.AllowUnexported(tt.expectedRequest),
				cmpopts.EquateComparable(testCtx),
				cmpopts.IgnoreUnexported(intake.DefaultAPIService{}),
			)
			if diff != "" {
				t.Fatalf("Data does not match: %s", diff)
			}
		})
	}
}

type testResponse struct {
	statusCode int
	body       intake.ListIntakeUsersResponse
}

func fixtureTestResponse(mods ...func(resp *testResponse)) testResponse {
	resp := testResponse{
		statusCode: 200,
	}
	for _, mod := range mods {
		mod(&resp)
	}
	return resp
}

func fixtureIntakeUsers(count int) []intake.IntakeUserResponse {
	items := make([]intake.IntakeUserResponse, count)
	for i := range count {
		items[i] = intake.IntakeUserResponse{
			Id:          fmt.Sprintf("user-%d", i+1),
			DisplayName: fmt.Sprintf("user-%d", i+1),
			Type:        intake.USERTYPE_INTAKE,
		}
	}
	return items
}

func TestFetchIntakeUsers(t *testing.T) {
	tests := []struct {
		description string
		limit       int64
		responses   []testResponse
		expected    []intake.IntakeUserResponse
		fails       bool
	}{
		{
			description: "no items",
			responses: []testResponse{
				fixtureTestResponse(),
			},
			expected: []intake.IntakeUserResponse{},
		},
		{
			description: "single item, single page",
			responses: []testResponse{
				fixtureTestResponse(func(resp *testResponse) {
					resp.body.IntakeUsers = fixtureIntakeUsers(1)
				}),
			},
			expected: fixtureIntakeUsers(1),
		},
		{
			description: "multiple pages",
			responses: []testResponse{
				fixtureTestResponse(func(resp *testResponse) {
					resp.body.NextPageToken = utils.Ptr(testNextPageToken)
					resp.body.IntakeUsers = fixtureIntakeUsers(1)
				}),
				fixtureTestResponse(func(resp *testResponse) {
					resp.body.IntakeUsers = fixtureIntakeUsers(1)
				}),
			},
			expected: slices.Concat(fixtureIntakeUsers(1), fixtureIntakeUsers(1)),
		},
		{
			description: "limit stops pagination once reached",
			limit:       1,
			responses: []testResponse{
				fixtureTestResponse(func(resp *testResponse) {
					resp.body.NextPageToken = utils.Ptr(testNextPageToken)
					resp.body.IntakeUsers = fixtureIntakeUsers(1)
				}),
			},
			expected: fixtureIntakeUsers(1),
		},
		{
			description: "API error",
			responses: []testResponse{
				fixtureTestResponse(func(resp *testResponse) {
					resp.statusCode = 500
				}),
			},
			fails: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			callCount := 0
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := tt.responses[callCount]
				callCount++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(resp.statusCode)
				bs, err := json.Marshal(resp.body)
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				_, err = w.Write(bs)
				if err != nil {
					t.Fatalf("write: %v", err)
				}
			})
			server := httptest.NewServer(handler)
			defer server.Close()
			client, err := intake.NewAPIClient(
				sdkConfig.WithEndpoint(server.URL),
				sdkConfig.WithoutAuthentication(),
			)
			if err != nil {
				t.Fatalf("failed to create test client: %v", err)
			}
			var mods []func(m *inputModel)
			if tt.limit > 0 {
				mods = append(mods, func(m *inputModel) {
					m.Limit = utils.Ptr(tt.limit)
				})
			}
			model := fixtureInputModel(mods...)
			got, err := fetchIntakeUsers(testCtx, model, client)
			if err != nil {
				if !tt.fails {
					t.Fatalf("fetchIntakeUsers() unexpected error: %v", err)
				}
				return
			}
			if tt.fails {
				t.Fatalf("fetchIntakeUsers() expected an error, got none")
			}
			if callCount != len(tt.responses) {
				t.Errorf("fetchIntakeUsers() expected %d calls, got %d", len(tt.responses), callCount)
			}
			diff := cmp.Diff(got, tt.expected,
				cmpopts.IgnoreFields(intake.IntakeUserResponse{}, "AdditionalProperties"),
			)
			if diff != "" {
				t.Errorf("fetchIntakeUsers() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOutputResult(t *testing.T) {
	type args struct {
		outputFormat string
		projectLabel string
		intakeId     string
		users        []intake.IntakeUserResponse
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name:    "default output",
			args:    args{outputFormat: "default", intakeId: testIntakeId, users: []intake.IntakeUserResponse{}},
			wantErr: false,
		},
		{
			name:    "json output",
			args:    args{outputFormat: print.JSONOutputFormat, intakeId: testIntakeId, users: []intake.IntakeUserResponse{}},
			wantErr: false,
		},
		{
			name:    "empty slice",
			args:    args{intakeId: testIntakeId, users: []intake.IntakeUserResponse{}},
			wantErr: false,
		},
		{
			name:    "nil slice",
			args:    args{intakeId: testIntakeId, users: nil},
			wantErr: false,
		},
		{
			name: "empty user in slice",
			args: args{
				intakeId: testIntakeId,
				users:    []intake.IntakeUserResponse{{}},
			},
			wantErr: false,
		},
		{
			name: "with project label",
			args: args{
				projectLabel: "my-project",
				intakeId:     testIntakeId,
				users:        []intake.IntakeUserResponse{},
			},
			wantErr: false,
		},
	}
	params := testparams.NewTestParams()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := outputResult(params.Printer, tt.args.outputFormat, tt.args.projectLabel, tt.args.intakeId, tt.args.users); (err != nil) != tt.wantErr {
				t.Errorf("outputResult() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
