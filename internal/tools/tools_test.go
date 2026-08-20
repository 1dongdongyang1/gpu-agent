package tools

import (
	"testing"

	"github.com/1dongdongyang1/gpu-agent/internal/idgen"
	"github.com/1dongdongyang1/gpu-agent/internal/mock"
	"github.com/1dongdongyang1/gpu-agent/internal/model"
)

func testState(t *testing.T) model.DiagnosisState {
	t.Helper()
	scenario := mock.HighMemoryScenario()
	state, err := model.NewDiagnosisState("diagnosis-001", scenario.Alert, scenario.Scope, model.DiagnosisMode{Type: model.DiagnosisModeGeneralAgent}, model.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	state.Status = model.DiagnosisRunning
	return state
}

func TestRegistryContainsOnlyReadOnlyMVPTools(t *testing.T) {
	definitions := NewRegistry().Definitions()
	if len(definitions) != 2 {
		t.Fatalf("tool count = %d, want 2", len(definitions))
	}
	for _, definition := range definitions {
		if !definition.ReadOnly {
			t.Fatalf("tool %s is not read-only", definition.Name)
		}
	}
}

func TestPolicyRejectsOutOfScopeGPU(t *testing.T) {
	state := testState(t)
	state.Scope = model.Scope{TargetID: "host-01", GPUAccessMode: model.GPUAccessSelected, AllowedGPUs: []string{"GPU-0"}}
	call := model.ToolCall{
		ID:                 "call-001",
		DecisionID:         "decision-001",
		ToolName:           QueryGPUProcesses,
		TargetID:           "host-01",
		RequestedArguments: model.ToolArguments{QueryGPUProcesses: &model.QueryGPUProcessesArgs{GPUID: "GPU-1"}},
		Status:             model.ToolCallPending,
	}
	_, _, policyErr := NewPolicy(NewRegistry()).Check(state, call)
	if policyErr == nil || policyErr.Code != model.ErrorGPUOutOfScope {
		t.Fatalf("policy error = %+v, want gpu_out_of_scope", policyErr)
	}
}

func TestPolicyRejectsSuccessfulDuplicate(t *testing.T) {
	state := testState(t)
	state.ToolCalls = []model.ToolCall{{
		ID:          "call-001",
		DecisionID:  "decision-001",
		ToolName:    QueryGPUStatus,
		TargetID:    "host-01",
		Fingerprint: "query_gpu_status|host-01|all",
		Status:      model.ToolCallSucceeded,
	}}
	call := model.ToolCall{ID: "call-002", DecisionID: "decision-002", ToolName: QueryGPUStatus, TargetID: "host-01", RequestedArguments: model.ToolArguments{QueryGPUStatus: &model.QueryGPUStatusArgs{}}, Status: model.ToolCallPending}
	_, _, policyErr := NewPolicy(NewRegistry()).Check(state, call)
	if policyErr == nil || policyErr.Code != model.ErrorDuplicateCall {
		t.Fatalf("policy error = %+v, want duplicate_call", policyErr)
	}
}

func TestExecutorBuildsTypedFactsFromUnifiedMachine(t *testing.T) {
	scenario := mock.HighMemoryScenario()
	executor := NewExecutor(scenario.Machine, idgen.NewSequential())
	statusCall := model.ToolCall{ID: "call-001", ToolName: QueryGPUStatus, TargetID: "host-01", ExecutedArguments: model.ToolArguments{QueryGPUStatus: &model.QueryGPUStatusArgs{}}, Status: model.ToolCallExecuting}
	statusObservation := executor.Execute(statusCall)
	if err := statusObservation.Validate(); err != nil {
		t.Fatalf("status observation invalid: %v", err)
	}
	if len(statusObservation.Facts) != 8 || statusObservation.Data.GPUStatus.GPUs[0].MemoryUsedMB != 23500 {
		t.Fatalf("unexpected status observation: %+v", statusObservation)
	}

	processCall := model.ToolCall{ID: "call-002", ToolName: QueryGPUProcesses, TargetID: "host-01", ExecutedArguments: model.ToolArguments{QueryGPUProcesses: &model.QueryGPUProcessesArgs{GPUID: "GPU-0"}}, Status: model.ToolCallExecuting}
	processObservation := executor.Execute(processCall)
	if err := processObservation.Validate(); err != nil {
		t.Fatalf("process observation invalid: %v", err)
	}
	if len(processObservation.Facts) != 6 || processObservation.Data.GPUProcesses.Processes[0].PID != 4321 {
		t.Fatalf("unexpected process observation: %+v", processObservation)
	}
	if processObservation.Raw.Digest == "" || !processObservation.Raw.Redacted {
		t.Fatalf("raw metadata is incomplete: %+v", processObservation.Raw)
	}
}
