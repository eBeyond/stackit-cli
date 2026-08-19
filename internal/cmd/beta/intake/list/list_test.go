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

	"github.com/stackitcloud/stackit-cli/internal/pkg/globalflags"
	"github.com/stackitcloud/stackit-cli/internal/pkg/print"
	"github.com/stackitcloud/stackit-cli/internal/pkg/testparams"
	"github.com/stackitcloud/stackit-cli/internal/pkg/testutils"
	"github.com/stackitcloud/stackit-cli/internal/pkg/utils"
)

type testCtxKey struct{}

const (
	testRegion = "eu01"
)

var (
	testCtx    = context.WithValue(context.Background(), testCtxKey{}, "foo")
	testClient = &intake.APIClient{
		DefaultAPI: &intake.DefaultAPIService{},
	}
	testProjectId     = uuid.NewString()
	testLimit         = int64(5)
	testNextPageToken = "next-page-token-123"
)

func fixtureFlagValues(mods ...func(flagValues map[string]string)) map[string]string {
	flagValues := map[string]string{
		globalflags.ProjectIdFlag: testProjectId,
		globalflags.RegionFlag:    testRegion,
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
	}
	for _, mod := range mods {
		mod(model)
	}
	return model
}

func fixtureRequest(mods ...func(request *intake.ApiListIntakesRequest)) intake.ApiListIntakesRequest {
	request := testClient.DefaultAPI.ListIntakes(testCtx, testProjectId, testRegion)
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
		expectedRequest intake.ApiListIntakesRequest
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
				cmpopts.EquateComparable(testClient.DefaultAPI),
			)
			if diff != "" {
				t.Fatalf("Data does not match: %s", diff)
			}
		})
	}
}

type testResponse struct {
	statusCode int
	body       intake.ListIntakesResponse
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

func fixtureIntakes(count int) []intake.IntakeResponse {
	items := make([]intake.IntakeResponse, count)
	for i := range count {
		items[i] = intake.IntakeResponse{
			Id:          fmt.Sprintf("intake-%d", i+1),
			DisplayName: fmt.Sprintf("intake-%d", i+1),
		}
	}
	return items
}

func TestFetchIntakes(t *testing.T) {
	tests := []struct {
		description string
		limit       int64
		responses   []testResponse
		expected    []intake.IntakeResponse
		fails       bool
	}{
		{
			description: "no items",
			responses: []testResponse{
				fixtureTestResponse(),
			},
			expected: []intake.IntakeResponse{},
		},
		{
			description: "single item, single page",
			responses: []testResponse{
				fixtureTestResponse(func(resp *testResponse) {
					resp.body.Intakes = fixtureIntakes(1)
				}),
			},
			expected: fixtureIntakes(1),
		},
		{
			description: "multiple pages",
			responses: []testResponse{
				fixtureTestResponse(func(resp *testResponse) {
					resp.body.NextPageToken = utils.Ptr(testNextPageToken)
					resp.body.Intakes = fixtureIntakes(1)
				}),
				fixtureTestResponse(func(resp *testResponse) {
					resp.body.Intakes = fixtureIntakes(1)
				}),
			},
			expected: slices.Concat(fixtureIntakes(1), fixtureIntakes(1)),
		},
		{
			description: "limit stops pagination once reached",
			limit:       1,
			responses: []testResponse{
				fixtureTestResponse(func(resp *testResponse) {
					resp.body.NextPageToken = utils.Ptr(testNextPageToken)
					resp.body.Intakes = fixtureIntakes(1)
				}),
			},
			expected: fixtureIntakes(1),
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
			got, err := fetchIntakes(testCtx, model, client)
			if err != nil {
				if !tt.fails {
					t.Fatalf("fetchIntakes() unexpected error: %v", err)
				}
				return
			}
			if tt.fails {
				t.Fatalf("fetchIntakes() expected an error, got none")
			}
			if callCount != len(tt.responses) {
				t.Errorf("fetchIntakes() expected %d calls, got %d", len(tt.responses), callCount)
			}
			diff := cmp.Diff(got, tt.expected,
				cmpopts.IgnoreFields(intake.IntakeResponse{}, "AdditionalProperties"),
				cmpopts.IgnoreFields(intake.IntakeCatalog{}, "AdditionalProperties"),
			)
			if diff != "" {
				t.Errorf("fetchIntakes() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOutputResult(t *testing.T) {
	type args struct {
		outputFormat string
		projectLabel string
		intakes      []intake.IntakeResponse
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name:    "default output",
			args:    args{outputFormat: "default", intakes: []intake.IntakeResponse{}},
			wantErr: false,
		},
		{
			name:    "json output",
			args:    args{outputFormat: print.JSONOutputFormat, intakes: []intake.IntakeResponse{}},
			wantErr: false,
		},
		{
			name:    "empty slice",
			args:    args{intakes: []intake.IntakeResponse{}},
			wantErr: false,
		},
		{
			name:    "nil slice",
			args:    args{intakes: nil},
			wantErr: false,
		},
		{
			name: "empty intake in slice",
			args: args{
				intakes: []intake.IntakeResponse{{}},
			},
			wantErr: false,
		},
		{
			name: "with project label",
			args: args{
				projectLabel: "my-project",
				intakes:      []intake.IntakeResponse{},
			},
			wantErr: false,
		},
	}
	params := testparams.NewTestParams()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := outputResult(params.Printer, tt.args.outputFormat, tt.args.projectLabel, tt.args.intakes); (err != nil) != tt.wantErr {
				t.Errorf("outputResult() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
