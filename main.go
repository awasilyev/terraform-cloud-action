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

type workspaceVar struct {
	Key         string      `json:"key"`
	Value       interface{} `json:"value"`
	Description *string     `json:"description"`
	HCL         *bool       `json:"hcl"`
	Sensitive   *bool       `json:"sensitive"`
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

	// Debug: show what we parsed
	fmt.Printf("Debug: Parsed %d variables from JSON input\n", len(vars))
	for i, v := range vars {
		fmt.Printf("Debug: Variable %d: Key=%q, Value=%v (type: %T)\n", i, v.Key, v.Value, v.Value)
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

	// Debug: show workspace details
	fmt.Printf("Debug: Workspace ID: %s, Name: %s\n", w.ID, w.Name)
	fmt.Printf("Debug: Workspace Terraform Version: %s\n", w.TerraformVersion)
	fmt.Printf("Debug: Workspace Execution Mode: %s\n", w.ExecutionMode)

	// Debug: try to list existing variables
	existingVars, listErr := client.Variables.List(ctx, w.ID, &tfe.VariableListOptions{})
	if listErr != nil {
		fmt.Printf("Debug: Could not list existing variables: %v\n", listErr)
	} else {
		fmt.Printf("Debug: Found %d existing variables in workspace\n", existingVars.TotalCount)
	}

	// Debug: check workspace state and permissions
	fmt.Printf("Debug: Workspace State: %s\n", w.State)
	fmt.Printf("Debug: Workspace Locked: %v\n", w.Locked)
	fmt.Printf("Debug: Workspace Auto Apply: %v\n", w.AutoApply)

	// Update the workspace vars
	for _, v := range vars {
		// First try to read the existing variable
		_, err := client.Variables.Read(ctx, w.ID, v.Key)
		if err != nil {
			// Debug: let's see what the actual error is
			fmt.Printf("Debug: Read error for variable %q: %v\n", v.Key, err)
			
			// Check if the error indicates the variable doesn't exist
			// TFE typically returns a 404 or specific error for non-existent variables
			if isVariableNotFoundError(err) {
				// Variable doesn't exist, create it
				category := tfe.CategoryTerraform // Use the proper TFE type
				
				// Convert value to string for TFE
				valueStr := convertValueToString(v.Value)
				
				// Debug: show what we're trying to create
				fmt.Printf("Debug: Creating variable %q with value: %q (type: %T)\n", v.Key, valueStr, v.Value)
				fmt.Printf("Debug: Description: %v, HCL: %v, Sensitive: %v\n", v.Description, v.HCL, v.Sensitive)
				
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
				
				// Try with all fields explicitly set (matching the test examples)
				createOpts := tfe.VariableCreateOptions{
					Key:       &v.Key,
					Value:     &valueStr,
					Category:  &category,
					HCL:       &hcl,
					Sensitive: &sensitive,
				}
				
				// Add description if provided
				if v.Description != nil {
					createOpts.Description = v.Description
				}
				
				// Debug: show final create options
				fmt.Printf("Debug: Final create options: Key=%q, Value=%q, Category=%q, HCL=%v, Sensitive=%v\n", 
					*createOpts.Key, *createOpts.Value, *createOpts.Category, *createOpts.HCL, *createOpts.Sensitive)
				
				_, err = client.Variables.Create(ctx, w.ID, createOpts)
				if err != nil {
					// Debug: show detailed error information
					fmt.Printf("Debug: Create error details: %T: %v\n", err, err)
					fmt.Printf("Debug: Error string: %q\n", err.Error())
					
					// Check if the error is due to the variable already existing
					if err.Error() == "Key has already been taken" {
						// Variable was created by another process, try to update it instead
						fmt.Printf("Variable %q already exists, updating instead\n", v.Key)
						_, updateErr := client.Variables.Update(ctx, w.ID, v.Key, tfe.VariableUpdateOptions{
							Value:       &valueStr,
							Description: v.Description,
							HCL:         v.HCL,
							Sensitive:   v.Sensitive,
						})
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
				// Some other error occurred, return it
				return fmt.Errorf("error reading variable %q: %w", v.Key, err)
			}
		} else {
			// Variable exists, update it
			valueStr := convertValueToString(v.Value)
			_, err = client.Variables.Update(ctx, w.ID, v.Key, tfe.VariableUpdateOptions{
				Value:       &valueStr,
				Description: v.Description,
				HCL:         v.HCL,
				Sensitive:   v.Sensitive,
			})
			if err != nil {
				return fmt.Errorf("could not update variable %q: %w", v.Key, err)
			}
			fmt.Printf("Updated variable %q\n", v.Key)
		}
	}

	// Get a run going!
	r, err := client.Runs.Create(ctx, tfe.RunCreateOptions{
		Workspace: w,
		Refresh:   tfe.Bool(true),
		Message:   &message,
	})
	if err != nil {
		return fmt.Errorf("unable to create run: %w", err)
	}
	runURL := fmt.Sprintf("%s/app/%s/workspaces/%s/runs/%s", url, organization, workspace, r.ID)
	fmt.Println("::set-output name=run-id::" + r.ID)
	fmt.Println("::set-output name=run-url::" + runURL)
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
			fmt.Println("Checking in on run status...")
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
