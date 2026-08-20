package diagnosis

import (
	"context"
	"testing"
	"time"

	"github.com/1dongdongyang1/gpu-agent/internal/idgen"
	"github.com/1dongdongyang1/gpu-agent/internal/mock"
	"github.com/1dongdongyang1/gpu-agent/internal/model"
	"github.com/1dongdongyang1/gpu-agent/internal/planner"
	"github.com/1dongdongyang1/gpu-agent/internal/tools"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type testReporter struct{}

func (testReporter) Build(state model.DiagnosisState) (model.DiagnosisReport, error) {
	return model.DiagnosisReport{DiagnosisID: state.DiagnosisID, Outcome: model.OutcomeIssueIdentified, Termination: *state.Termination}, nil
}

func TestOrchestratorRunsExpectedHighMemoryPath(t *testing.T) {
	scenario := mock.HighMemoryScenario()
	state, err := model.NewDiagnosisState("diagnosis-001", scenario.Alert, scenario.Scope, model.DiagnosisMode{Type: model.DiagnosisModeGeneralAgent}, model.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	ids := idgen.NewSequential()
	registry := tools.NewRegistry()
	orchestrator := NewOrchestrator(
		planner.NewDeterministic(ids),
		tools.NewPolicy(registry),
		tools.NewExecutor(scenario.Machine, ids),
		testReporter{},
		ids,
		fixedClock{now: time.Unix(0, 0)},
	)

	finalState, report, err := orchestrator.Run(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if finalState.Status != model.DiagnosisFinished || finalState.Termination.Reason != model.StopEvidenceSufficient {
		t.Fatalf("unexpected final state: status=%s termination=%+v", finalState.Status, finalState.Termination)
	}
	if finalState.PlannerRounds() != 3 || finalState.ExecutionAttempts() != 2 || finalState.RejectedCalls() != 0 {
		t.Fatalf("unexpected counters: rounds=%d attempts=%d rejected=%d", finalState.PlannerRounds(), finalState.ExecutionAttempts(), finalState.RejectedCalls())
	}
	wantTools := []string{tools.QueryGPUStatus, tools.QueryGPUProcesses}
	for i, want := range wantTools {
		if finalState.ToolCalls[i].ToolName != want {
			t.Fatalf("tool call %d = %s, want %s", i, finalState.ToolCalls[i].ToolName, want)
		}
	}
	if finalState.ToolCalls[1].ExecutedArguments.QueryGPUProcesses.GPUID != "GPU-0" {
		t.Fatalf("process query targeted %+v", finalState.ToolCalls[1].ExecutedArguments)
	}
	if report.Outcome != model.OutcomeIssueIdentified {
		t.Fatalf("report outcome = %s", report.Outcome)
	}
}

type alwaysInvalidPlanner struct{}

func (alwaysInvalidPlanner) Decide(model.DiagnosisState) (model.PlannerDecision, error) {
	return model.PlannerDecision{Type: "run_shell"}, nil
}

func TestOrchestratorSafelyStopsInvalidPlannerOutput(t *testing.T) {
	scenario := mock.HighMemoryScenario()
	state, err := model.NewDiagnosisState("diagnosis-001", scenario.Alert, scenario.Scope, model.DiagnosisMode{Type: model.DiagnosisModeGeneralAgent}, model.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	ids := idgen.NewSequential()
	orchestrator := NewOrchestrator(alwaysInvalidPlanner{}, tools.NewPolicy(tools.NewRegistry()), tools.NewExecutor(scenario.Machine, ids), testReporter{}, ids, fixedClock{now: time.Unix(0, 0)})
	finalState, _, err := orchestrator.Run(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if finalState.Termination.Reason != model.StopPlannerInvalidOutput || len(finalState.ToolCalls) != 0 {
		t.Fatalf("invalid planner output was not safely contained: %+v", finalState)
	}
}

type outOfScopePlanner struct{ ids idgen.Generator }

func (p outOfScopePlanner) Decide(model.DiagnosisState) (model.PlannerDecision, error) {
	return model.PlannerDecision{
		ID:        p.ids.Next("decision"),
		Type:      model.DecisionCallTool,
		ToolName:  tools.QueryGPUProcesses,
		Arguments: model.ToolArguments{QueryGPUProcesses: &model.QueryGPUProcessesArgs{GPUID: "GPU-9"}},
		Reason:    "request an out-of-scope GPU for guard testing",
	}, nil
}

func TestLoopGuardStopsRepeatedPolicyRejections(t *testing.T) {
	scenario := mock.HighMemoryScenario()
	scenario.Scope = model.Scope{TargetID: "host-01", GPUAccessMode: model.GPUAccessSelected, AllowedGPUs: []string{"GPU-0"}}
	state, err := model.NewDiagnosisState("diagnosis-001", scenario.Alert, scenario.Scope, model.DiagnosisMode{Type: model.DiagnosisModeGeneralAgent}, model.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	ids := idgen.NewSequential()
	orchestrator := NewOrchestrator(outOfScopePlanner{ids: ids}, tools.NewPolicy(tools.NewRegistry()), tools.NewExecutor(scenario.Machine, ids), testReporter{}, ids, fixedClock{now: time.Unix(0, 0)})
	finalState, _, err := orchestrator.Run(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if finalState.Termination.Reason != model.StopRepeatedPolicyRejection || finalState.RejectedCalls() != 2 || finalState.ExecutionAttempts() != 0 {
		t.Fatalf("unexpected rejection guard result: termination=%s rejected=%d attempts=%d", finalState.Termination.Reason, finalState.RejectedCalls(), finalState.ExecutionAttempts())
	}
	if len(finalState.Observations) != 0 {
		t.Fatalf("policy-rejected calls produced observations: %+v", finalState.Observations)
	}
}

type failingMachine struct{}

func (failingMachine) QueryGPUStatus(string) ([]model.GPUStatus, error) {
	return nil, context.DeadlineExceeded
}

func (failingMachine) QueryGPUProcesses(string, string) ([]model.GPUProcess, error) {
	return nil, context.DeadlineExceeded
}

func TestLoopGuardStopsConsecutiveToolFailures(t *testing.T) {
	scenario := mock.HighMemoryScenario()
	state, err := model.NewDiagnosisState("diagnosis-001", scenario.Alert, scenario.Scope, model.DiagnosisMode{Type: model.DiagnosisModeGeneralAgent}, model.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	ids := idgen.NewSequential()
	orchestrator := NewOrchestrator(planner.NewDeterministic(ids), tools.NewPolicy(tools.NewRegistry()), tools.NewExecutor(failingMachine{}, ids), testReporter{}, ids, fixedClock{now: time.Unix(0, 0)})
	finalState, _, err := orchestrator.Run(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if finalState.Termination.Reason != model.StopConsecutiveFailures || finalState.ExecutionAttempts() != 2 {
		t.Fatalf("unexpected failure guard result: termination=%s attempts=%d", finalState.Termination.Reason, finalState.ExecutionAttempts())
	}
	if len(finalState.Observations) != 2 || finalState.Observations[0].Status != model.ObservationFailed {
		t.Fatalf("tool failures were not recorded as failed observations: %+v", finalState.Observations)
	}
}
