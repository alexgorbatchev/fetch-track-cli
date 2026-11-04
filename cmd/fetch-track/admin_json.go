package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/alexgorbatchev/fetch-track-cli/internal/deps"

	"github.com/spf13/cobra"
)

type dependencyJSONResult struct {
	Status       string                  `json:"status"`
	Dependencies []deps.DependencyReport `json:"dependencies"`
	Changed      []string                `json:"changed"`
	Error        string                  `json:"error,omitempty"`
}

// runDepsJSON uses the same dependency manager as the interactive commands.
func runDepsJSON(cmd *cobra.Command, args []string, action string) error {
	result := dependencyJSONResult{Status: "ok", Changed: []string{}}
	// Inspection reports the copies acquisition would use, so it needs the
	// managed directories on PATH too; like acquisition, it still inspects
	// PATH when they cannot be added.
	pathErr := deps.InitManagedPath(cmd.Context())
	var operationErr error
	if action != "inspect" {
		operationErr = pathErr
		if operationErr == nil {
			result.Changed, operationErr = changeDependencies(cmd.Context(), args, action)
		}
	}
	reports, verifyErr := deps.VerifyDependencies(cmd.Context())
	result.Dependencies = reports
	if result.Dependencies == nil {
		result.Dependencies = []deps.DependencyReport{}
	}
	if result.Changed == nil {
		result.Changed = []string{}
	}
	operationErr = errors.Join(operationErr, verifyErr)
	if operationErr != nil {
		result.Status = "error"
		result.Error = operationErr.Error()
	}
	return errors.Join(operationErr, json.NewEncoder(cmd.OutOrStdout()).Encode(result))
}

func runUpgradeJSON(cmd *cobra.Command, current string) error {
	result := struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Error   string `json:"error,omitempty"`
	}{Status: "ok", Version: current}
	latest, err := deps.UpgradeSelf(cmd.Context(), current)
	if err == nil {
		result.Version = latest
	} else if strings.Contains(err.Error(), "already at the latest version") {
		result.Status = "current"
		err = nil
	} else {
		result.Status = "error"
		result.Error = err.Error()
	}
	return errors.Join(err, json.NewEncoder(cmd.OutOrStdout()).Encode(result))
}

// changeDependencies installs or updates the named dependencies, or every
// dependency that needs it when names is empty, and returns the changed ones.
func changeDependencies(ctx context.Context, names []string, action string) ([]string, error) {
	if len(names) == 0 {
		if action == "install" {
			return deps.InstallMissingDependencies(ctx)
		}
		return deps.UpdateAllDependencies(ctx)
	}
	var changed []string
	var errs error
	for _, name := range names {
		var err error
		if action == "install" {
			err = deps.InstallDependency(ctx, name)
		} else {
			err = deps.UpdateDependency(ctx, name)
		}
		if err != nil {
			errs = errors.Join(errs, err)
		} else {
			changed = append(changed, name)
		}
	}
	return changed, errs
}
