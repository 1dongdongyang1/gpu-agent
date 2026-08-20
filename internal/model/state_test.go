package model

import "testing"

func validInitialState(t *testing.T) DiagnosisState {
	t.Helper()
	state, err := NewDiagnosisState(
		"diagnosis-001",
		Alert{
			ID:       "alert-001",
			TargetID: "host-01",
			Type:     "gpu_abnormal",
			Severity: AlertSeverityWarning,
			Message:  "GPU resource usage appears abnormal",
		},
		Scope{TargetID: "host-01", GPUAccessMode: GPUAccessAll},
		DiagnosisMode{Type: DiagnosisModeGeneralAgent},
		DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("NewDiagnosisState() failed: %v", err)
	}
	return state
}

func TestNewDiagnosisStateCreatesValidInitializedState(t *testing.T) {
	state := validInitialState(t)
	if state.Status != DiagnosisInitialized {
		t.Fatalf("status = %q, want %q", state.Status, DiagnosisInitialized)
	}
	if state.Termination != nil {
		t.Fatal("initialized state unexpectedly has termination")
	}
	if state.RemainingExecutionBudget() != 4 {
		t.Fatalf("remaining budget = %d, want 4", state.RemainingExecutionBudget())
	}
}

func TestDiagnosisStateValidateRejectsAlertOutsideScope(t *testing.T) {
	state := validInitialState(t)
	state.Alert.GPUID = "GPU-1"
	state.Scope = Scope{
		TargetID:      "host-01",
		GPUAccessMode: GPUAccessSelected,
		AllowedGPUs:   []string{"GPU-0"},
	}
	if err := state.Validate(); err == nil {
		t.Fatal("alert GPU outside scope was accepted")
	}
}

func TestDiagnosisStateValidateEnforcesLifecycleTermination(t *testing.T) {
	state := validInitialState(t)
	state.Termination = &Termination{Reason: StopEvidenceSufficient}
	if err := state.Validate(); err == nil {
		t.Fatal("initialized state with termination was accepted")
	}

	state.Status = DiagnosisReporting
	state.Termination = nil
	if err := state.Validate(); err == nil {
		t.Fatal("reporting state without termination was accepted")
	}

	state.Termination = &Termination{Reason: StopEvidenceSufficient}
	if err := state.Validate(); err != nil {
		t.Fatalf("valid reporting state rejected: %v", err)
	}
}

func TestDiagnosisStateValidateEnforcesHistoryReferences(t *testing.T) {
	state := validInitialState(t)
	state.Status = DiagnosisRunning
	state.ToolCalls = []ToolCall{{
		ID:         "call-001",
		DecisionID: "missing-decision",
		ToolName:   "query_gpu_status",
		TargetID:   "host-01",
		Status:     ToolCallPending,
	}}
	if err := state.Validate(); err == nil {
		t.Fatal("tool call with unknown decision was accepted")
	}
}

func TestDiagnosisStateDerivesCountersFromHistory(t *testing.T) {
	state := validInitialState(t)
	state.Status = DiagnosisRunning
	state.Decisions = []PlannerDecision{
		{
			ID:        "decision-001",
			Type:      DecisionCallTool,
			ToolName:  "query_gpu_status",
			Arguments: ToolArguments{QueryGPUStatus: &QueryGPUStatusArgs{}},
			Reason:    "establish baseline",
		},
		{
			ID:        "decision-002",
			Type:      DecisionCallTool,
			ToolName:  "query_gpu_status",
			Arguments: ToolArguments{QueryGPUStatus: &QueryGPUStatusArgs{}},
			Reason:    "retry after rejection",
		},
	}
	state.ToolCalls = []ToolCall{
		{ID: "call-001", DecisionID: "decision-001", ToolName: "query_gpu_status", TargetID: "host-01", Status: ToolCallRejected, Error: &RuntimeError{Code: "policy_rejected", Message: "rejected"}},
		{ID: "call-002", DecisionID: "decision-002", ToolName: "query_gpu_status", TargetID: "host-01", Status: ToolCallSucceeded},
	}

	if err := state.Validate(); err != nil {
		t.Fatalf("valid history rejected: %v", err)
	}
	if state.PlannerRounds() != 2 || state.ExecutionAttempts() != 1 || state.RejectedCalls() != 1 {
		t.Fatalf("unexpected counters: rounds=%d attempts=%d rejected=%d", state.PlannerRounds(), state.ExecutionAttempts(), state.RejectedCalls())
	}
	if state.RemainingExecutionBudget() != 3 {
		t.Fatalf("remaining budget = %d, want 3", state.RemainingExecutionBudget())
	}
}
