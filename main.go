package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/hashicorp/go-tfe"
)

var (
	tfeToken     = os.Getenv("INPUT_TFE-TOKEN")
	organization = os.Getenv("INPUT_ORGANIZATION")
	workspace    = os.Getenv("INPUT_WORKSPACE")
	jsonVars     = os.Getenv("INPUT_JSON-VARS")
	message      = os.Getenv("INPUT_MESSAGE")
	url          = os.Getenv("INPUT_URL")
	wait         = os.Getenv("INPUT_WAIT")
)

const maximumTimeout = time.Minute * 60

// isVariableNotFoundError checks if the error indicates a variable was not found
func isVariableNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	// Check for common TFE "not found" error patterns
	errStr := err.Error()
	return errStr == "resource not found" ||
		errStr == "variable not found" ||
		errStr == "404" ||
		errStr == "not found"
}

// convertValueToString converts the interface{} value to a string for TFE
func convertValueToString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return fmt.Sprintf("%t", v)
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%f", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// containsHCLSyntax detects if a string value contains HCL syntax
func containsHCLSyntax(value string) bool {
	// Check for common HCL patterns
	return strings.Contains(value, "[") ||
		strings.Contains(value, "]") ||
		strings.Contains(value, "{") ||
		strings.Contains(value, "}") ||
		strings.Contains(value, "=") ||
		strings.Contains(value, ",")
}

// appendToFile appends a key-value pair to the GITHUB_OUTPUT file
func appendToFile(filename, key, value string) error {
	// Use simple key=value format for single-line outputs
	content := fmt.Sprintf("%s=%s\n", key, value)

	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open output file: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return fmt.Errorf("failed to write to output file: %w", err)
	}

	return nil
}

type workspaceVar struct {
	Key         string      `json:"key"`
	Value       interface{} `json:"value"`
	Description *string     `json:"description"`
	HCL         *bool       `json:"hcl"`
	Sensitive   *bool       `json:"sensitive"`
	Category    *string     `json:"category"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseVars() ([]workspaceVar, error) {
	ret := []workspaceVar{}
	err := json.Unmarshal([]byte(jsonVars), &ret)
	return ret, err
}

func run(ctx context.Context, args []string) error {
	vars, err := parseVars()
	if err != nil {
		return fmt.Errorf("could not decode json-vars. Make sure that this is a key-value dictionary of vars to be set: %w", err)
	}

	// Build client
	cfg := tfe.DefaultConfig()
	cfg.Address = url
	cfg.Token = tfeToken
	client, err := tfe.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("unable to create client: %w", err)
	}

	// Get the workspace
	w, err := client.Workspaces.Read(ctx, organization, workspace)
	if err != nil {
		return fmt.Errorf("could not read workspace: %w", err)
	}

	// Update the workspace vars
	for _, v := range vars {
		// Check if variable exists by listing variables and searching for the key
		existingVars, listErr := client.Variables.List(ctx, w.ID, &tfe.VariableListOptions{})
		if listErr != nil {
			return fmt.Errorf("could not list variables: %w", listErr)
		}

		// Search for existing variable with this key and category
		var existingVar *tfe.Variable
		for _, ev := range existingVars.Items {
			if ev.Key == v.Key {
				// If category is specified, also check category match
				if v.Category != nil {
					if ev.Category == tfe.CategoryType(*v.Category) {
						existingVar = ev
						break
					}
				} else {
					// If no category specified, match any category
					existingVar = ev
					break
				}
			}
		}

		if existingVar == nil {
			// Variable doesn't exist, create it

			// Convert value to string for TFE
			valueStr := convertValueToString(v.Value)

			// Detect if this should be treated as HCL (complex values with brackets, braces, etc.)
			isHCL := false
			if v.HCL != nil {
				isHCL = *v.HCL
			} else {
				// Auto-detect HCL for complex values
				valueStr := convertValueToString(v.Value)
				isHCL = containsHCLSyntax(valueStr)
			}

			// Set default values for all fields (matching the test pattern)
			hcl := isHCL
			sensitive := false
			if v.Sensitive != nil {
				sensitive = *v.Sensitive
			}

			// Create variable with TFE helper functions
			createOpts := tfe.VariableCreateOptions{
				Key:       tfe.String(v.Key),
				Value:     tfe.String(valueStr),
				Category:  tfe.Category(tfe.CategoryTerraform), // Default to terraform category
				HCL:       tfe.Bool(hcl),
				Sensitive: tfe.Bool(sensitive),
			}

			// Override category if specified
			if v.Category != nil {
				createOpts.Category = tfe.Category(tfe.CategoryType(*v.Category))
			}

			// Add description if provided
			if v.Description != nil {
				createOpts.Description = v.Description
			}

			_, err = client.Variables.Create(ctx, w.ID, createOpts)

			if err != nil {
				// Check if the error is due to the variable already existing
				if err.Error() == "Key has already been taken" {
					// Variable was created by another process, try to update it instead
					fmt.Printf("Variable %q already exists, updating instead\n", v.Key)
					// We need to get the variable ID first since Update requires it
					updateVars, updateListErr := client.Variables.List(ctx, w.ID, &tfe.VariableListOptions{})
					if updateListErr != nil {
						return fmt.Errorf("could not list variables for update: %w", updateListErr)
					}

					var updateVar *tfe.Variable
					for _, ev := range updateVars.Items {
						if ev.Key == v.Key {
							updateVar = ev
							break
						}
					}

					if updateVar == nil {
						return fmt.Errorf("variable %q not found for update", v.Key)
					}

					updateOpts := tfe.VariableUpdateOptions{
						Value:       &valueStr,
						Description: v.Description,
						HCL:         v.HCL,
						Sensitive:   v.Sensitive,
					}
					if v.Category != nil {
						category := tfe.CategoryType(*v.Category)
						updateOpts.Category = &category
					}
					_, updateErr := client.Variables.Update(ctx, w.ID, updateVar.ID, updateOpts)
					if updateErr != nil {
						return fmt.Errorf("could not update variable %q: %w", v.Key, updateErr)
					}
					fmt.Printf("Updated variable %q\n", v.Key)
				} else {
					return fmt.Errorf("could not create variable %q: %w", v.Key, err)
				}
			} else {
				fmt.Printf("Created variable %q\n", v.Key)
			}
		} else {
			// Variable exists, update it
			valueStr := convertValueToString(v.Value)
			updateOpts := tfe.VariableUpdateOptions{
				Value:       &valueStr,
				Description: v.Description,
				HCL:         v.HCL,
				Sensitive:   v.Sensitive,
			}
			if v.Category != nil {
				category := tfe.CategoryType(*v.Category)
				updateOpts.Category = &category
			}
			_, err = client.Variables.Update(ctx, w.ID, existingVar.ID, updateOpts)
			if err != nil {
				return fmt.Errorf("could not update variable %q: %w", v.Key, err)
			}
			fmt.Printf("Updated variable %q\n", v.Key)
		}
	}

	// Use the latest configuration version instead of creating a new one
	cv, err := client.ConfigurationVersions.List(ctx, w.ID, &tfe.ConfigurationVersionListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list configuration versions: %w", err)
	}
	if len(cv.Items) == 0 {
		return fmt.Errorf("no configuration versions found for workspace")
	}
	// Use the most recent configuration version
	latestCV := cv.Items[0]
	fmt.Printf("Using existing configuration version: %s\n", latestCV.ID)

	// Get a run going!
	r, err := client.Runs.Create(ctx, tfe.RunCreateOptions{
		Workspace:            w,
		ConfigurationVersion: latestCV,
		Refresh:              tfe.Bool(true),
		Message:              &message,
	})
	if err != nil {
		return fmt.Errorf("unable to create run: %w", err)
	}
	runURL := fmt.Sprintf("%s/app/%s/workspaces/%s/runs/%s", url, organization, workspace, r.ID)
	// Write outputs to GITHUB_OUTPUT file for GitHub Actions
	if outputFile := os.Getenv("GITHUB_OUTPUT"); outputFile != "" {
		// Append run-id output
		if err := appendToFile(outputFile, "run-id", r.ID); err != nil {
			fmt.Printf("Warning: could not write run-id output: %v\n", err)
		}
		// Append run-url output
		if err := appendToFile(outputFile, "run-url", runURL); err != nil {
			fmt.Printf("Warning: could not write run-url output: %v\n", err)
		}
	}
	fmt.Println("Run URL: " + runURL)

	if wait != "true" {
		return nil
	}
	fmt.Println("Waiting for run to complete")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(maximumTimeout):
			return fmt.Errorf("run timed out")
		case <-time.After(time.Second * 5):
			checkin, err := client.Runs.Read(ctx, r.ID)
			if err != nil {
				return fmt.Errorf("unable to find run %q: %w", r.ID, err)
			}

			switch checkin.Status {
			case tfe.RunApplied, tfe.RunPlannedAndFinished:
				fmt.Println("run finished successfully")
				return nil
			case tfe.RunCanceled:
				return fmt.Errorf("run was canceled")
			case tfe.RunDiscarded:
				return fmt.Errorf("run was discarded")
			case tfe.RunErrored:
				return fmt.Errorf("run encountered an error")
			}

			// RunApplyQueued        RunStatus = "apply_queued"
			// RunApplying           RunStatus = "applying"
			// RunConfirmed          RunStatus = "confirmed"
			// RunCostEstimated      RunStatus = "cost_estimated"
			// RunCostEstimating     RunStatus = "cost_estimating"
			// RunPending            RunStatus = "pending"
			// RunPlanQueued         RunStatus = "plan_queued"
			// RunPlanned            RunStatus = "planned"
			// RunPlanning           RunStatus = "planning"
			// RunPolicyChecked      RunStatus = "policy_checked"
			// RunPolicyChecking     RunStatus = "policy_checking"
			// RunPolicyOverride     RunStatus = "policy_override"
			// RunPolicySoftFailed   RunStatus = "policy_soft_failed"
		}
	}
}
