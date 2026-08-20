package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/1dongdongyang1/gpu-agent/internal/app"
	"github.com/1dongdongyang1/gpu-agent/internal/model"
)

type output struct {
	DiagnosisID       string                `json:"diagnosis_id"`
	Status            model.DiagnosisStatus `json:"status"`
	Termination       *model.Termination    `json:"termination"`
	PlannerRounds     int                   `json:"planner_rounds"`
	ExecutionAttempts int                   `json:"execution_attempts"`
	RejectedCalls     int                   `json:"rejected_calls"`
	ToolPath          []toolStep            `json:"tool_path"`
	Report            model.DiagnosisReport `json:"report"`
}

type toolStep struct {
	ID       string               `json:"id"`
	ToolName string               `json:"tool_name"`
	Status   model.ToolCallStatus `json:"status"`
	TargetID string               `json:"target_id"`
}

func main() {
	scenario := flag.String("scenario", app.HighMemoryScenario, "mock scenario to run")
	flag.Parse()

	state, diagnosisReport, err := app.Run(context.Background(), *scenario)
	if err != nil {
		fmt.Fprintln(os.Stderr, "diagnosis failed:", err)
		os.Exit(1)
	}
	steps := make([]toolStep, 0, len(state.ToolCalls))
	for _, call := range state.ToolCalls {
		steps = append(steps, toolStep{ID: call.ID, ToolName: call.ToolName, Status: call.Status, TargetID: call.TargetID})
	}
	result := output{
		DiagnosisID:       state.DiagnosisID,
		Status:            state.Status,
		Termination:       state.Termination,
		PlannerRounds:     state.PlannerRounds(),
		ExecutionAttempts: state.ExecutionAttempts(),
		RejectedCalls:     state.RejectedCalls(),
		ToolPath:          steps,
		Report:            diagnosisReport,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "encode result:", err)
		os.Exit(1)
	}
}
