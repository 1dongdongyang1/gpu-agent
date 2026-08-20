package mock

import "github.com/1dongdongyang1/gpu-agent/internal/model"

type Scenario struct {
	Name    string
	Alert   model.Alert
	Scope   model.Scope
	Machine MachineState
}

func HighMemoryScenario() Scenario {
	return Scenario{
		Name: "high-memory",
		Alert: model.Alert{
			ID:       "alert-001",
			TargetID: "host-01",
			Type:     "gpu_abnormal",
			Severity: model.AlertSeverityWarning,
			Message:  "GPU resource usage appears abnormal",
		},
		Scope: model.Scope{TargetID: "host-01", GPUAccessMode: model.GPUAccessAll},
		Machine: MachineState{
			TargetID: "host-01",
			GPUs: map[string]GPU{
				"GPU-0": {ID: "GPU-0", MemoryTotalMB: 24576, MemoryUsedMB: 23500, Utilization: 92, TemperatureC: 72},
				"GPU-1": {ID: "GPU-1", MemoryTotalMB: 24576, MemoryUsedMB: 1200, Utilization: 15, TemperatureC: 46},
			},
			Processes: []Process{
				{PID: 4321, GPUID: "GPU-0", Name: "python", MemoryUsedMB: 22000},
				{PID: 5678, GPUID: "GPU-0", Name: "monitor", MemoryUsedMB: 300},
			},
		},
	}
}

func (s Scenario) Validate() error {
	if err := s.Alert.Validate(); err != nil {
		return err
	}
	if err := s.Scope.Validate(); err != nil {
		return err
	}
	if s.Alert.TargetID != s.Machine.TargetID || s.Scope.TargetID != s.Machine.TargetID {
		return invalidScenario("alert, scope, and machine targets must match")
	}
	return s.Machine.Validate()
}

type scenarioError string

func (e scenarioError) Error() string { return string(e) }

func invalidScenario(message string) error { return scenarioError(message) }
