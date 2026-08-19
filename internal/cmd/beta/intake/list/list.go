package list

import (
	"context"
	"fmt"
	"math"

	"github.com/spf13/cobra"
	intake "github.com/stackitcloud/stackit-sdk-go/services/intake/v1betaapi"

	"github.com/stackitcloud/stackit-cli/internal/pkg/args"
	cliErr "github.com/stackitcloud/stackit-cli/internal/pkg/errors"
	"github.com/stackitcloud/stackit-cli/internal/pkg/examples"
	"github.com/stackitcloud/stackit-cli/internal/pkg/flags"
	"github.com/stackitcloud/stackit-cli/internal/pkg/globalflags"
	"github.com/stackitcloud/stackit-cli/internal/pkg/print"
	"github.com/stackitcloud/stackit-cli/internal/pkg/projectname"
	"github.com/stackitcloud/stackit-cli/internal/pkg/services/intake/client"
	"github.com/stackitcloud/stackit-cli/internal/pkg/tables"
	"github.com/stackitcloud/stackit-cli/internal/pkg/types"
)

const (
	limitFlag = "limit"

	// maxPageSize is the maximum number of items the API returns per page.
	maxPageSize = int32(100)
)

// inputModel struct holds all the input parameters for the command
type inputModel struct {
	*globalflags.GlobalFlagModel
	Limit *int64
}

// NewCmd creates a new cobra command for listing Intakes
func NewCmd(p *types.CmdParams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lists all Intakes",
		Long:  "Lists all Intakes for the current project.",
		Args:  args.NoArgs,
		Example: examples.Build(
			examples.NewExample(
				`List all Intakes`,
				`$ stackit beta intake list`),
			examples.NewExample(
				`List all Intakes in JSON format`,
				`$ stackit beta intake list --output-format json`),
			examples.NewExample(
				`List up to 5 Intakes`,
				`$ stackit beta intake list --limit 5`),
		),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			model, err := parseInput(p.Printer, cmd)
			if err != nil {
				return err
			}

			// Configure API client
			apiClient, err := client.ConfigureClient(p.Printer, p.CliVersion)
			if err != nil {
				return err
			}

			// Call API
			intakes, err := fetchIntakes(ctx, model, apiClient)
			if err != nil {
				return fmt.Errorf("list Intakes: %w", err)
			}

			projectLabel := model.ProjectId
			if len(intakes) == 0 {
				projectLabel, err = projectname.GetProjectName(ctx, p.Printer, p.CliVersion, cmd)
				if err != nil {
					p.Printer.Debug(print.ErrorLevel, "get project name: %v", err)
				}
			}

			return outputResult(p.Printer, model.OutputFormat, projectLabel, intakes)
		},
	}
	configureFlags(cmd)
	return cmd
}

// configureFlags adds the --limit flag to the command
func configureFlags(cmd *cobra.Command) {
	cmd.Flags().Int64(limitFlag, 0, "Maximum number of entries to list")
}

// parseInput parses the command flags into a standardized model
func parseInput(p *print.Printer, cmd *cobra.Command) (*inputModel, error) {
	globalFlags := globalflags.Parse(p, cmd)
	if globalFlags.ProjectId == "" {
		return nil, &cliErr.ProjectIdError{}
	}

	limit := flags.FlagToInt64Pointer(p, cmd, limitFlag)
	if limit != nil && *limit < 1 {
		return nil, &cliErr.FlagValidationError{
			Flag:    limitFlag,
			Details: "must be greater than 0",
		}
	}

	model := inputModel{
		GlobalFlagModel: globalFlags,
		Limit:           limit,
	}

	p.DebugInputModel(model)
	return &model, nil
}

// buildRequest creates the API request to list Intakes
func buildRequest(ctx context.Context, model *inputModel, apiClient *intake.APIClient, pageToken string, pageSize int32) intake.ApiListIntakesRequest {
	req := apiClient.DefaultAPI.ListIntakes(ctx, model.ProjectId, model.Region)
	req = req.PageSize(pageSize)
	if pageToken != "" {
		req = req.PageToken(pageToken)
	}
	return req
}

// fetchIntakes retrieves Intakes from the API, following pagination until either the requested
// limit is reached or no more pages remain.
func fetchIntakes(ctx context.Context, model *inputModel, apiClient *intake.APIClient) ([]intake.IntakeResponse, error) {
	var pageToken string
	intakes := make([]intake.IntakeResponse, 0)
	received := int64(0)
	limit := int64(math.MaxInt64)
	if model.Limit != nil {
		limit = *model.Limit
	}
	for {
		want := min(int64(maxPageSize), limit-received)
		request := buildRequest(ctx, model, apiClient, pageToken, int32(want))
		response, err := request.Execute()
		if err != nil {
			return nil, fmt.Errorf("list Intakes: %w", err)
		}
		intakes = append(intakes, response.GetIntakes()...)
		pageToken = response.GetNextPageToken()
		received += want
		if pageToken == "" || received >= limit {
			break
		}
	}
	return intakes, nil
}

// outputResult formats the API response and prints it to the console
func outputResult(p *print.Printer, outputFormat, projectLabel string, intakes []intake.IntakeResponse) error {
	return p.OutputResult(outputFormat, intakes, func() error {
		if len(intakes) == 0 {
			p.Outputf("No intakes found for project %q\n", projectLabel)
			return nil
		}

		table := tables.NewTable()
		table.SetHeader("ID", "NAME", "STATE", "RUNNER ID")
		for i := range intakes {
			intakeItem := intakes[i]
			table.AddRow(
				intakeItem.GetId(),
				intakeItem.GetDisplayName(),
				intakeItem.GetState(),
				intakeItem.GetIntakeRunnerId(),
			)
		}
		err := table.Display(p)
		if err != nil {
			return fmt.Errorf("render table: %w", err)
		}
		return nil
	})
}
