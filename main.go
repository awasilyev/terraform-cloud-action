package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/hashicorp/go-tfe"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	// Parse args
	if len(args) < 5 {
		return fmt.Errorf("insufficient arguments supplied")
	}
	tfeToken := args[0]
	organization := args[1]
	workspace := args[2]
	message := args[3]
	url := args[4]

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

	// Get a run going!
	r, err := client.Runs.Create(ctx, tfe.RunCreateOptions{
		Workspace: w,
		Refresh:   tfe.Bool(true),
		Message:   &message,
	})
	if err != nil {
		return fmt.Errorf("unable to create run: %w", err)
	}
	fmt.Printf("::set-output name=run-url::%s/app/%s/workspaces/%s/runs/%s\n", url, organization, workspace, r.ID)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second * 2):
			fmt.Println("checking in on run status")
			checkin, err := client.Runs.Read(ctx, r.ID)
			if err != nil {
				return fmt.Errorf("unable to find created run: %w", err)
			}

			switch checkin.Status {
			case tfe.RunApplied, tfe.RunPlannedAndFinished:
				fmt.Println("run finished successfully")
				return nil
			case tfe.RunCanceled, tfe.RunDiscarded, tfe.RunErrored:
				return fmt.Errorf("run did not complete successfully")
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
