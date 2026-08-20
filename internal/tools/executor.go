package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/1dongdongyang1/gpu-agent/internal/idgen"
	"github.com/1dongdongyang1/gpu-agent/internal/model"
)

const maxStoredRawBytes = 8 * 1024

type MachineReader interface {
	QueryGPUStatus(targetID string) ([]model.GPUStatus, error)
	QueryGPUProcesses(targetID, gpuID string) ([]model.GPUProcess, error)
}

type Executor struct {
	machine MachineReader
	ids     idgen.Generator
}

func NewExecutor(machine MachineReader, ids idgen.Generator) Executor {
	return Executor{machine: machine, ids: ids}
}

func (e Executor) Execute(call model.ToolCall) model.Observation {
	switch call.ToolName {
	case QueryGPUStatus:
		statuses, err := e.machine.QueryGPUStatus(call.TargetID)
		if err != nil {
			return e.failedObservation(call.ID, err)
		}
		facts := make([]model.ObservedFact, 0, len(statuses)*4)
		for _, status := range statuses {
			facts = append(facts,
				e.fact("gpu", status.GPUID, "memory_total_mb", model.NewIntegerValue(status.MemoryTotalMB), "MiB"),
				e.fact("gpu", status.GPUID, "memory_used_mb", model.NewIntegerValue(status.MemoryUsedMB), "MiB"),
				e.fact("gpu", status.GPUID, "utilization_percent", model.NewDecimalValue(status.Utilization), "%"),
				e.fact("gpu", status.GPUID, "temperature_c", model.NewDecimalValue(status.TemperatureC), "C"),
			)
		}
		data := model.ObservationData{Type: model.ObservationDataGPUStatus, GPUStatus: &model.GPUStatusData{GPUs: statuses}}
		return e.successObservation(call.ID, data, facts)
	case QueryGPUProcesses:
		if call.ExecutedArguments.QueryGPUProcesses == nil {
			return e.failedObservation(call.ID, fmt.Errorf("normalized gpu process arguments are missing"))
		}
		processes, err := e.machine.QueryGPUProcesses(call.TargetID, call.ExecutedArguments.QueryGPUProcesses.GPUID)
		if err != nil {
			return e.failedObservation(call.ID, err)
		}
		facts := make([]model.ObservedFact, 0, len(processes)*3)
		for _, process := range processes {
			subjectID := fmt.Sprintf("PID-%d", process.PID)
			facts = append(facts,
				e.fact("process", subjectID, "process_name", model.NewTextValue(process.ProcessName), ""),
				e.fact("process", subjectID, "gpu_id", model.NewTextValue(process.GPUID), ""),
				e.fact("process", subjectID, "memory_used_mb", model.NewIntegerValue(process.MemoryUsedMB), "MiB"),
			)
		}
		data := model.ObservationData{Type: model.ObservationDataGPUProcesses, GPUProcesses: &model.GPUProcessesData{Processes: processes}}
		return e.successObservation(call.ID, data, facts)
	default:
		return e.failedObservation(call.ID, fmt.Errorf("tool %s is not executable", call.ToolName))
	}
}

func (e Executor) fact(subjectType, subjectID, key string, value model.FactValue, unit string) model.ObservedFact {
	return model.ObservedFact{ID: e.ids.Next("fact"), SubjectType: subjectType, SubjectID: subjectID, Key: key, Value: value, Unit: unit}
}

func (e Executor) successObservation(toolCallID string, data model.ObservationData, facts []model.ObservedFact) model.Observation {
	return model.Observation{
		ID:         e.ids.Next("obs"),
		ToolCallID: toolCallID,
		Status:     model.ObservationSucceeded,
		Data:       &data,
		Facts:      facts,
		Raw:        safeRaw(data),
	}
}

func (e Executor) failedObservation(toolCallID string, cause error) model.Observation {
	return model.Observation{
		ID:         e.ids.Next("obs"),
		ToolCallID: toolCallID,
		Status:     model.ObservationFailed,
		Error:      &model.RuntimeError{Code: model.ErrorToolExecutionFailed, Message: cause.Error()},
	}
}

func safeRaw(value any) model.RawResult {
	encoded, _ := json.Marshal(value)
	digestBytes := sha256.Sum256(encoded)
	originalSize := len(encoded)
	truncated := originalSize > maxStoredRawBytes
	if truncated {
		encoded = encoded[:maxStoredRawBytes]
	}
	return model.RawResult{
		Content:           string(encoded),
		Truncated:         truncated,
		OriginalSizeBytes: originalSize,
		Redacted:          true,
		Digest:            hex.EncodeToString(digestBytes[:]),
	}
}
