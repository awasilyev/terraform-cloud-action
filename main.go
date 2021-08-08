package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
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

type workspaceVar struct {
	Key         string  `json:"key"`
	Value       string  `json:"value"`
	Description *string `json:"description"`
	HCL         *bool   `json:"hcl"`
	Sensitive   *bool   `json:"sensitive"`
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
		_, err = client.Variables.Update(ctx, w.ID, v.Key, tfe.VariableUpdateOptions{
			Value:       &v.Value,
			Description: v.Description,
			HCL:         v.HCL,
			Sensitive:   v.Sensitive,
		})
		if err != nil {
			return fmt.Errorf("could not update variable %q: %w", v.Key, err)
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
