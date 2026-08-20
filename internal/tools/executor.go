package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/1dongdongyang1/gpu-agent/internal/idgen"
	"github.com/1dongdongyang1/gpu-agent/internal/model"
)

const maxStoredRawBytes = 8 * 1024

type MachineReader interface {
	QueryGPUStatus(targetID string) ([]model.GPUStatus, error)
	QueryGPUProcesses(targetID, gpuID string) ([]model.GPUProcess, error)
	QueryDriverStatus(targetID string) (model.DriverStatusData, error)
	QueryXIDEvents(targetID, gpuID string, since time.Time, limit int) ([]model.XIDEvent, error)
	QueryRecentKernelLogs(targetID, gpuID string, since time.Time, limit int) ([]model.KernelLogEntry, error)
}

type Clock interface{ Now() time.Time }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type Executor struct {
	machine MachineReader
	ids     idgen.Generator
	clock   Clock
}

type ExecutorOption func(*Executor)

func WithClock(clock Clock) ExecutorOption { return func(e *Executor) { e.clock = clock } }

func NewExecutor(machine MachineReader, ids idgen.Generator, options ...ExecutorOption) Executor {
	executor := Executor{machine: machine, ids: ids, clock: realClock{}}
	for _, option := range options {
		option(&executor)
	}
	return executor
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
			facts = append(facts, e.fact("gpu", status.GPUID, "availability", model.NewTextValue(string(status.Availability)), ""))
			if status.Availability == model.GPUOnline {
				facts = append(facts,
					e.fact("gpu", status.GPUID, "memory_total_mb", model.NewIntegerValue(status.MemoryTotalMB), "MiB"),
					e.fact("gpu", status.GPUID, "memory_used_mb", model.NewIntegerValue(status.MemoryUsedMB), "MiB"),
					e.fact("gpu", status.GPUID, "utilization_percent", model.NewDecimalValue(status.Utilization), "%"),
					e.fact("gpu", status.GPUID, "temperature_c", model.NewDecimalValue(status.TemperatureC), "C"),
				)
			}
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
	case QueryDriverStatus:
		status, err := e.machine.QueryDriverStatus(call.TargetID)
		if err != nil {
			return e.failedObservation(call.ID, err)
		}
		facts := []model.ObservedFact{
			e.fact("driver", "nvidia", "loaded", model.NewBooleanValue(status.Loaded), ""),
			e.fact("driver", "nvidia", "nvml_available", model.NewBooleanValue(status.NVMLAvailable), ""),
		}
		if status.Loaded {
			facts = append(facts, e.fact("driver", "nvidia", "version", model.NewTextValue(status.Version), ""))
		}
		data := model.ObservationData{Type: model.ObservationDataDriverStatus, DriverStatus: &status}
		return e.successObservation(call.ID, data, facts)
	case QueryXIDEvents:
		args := call.ExecutedArguments.QueryXIDEvents
		if args == nil {
			return e.failedObservation(call.ID, fmt.Errorf("normalized Xid arguments are missing"))
		}
		events, err := e.machine.QueryXIDEvents(call.TargetID, args.GPUID, e.clock.Now().Add(-time.Duration(args.SinceMinutes)*time.Minute), args.Limit)
		if err != nil {
			return e.failedObservation(call.ID, err)
		}
		facts := make([]model.ObservedFact, 0, len(events)*4)
		for _, event := range events {
			facts = append(facts,
				e.fact("xid_event", event.ID, "gpu_id", model.NewTextValue(event.GPUID), ""),
				e.fact("xid_event", event.ID, "code", model.NewIntegerValue(event.Code), ""),
				e.fact("xid_event", event.ID, "occurred_at", model.NewTextValue(event.OccurredAt.Format(time.RFC3339)), ""),
				e.fact("xid_event", event.ID, "summary", model.NewTextValue(event.Summary), ""),
			)
		}
		data := model.ObservationData{Type: model.ObservationDataXIDEvents, XIDEvents: &model.XIDEventsData{GPUID: args.GPUID, SinceMinutes: args.SinceMinutes, Events: events}}
		return e.successObservation(call.ID, data, facts)
	case QueryRecentKernelLogs:
		args := call.ExecutedArguments.QueryRecentKernelLogs
		if args == nil {
			return e.failedObservation(call.ID, fmt.Errorf("normalized kernel log arguments are missing"))
		}
		entries, err := e.machine.QueryRecentKernelLogs(call.TargetID, args.GPUID, e.clock.Now().Add(-time.Duration(args.SinceMinutes)*time.Minute), args.Limit)
		if err != nil {
			return e.failedObservation(call.ID, err)
		}
		facts := make([]model.ObservedFact, 0, len(entries)*6)
		for _, entry := range entries {
			facts = append(facts,
				e.fact("kernel_log", entry.ID, "gpu_id", model.NewTextValue(entry.GPUID), ""),
				e.fact("kernel_log", entry.ID, "occurred_at", model.NewTextValue(entry.OccurredAt.Format(time.RFC3339)), ""),
				e.fact("kernel_log", entry.ID, "severity", model.NewTextValue(string(entry.Severity)), ""),
				e.fact("kernel_log", entry.ID, "component", model.NewTextValue(entry.Component), ""),
				e.fact("kernel_log", entry.ID, "message", model.NewTextValue(entry.Message), ""),
			)
			if entry.RelatedXIDCode != nil {
				facts = append(facts, e.fact("kernel_log", entry.ID, "related_xid_code", model.NewIntegerValue(*entry.RelatedXIDCode), ""))
			}
		}
		data := model.ObservationData{Type: model.ObservationDataKernelLogs, KernelLogs: &model.KernelLogsData{GPUID: args.GPUID, SinceMinutes: args.SinceMinutes, Entries: entries}}
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
