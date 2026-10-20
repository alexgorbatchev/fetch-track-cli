package main

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/dj/fetch-track-cli/internal/deps"

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
	var operationErr error
	if action != "inspect" {
		operationErr = deps.InitManagedPath()
		if operationErr == nil {
			if len(args) == 0 {
				if action == "install" {
					result.Changed, operationErr = deps.InstallMissingDependencies(cmd.Context())
				} else {
					result.Changed, operationErr = deps.UpdateAllDependencies(cmd.Context())
				}
			} else {
				for _, name := range args {
					var err error
					if action == "install" {
						err = deps.InstallDependency(cmd.Context(), name)
					} else {
						err = deps.UpdateDependency(cmd.Context(), name)
					}
					if err != nil {
						operationErr = errors.Join(operationErr, err)
					} else {
						result.Changed = append(result.Changed, name)
					}
				}
			}
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
