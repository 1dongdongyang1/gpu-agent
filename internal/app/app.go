package app

import (
	"context"
	"fmt"
	"time"

	"github.com/1dongdongyang1/gpu-agent/internal/diagnosis"
	"github.com/1dongdongyang1/gpu-agent/internal/idgen"
	"github.com/1dongdongyang1/gpu-agent/internal/mock"
	"github.com/1dongdongyang1/gpu-agent/internal/model"
	"github.com/1dongdongyang1/gpu-agent/internal/planner"
	"github.com/1dongdongyang1/gpu-agent/internal/report"
	"github.com/1dongdongyang1/gpu-agent/internal/tools"
)

const (
	HighMemoryScenario = "high-memory"
	XIDDropScenario    = "xid-drop"
)

type scenarioClock struct{ now time.Time }

func (c scenarioClock) Now() time.Time { return c.now }

func Run(ctx context.Context, scenarioName string) (model.DiagnosisState, model.DiagnosisReport, error) {
	var scenario mock.Scenario
	switch scenarioName {
	case HighMemoryScenario:
		scenario = mock.HighMemoryScenario()
	case XIDDropScenario:
		scenario = mock.XIDScenario()
	default:
		return model.DiagnosisState{}, model.DiagnosisReport{}, fmt.Errorf("unsupported scenario %q", scenarioName)
	}
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
	clock := scenarioClock{now: scenario.Now}
	orchestrator := diagnosis.NewOrchestrator(
		planner.NewDeterministic(ids),
		tools.NewPolicy(registry),
		tools.NewExecutor(scenario.Machine, ids, tools.WithClock(clock)),
		report.NewBuilder(),
		ids,
		clock,
	)
	return orchestrator.Run(ctx, state)
}
