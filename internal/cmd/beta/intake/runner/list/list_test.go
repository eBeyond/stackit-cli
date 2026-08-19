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
	testRegion        = "eu01"
	testLimit         = int64(5)
	testNextPageToken = "next-page-token-123"
)

var (
	testCtx    = context.WithValue(context.Background(), testCtxKey{}, "foo")
	testClient = &intake.APIClient{
		DefaultAPI: &intake.DefaultAPIService{},
	}
	testProjectId = uuid.NewString()
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

func fixtureRequest(mods ...func(request *intake.ApiListIntakeRunnersRequest)) intake.ApiListIntakeRunnersRequest {
	request := testClient.DefaultAPI.ListIntakeRunners(testCtx, testProjectId, testRegion)
	request = request.PageSize(maxPageSize)
	for _, mod := range mods {
		mod(&request)
	}
	return request
}

func TestParseInput(t *testing.T) {
	tests := []struct {
		description   string
		argValues     []string
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
			}, tt.expectedModel, tt.argValues, tt.flagValues, tt.isValid)
		})
	}
}

func TestBuildRequest(t *testing.T) {
	tests := []struct {
		description     string
		model           *inputModel
		expectedRequest intake.ApiListIntakeRunnersRequest
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
	body       intake.ListIntakeRunnersResponse
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

func fixtureIntakeRunners(count int) []intake.IntakeRunnerResponse {
	items := make([]intake.IntakeRunnerResponse, count)
	for i := range count {
		items[i] = intake.IntakeRunnerResponse{
			Id:          fmt.Sprintf("runner-%d", i+1),
			DisplayName: fmt.Sprintf("runner-%d", i+1),
		}
	}
	return items
}

func TestFetchIntakeRunners(t *testing.T) {
	tests := []struct {
		description string
		limit       int64
		responses   []testResponse
		expected    []intake.IntakeRunnerResponse
		fails       bool
	}{
		{
			description: "no items",
			responses: []testResponse{
				fixtureTestResponse(),
			},
			expected: []intake.IntakeRunnerResponse{},
		},
		{
			description: "single item, single page",
			responses: []testResponse{
				fixtureTestResponse(func(resp *testResponse) {
					resp.body.IntakeRunners = fixtureIntakeRunners(1)
				}),
			},
			expected: fixtureIntakeRunners(1),
		},
		{
			description: "multiple pages",
			responses: []testResponse{
				fixtureTestResponse(func(resp *testResponse) {
					resp.body.NextPageToken = utils.Ptr(testNextPageToken)
					resp.body.IntakeRunners = fixtureIntakeRunners(1)
				}),
				fixtureTestResponse(func(resp *testResponse) {
					resp.body.IntakeRunners = fixtureIntakeRunners(1)
				}),
			},
			expected: slices.Concat(fixtureIntakeRunners(1), fixtureIntakeRunners(1)),
		},
		{
			description: "limit stops pagination once reached",
			limit:       1,
			responses: []testResponse{
				fixtureTestResponse(func(resp *testResponse) {
					resp.body.NextPageToken = utils.Ptr(testNextPageToken)
					resp.body.IntakeRunners = fixtureIntakeRunners(1)
				}),
			},
			expected: fixtureIntakeRunners(1),
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
			got, err := fetchIntakeRunners(testCtx, model, client)
			if err != nil {
				if !tt.fails {
					t.Fatalf("fetchIntakeRunners() unexpected error: %v", err)
				}
				return
			}
			if tt.fails {
				t.Fatalf("fetchIntakeRunners() expected an error, got none")
			}
			if callCount != len(tt.responses) {
				t.Errorf("fetchIntakeRunners() expected %d calls, got %d", len(tt.responses), callCount)
			}
			diff := cmp.Diff(got, tt.expected,
				cmpopts.IgnoreFields(intake.IntakeRunnerResponse{}, "AdditionalProperties"),
			)
			if diff != "" {
				t.Errorf("fetchIntakeRunners() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOutputResult(t *testing.T) {
	type args struct {
		outputFormat string
		runners      []intake.IntakeRunnerResponse
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name:    "default output",
			args:    args{outputFormat: "default", runners: []intake.IntakeRunnerResponse{}},
			wantErr: false,
		},
		{
			name:    "json output",
			args:    args{outputFormat: print.JSONOutputFormat, runners: []intake.IntakeRunnerResponse{}},
			wantErr: false,
		},
		{
			name:    "empty slice",
			args:    args{runners: []intake.IntakeRunnerResponse{}},
			wantErr: false,
		},
		{
			name:    "nil slice",
			args:    args{runners: nil},
			wantErr: false,
		},
		{
			name: "empty intake runner in slice",
			args: args{
				runners: []intake.IntakeRunnerResponse{{}},
			},
			wantErr: false,
		},
	}
	params := testparams.NewTestParams()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := outputResult(params.Printer, tt.args.outputFormat, "dummy-projectlabel", tt.args.runners); (err != nil) != tt.wantErr {
				t.Errorf("outputResult() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
