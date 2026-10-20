package main

import (
	"encoding/json"
	"errors"

	"github.com/alexgorbatchev/fetch-track-cli/internal/pipeline"
	"github.com/spf13/cobra"
)

// Preserve the command failure even when its structured result cannot be written.
func writeJSONFailure(cmd *cobra.Command, target string, err error) error {
	return errors.Join(err, json.NewEncoder(cmd.OutOrStdout()).Encode(pipeline.JSONExecutionResult{
		Target: target, Status: "error", Error: err.Error(),
	}))
}
