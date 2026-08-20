package model

import "testing"

func TestScopeValidateRejectsAmbiguousGPUAccess(t *testing.T) {
	tests := []Scope{
		{TargetID: "host-01", GPUAccessMode: GPUAccessAll, AllowedGPUs: []string{"GPU-0"}},
		{TargetID: "host-01", GPUAccessMode: GPUAccessSelected},
	}
	for _, scope := range tests {
		if err := scope.Validate(); err == nil {
			t.Fatalf("Validate() accepted invalid scope: %+v", scope)
		}
	}
}

func TestFactValueValidateRequiresExactlyOneMatchingValue(t *testing.T) {
	integer := int64(23500)
	text := "23500"
	tests := []FactValue{
		{},
		{Kind: FactValueInteger, Integer: &integer, Text: &text},
		{Kind: FactValueText, Integer: &integer},
	}
	for _, value := range tests {
		if err := value.Validate(); err == nil {
			t.Fatalf("Validate() accepted invalid value: %+v", value)
		}
	}
	if err := NewIntegerValue(23500).Validate(); err != nil {
		t.Fatalf("valid integer rejected: %v", err)
	}
}

func TestPlannerDecisionValidateEnforcesShape(t *testing.T) {
	valid := PlannerDecision{
		ID:       "decision-001",
		Type:     DecisionCallTool,
		ToolName: "query_gpu_status",
		Arguments: ToolArguments{
			QueryGPUStatus: &QueryGPUStatusArgs{},
		},
		Reason: "establish a GPU baseline",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid call_tool decision rejected: %v", err)
	}

	invalidFinish := PlannerDecision{ID: "decision-002", Type: DecisionFinish, Reason: "done"}
	if err := invalidFinish.Validate(); err == nil {
		t.Fatal("finish without evidence was accepted")
	}

	invalidEscalate := PlannerDecision{
		ID:       "decision-003",
		Type:     DecisionEscalate,
		ToolName: "query_gpu_status",
		Reason:   "cannot continue",
		Unknowns: []string{"GPU state"},
	}
	if err := invalidEscalate.Validate(); err == nil {
		t.Fatal("escalate with tool fields was accepted")
	}

	unknownTool := valid
	unknownTool.ToolName = "run_shell"
	if err := unknownTool.Validate(); err == nil {
		t.Fatal("unregistered tool was accepted")
	}
}

func TestToolCallValidateEnforcesStatusErrorCombination(t *testing.T) {
	base := ToolCall{ID: "call-001", DecisionID: "decision-001", ToolName: "query_gpu_status", TargetID: "host-01"}

	base.Status = ToolCallSucceeded
	base.Error = &RuntimeError{Code: "unexpected", Message: "must not be present"}
	if err := base.Validate(); err == nil {
		t.Fatal("succeeded tool call with error was accepted")
	}

	base.Status = ToolCallRejected
	base.Error = nil
	if err := base.Validate(); err == nil {
		t.Fatal("rejected tool call without error was accepted")
	}
}

func TestObservationValidateEnforcesTypedDataAndError(t *testing.T) {
	observation := Observation{
		ID:         "obs-001",
		ToolCallID: "call-001",
		Status:     ObservationSucceeded,
		Data: &ObservationData{
			Type:         ObservationDataGPUStatus,
			GPUProcesses: &GPUProcessesData{},
		},
	}
	if err := observation.Validate(); err == nil {
		t.Fatal("observation with mismatched data type was accepted")
	}

	observation.Data = &ObservationData{Type: "unknown", GPUStatus: &GPUStatusData{}}
	if err := observation.Validate(); err == nil {
		t.Fatal("observation with unknown data type was accepted")
	}

	observation.Status = ObservationFailed
	observation.Data = nil
	observation.Error = &RuntimeError{Code: "parse_failed", Message: "could not parse result"}
	if err := observation.Validate(); err != nil {
		t.Fatalf("valid failed observation rejected: %v", err)
	}
}

func TestGPUStatusRejectsMetricsForUnavailableGPU(t *testing.T) {
	status := GPUStatus{GPUID: "GPU-0", Availability: GPUUnavailable, MemoryTotalMB: 24576}
	if err := status.Validate(); err == nil {
		t.Fatal("unavailable GPU with live metrics was accepted")
	}
	status.MemoryTotalMB = 0
	if err := status.Validate(); err != nil {
		t.Fatalf("valid unavailable GPU rejected: %v", err)
	}
}

func TestDriverStatusEnforcesLoadedState(t *testing.T) {
	for _, status := range []DriverStatusData{
		{Loaded: false, Version: "550.54.15"},
		{Loaded: false, NVMLAvailable: true},
		{Loaded: true},
	} {
		if err := status.Validate(); err == nil {
			t.Fatalf("invalid driver status accepted: %+v", status)
		}
	}
	if err := (DriverStatusData{Loaded: true, Version: "550.54.15", NVMLAvailable: true}).Validate(); err != nil {
		t.Fatalf("valid driver status rejected: %v", err)
	}
}
