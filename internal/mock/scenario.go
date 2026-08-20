package mock

import (
	"time"

	"github.com/1dongdongyang1/gpu-agent/internal/model"
)

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
				"GPU-0": {ID: "GPU-0", Availability: model.GPUOnline, MemoryTotalMB: 24576, MemoryUsedMB: 23500, Utilization: 92, TemperatureC: 72},
				"GPU-1": {ID: "GPU-1", Availability: model.GPUOnline, MemoryTotalMB: 24576, MemoryUsedMB: 1200, Utilization: 15, TemperatureC: 46},
			},
			Driver: model.DriverStatusData{Loaded: true, Version: "550.54.15", NVMLAvailable: true},
			Processes: []Process{
				{PID: 4321, GPUID: "GPU-0", Name: "python", MemoryUsedMB: 22000},
				{PID: 5678, GPUID: "GPU-0", Name: "monitor", MemoryUsedMB: 300},
			},
		},
	}
}

func XIDScenario() Scenario {
	xidCode := int64(79)
	return Scenario{
		Name: "xid-drop",
		Alert: model.Alert{
			ID:       "alert-xid-001",
			TargetID: "host-01",
			Type:     "gpu_abnormal",
			Severity: model.AlertSeverityCritical,
			Message:  "One registered GPU is unavailable",
		},
		Scope: model.Scope{TargetID: "host-01", GPUAccessMode: model.GPUAccessAll},
		Machine: MachineState{
			TargetID: "host-01",
			GPUs: map[string]GPU{
				"GPU-0": {ID: "GPU-0", Availability: model.GPUUnavailable},
				"GPU-1": {ID: "GPU-1", Availability: model.GPUOnline, MemoryTotalMB: 24576, MemoryUsedMB: 1200, Utilization: 15, TemperatureC: 46},
			},
			Driver: model.DriverStatusData{Loaded: true, Version: "550.54.15", NVMLAvailable: true},
			XIDEvents: []model.XIDEvent{{
				ID: "xid-event-001", GPUID: "GPU-0", Code: xidCode,
				OccurredAt: time.Date(2026, 8, 20, 9, 58, 0, 0, time.FixedZone("CST", 8*60*60)),
				Summary:    "GPU has fallen off the bus",
			}},
			KernelLogs: []model.KernelLogEntry{{
				ID: "kernel-log-001", GPUID: "GPU-0",
				OccurredAt: time.Date(2026, 8, 20, 9, 58, 1, 0, time.FixedZone("CST", 8*60*60)),
				Severity:   model.KernelLogError, Component: "NVRM",
				Message: "NVRM reported Xid 79 for GPU-0", RelatedXIDCode: &xidCode,
			}},
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
