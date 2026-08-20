package tools

import (
	"testing"
	"time"

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
	if len(definitions) != 5 {
		t.Fatalf("tool count = %d, want 5", len(definitions))
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
	if len(statusObservation.Facts) != 10 || statusObservation.Data.GPUStatus.GPUs[0].MemoryUsedMB != 23500 {
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

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestExecutorBuildsDriverXIDAndKernelFacts(t *testing.T) {
	scenario := mock.XIDScenario()
	ids := idgen.NewSequential()
	executor := NewExecutor(scenario.Machine, ids, WithClock(fixedClock{now: scenario.Now}))

	driver := executor.Execute(model.ToolCall{ID: "call-driver", ToolName: QueryDriverStatus, TargetID: "host-01", ExecutedArguments: model.ToolArguments{QueryDriverStatus: &model.QueryDriverStatusArgs{}}, Status: model.ToolCallExecuting})
	if err := driver.Validate(); err != nil || len(driver.Facts) != 3 || driver.Data.DriverStatus.Version != "550.54.15" {
		t.Fatalf("unexpected driver observation: observation=%+v err=%v", driver, err)
	}

	xid := executor.Execute(model.ToolCall{ID: "call-xid", ToolName: QueryXIDEvents, TargetID: "host-01", ExecutedArguments: model.ToolArguments{QueryXIDEvents: &model.QueryXIDEventsArgs{GPUID: "GPU-0", SinceMinutes: 30, Limit: 20}}, Status: model.ToolCallExecuting})
	if err := xid.Validate(); err != nil || len(xid.Facts) != 4 || xid.Data.XIDEvents.Events[0].Code != 79 {
		t.Fatalf("unexpected Xid observation: observation=%+v err=%v", xid, err)
	}

	logs := executor.Execute(model.ToolCall{ID: "call-logs", ToolName: QueryRecentKernelLogs, TargetID: "host-01", ExecutedArguments: model.ToolArguments{QueryRecentKernelLogs: &model.QueryRecentKernelLogsArgs{GPUID: "GPU-0", SinceMinutes: 30, Limit: 50}}, Status: model.ToolCallExecuting})
	if err := logs.Validate(); err != nil || len(logs.Facts) != 6 || *logs.Data.KernelLogs.Entries[0].RelatedXIDCode != 79 {
		t.Fatalf("unexpected kernel observation: observation=%+v err=%v", logs, err)
	}
}

func TestUnavailableGPUProducesNoLiveMetricFacts(t *testing.T) {
	scenario := mock.XIDScenario()
	executor := NewExecutor(scenario.Machine, idgen.NewSequential(), WithClock(fixedClock{now: scenario.Now}))
	observation := executor.Execute(model.ToolCall{ID: "call-status", ToolName: QueryGPUStatus, TargetID: "host-01", ExecutedArguments: model.ToolArguments{QueryGPUStatus: &model.QueryGPUStatusArgs{}}, Status: model.ToolCallExecuting})
	if err := observation.Validate(); err != nil {
		t.Fatal(err)
	}
	availability := 0
	for _, fact := range observation.Facts {
		if fact.SubjectID != "GPU-0" {
			continue
		}
		if fact.Key == "availability" {
			availability++
			continue
		}
		t.Fatalf("unavailable GPU produced live metric fact: %+v", fact)
	}
	if availability != 1 {
		t.Fatalf("availability fact count = %d, want 1", availability)
	}
}

func TestPolicyAppliesScopeToXIDAndKernelQueries(t *testing.T) {
	state := testState(t)
	state.Scope = model.Scope{TargetID: "host-01", GPUAccessMode: model.GPUAccessSelected, AllowedGPUs: []string{"GPU-0"}}
	queries := []struct {
		name string
		args model.ToolArguments
	}{
		{QueryXIDEvents, model.ToolArguments{QueryXIDEvents: &model.QueryXIDEventsArgs{GPUID: "GPU-1", SinceMinutes: 30, Limit: 20}}},
		{QueryRecentKernelLogs, model.ToolArguments{QueryRecentKernelLogs: &model.QueryRecentKernelLogsArgs{GPUID: "GPU-1", SinceMinutes: 30, Limit: 50}}},
	}
	for _, query := range queries {
		call := model.ToolCall{ID: "call-scope", DecisionID: "decision-scope", ToolName: query.name, TargetID: "host-01", RequestedArguments: query.args, Status: model.ToolCallPending}
		_, _, policyErr := NewPolicy(NewRegistry()).Check(state, call)
		if policyErr == nil || policyErr.Code != model.ErrorGPUOutOfScope {
			t.Fatalf("%s policy error = %+v, want gpu_out_of_scope", query.name, policyErr)
		}
	}
}

func TestRegistryRejectsOutOfRangeEventQueries(t *testing.T) {
	registry := NewRegistry()
	invalid := []struct {
		name string
		args model.ToolArguments
	}{
		{QueryXIDEvents, model.ToolArguments{QueryXIDEvents: &model.QueryXIDEventsArgs{GPUID: "GPU-0", SinceMinutes: 0, Limit: 20}}},
		{QueryXIDEvents, model.ToolArguments{QueryXIDEvents: &model.QueryXIDEventsArgs{GPUID: "GPU-0", SinceMinutes: 30, Limit: 101}}},
		{QueryRecentKernelLogs, model.ToolArguments{QueryRecentKernelLogs: &model.QueryRecentKernelLogsArgs{GPUID: "GPU-0", SinceMinutes: 1441, Limit: 50}}},
		{QueryRecentKernelLogs, model.ToolArguments{QueryRecentKernelLogs: &model.QueryRecentKernelLogsArgs{GPUID: "GPU-0", SinceMinutes: 30, Limit: 201}}},
	}
	for _, test := range invalid {
		if _, err := registry.Normalize(test.name, test.args); err == nil {
			t.Fatalf("Normalize(%s) accepted invalid arguments: %+v", test.name, test.args)
		}
	}
}
