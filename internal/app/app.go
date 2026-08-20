package app

import (
	"context"
	"fmt"

	"github.com/1dongdongyang1/gpu-agent/internal/diagnosis"
	"github.com/1dongdongyang1/gpu-agent/internal/idgen"
	"github.com/1dongdongyang1/gpu-agent/internal/mock"
	"github.com/1dongdongyang1/gpu-agent/internal/model"
	"github.com/1dongdongyang1/gpu-agent/internal/planner"
	"github.com/1dongdongyang1/gpu-agent/internal/report"
	"github.com/1dongdongyang1/gpu-agent/internal/tools"
)

const HighMemoryScenario = "high-memory"

func Run(ctx context.Context, scenarioName string) (model.DiagnosisState, model.DiagnosisReport, error) {
	if scenarioName != HighMemoryScenario {
		return model.DiagnosisState{}, model.DiagnosisReport{}, fmt.Errorf("unsupported scenario %q", scenarioName)
	}
	scenario := mock.HighMemoryScenario()
	if err := scenario.Validate(); err != nil {
		return model.DiagnosisState{}, model.DiagnosisReport{}, fmt.Errorf("invalid scenario: %w", err)
	}
	state, err := model.NewDiagnosisState(
		"diagnosis-001",
		scenario.Alert,
		scenario.Scope,
		model.DiagnosisMode{Type: model.DiagnosisModeGeneralAgent},
		model.DefaultLimits(),
	)
	if err != nil {
		return model.DiagnosisState{}, model.DiagnosisReport{}, err
	}
	ids := idgen.NewSequential()
	registry := tools.NewRegistry()
	orchestrator := diagnosis.NewOrchestrator(
		planner.NewDeterministic(ids),
		tools.NewPolicy(registry),
		tools.NewExecutor(scenario.Machine, ids),
		report.NewBuilder(),
		ids,
		diagnosis.RealClock{},
	)
	return orchestrator.Run(ctx, state)
}
