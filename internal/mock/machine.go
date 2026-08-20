package mock

import (
	"fmt"
	"sort"
	"time"

	"github.com/1dongdongyang1/gpu-agent/internal/model"
)

type GPU struct {
	Availability  model.GPUAvailability
	ID            string
	MemoryTotalMB int64
	MemoryUsedMB  int64
	Utilization   float64
	TemperatureC  float64
}

type Process struct {
	PID          int
	GPUID        string
	Name         string
	MemoryUsedMB int64
}

// MachineState is the single source of truth read by every mock diagnostic tool.
type MachineState struct {
	TargetID   string
	GPUs       map[string]GPU
	Processes  []Process
	Driver     model.DriverStatusData
	XIDEvents  []model.XIDEvent
	KernelLogs []model.KernelLogEntry
}

func (m MachineState) Validate() error {
	if m.TargetID == "" {
		return fmt.Errorf("target ID is required")
	}
	if len(m.GPUs) == 0 {
		return fmt.Errorf("at least one GPU is required")
	}
	processMemory := make(map[string]int64, len(m.GPUs))
	for id, gpu := range m.GPUs {
		if id == "" || gpu.ID == "" || id != gpu.ID {
			return fmt.Errorf("GPU map key %q does not match GPU ID %q", id, gpu.ID)
		}
		status := model.GPUStatus{Availability: gpu.Availability, GPUID: gpu.ID, MemoryTotalMB: gpu.MemoryTotalMB, MemoryUsedMB: gpu.MemoryUsedMB, Utilization: gpu.Utilization, TemperatureC: gpu.TemperatureC}
		if err := status.Validate(); err != nil {
			return fmt.Errorf("GPU %s: %w", id, err)
		}
	}
	if err := m.Driver.Validate(); err != nil {
		return err
	}
	seenPIDs := make(map[int]struct{}, len(m.Processes))
	for _, process := range m.Processes {
		if process.PID <= 0 || process.Name == "" {
			return fmt.Errorf("process PID and name are required")
		}
		if _, exists := seenPIDs[process.PID]; exists {
			return fmt.Errorf("duplicate process PID %d", process.PID)
		}
		seenPIDs[process.PID] = struct{}{}
		if _, exists := m.GPUs[process.GPUID]; !exists {
			return fmt.Errorf("process %d references unknown GPU %s", process.PID, process.GPUID)
		}
		if process.MemoryUsedMB < 0 {
			return fmt.Errorf("process %d memory must not be negative", process.PID)
		}
		processMemory[process.GPUID] += process.MemoryUsedMB
	}
	for gpuID, total := range processMemory {
		if total > m.GPUs[gpuID].MemoryUsedMB {
			return fmt.Errorf("process memory on %s exceeds GPU used memory", gpuID)
		}
	}
	seenEvents := make(map[string]struct{}, len(m.XIDEvents))
	for _, event := range m.XIDEvents {
		if err := event.Validate(); err != nil {
			return err
		}
		if _, ok := m.GPUs[event.GPUID]; !ok {
			return fmt.Errorf("Xid event %s references unknown GPU %s", event.ID, event.GPUID)
		}
		if _, exists := seenEvents[event.ID]; exists {
			return fmt.Errorf("duplicate Xid event ID %s", event.ID)
		}
		seenEvents[event.ID] = struct{}{}
	}
	seenLogs := make(map[string]struct{}, len(m.KernelLogs))
	for _, entry := range m.KernelLogs {
		if err := entry.Validate(); err != nil {
			return err
		}
		if _, ok := m.GPUs[entry.GPUID]; !ok {
			return fmt.Errorf("kernel log %s references unknown GPU %s", entry.ID, entry.GPUID)
		}
		if _, exists := seenLogs[entry.ID]; exists {
			return fmt.Errorf("duplicate kernel log ID %s", entry.ID)
		}
		seenLogs[entry.ID] = struct{}{}
		if entry.RelatedXIDCode != nil && !m.hasRelatedXID(entry) {
			return fmt.Errorf("kernel log %s has no matching Xid event", entry.ID)
		}
	}
	return nil
}

func (m MachineState) hasRelatedXID(entry model.KernelLogEntry) bool {
	for _, event := range m.XIDEvents {
		delta := entry.OccurredAt.Sub(event.OccurredAt)
		if delta < 0 {
			delta = -delta
		}
		if event.GPUID == entry.GPUID && event.Code == *entry.RelatedXIDCode && delta <= 5*time.Minute {
			return true
		}
	}
	return false
}

func (m MachineState) QueryGPUStatus(targetID string) ([]model.GPUStatus, error) {
	if targetID != m.TargetID {
		return nil, fmt.Errorf("target %s is not this machine", targetID)
	}
	ids := make([]string, 0, len(m.GPUs))
	for id := range m.GPUs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]model.GPUStatus, 0, len(ids))
	for _, id := range ids {
		gpu := m.GPUs[id]
		result = append(result, model.GPUStatus{
			Availability:  gpu.Availability,
			GPUID:         gpu.ID,
			MemoryTotalMB: gpu.MemoryTotalMB,
			MemoryUsedMB:  gpu.MemoryUsedMB,
			Utilization:   gpu.Utilization,
			TemperatureC:  gpu.TemperatureC,
		})
	}
	return result, nil
}

func (m MachineState) QueryGPUProcesses(targetID, gpuID string) ([]model.GPUProcess, error) {
	if targetID != m.TargetID {
		return nil, fmt.Errorf("target %s is not this machine", targetID)
	}
	if _, exists := m.GPUs[gpuID]; !exists {
		return nil, fmt.Errorf("GPU %s is not registered", gpuID)
	}
	if m.GPUs[gpuID].Availability != model.GPUOnline {
		return nil, fmt.Errorf("GPU %s is unavailable", gpuID)
	}
	result := make([]model.GPUProcess, 0)
	for _, process := range m.Processes {
		if process.GPUID == gpuID {
			result = append(result, model.GPUProcess{
				PID:          process.PID,
				GPUID:        process.GPUID,
				ProcessName:  process.Name,
				MemoryUsedMB: process.MemoryUsedMB,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PID < result[j].PID })
	return result, nil
}

func (m MachineState) QueryDriverStatus(targetID string) (model.DriverStatusData, error) {
	if targetID != m.TargetID {
		return model.DriverStatusData{}, fmt.Errorf("target %s is not this machine", targetID)
	}
	return m.Driver, nil
}

func (m MachineState) QueryXIDEvents(targetID, gpuID string, since time.Time, limit int) ([]model.XIDEvent, error) {
	if targetID != m.TargetID {
		return nil, fmt.Errorf("target %s is not this machine", targetID)
	}
	if _, exists := m.GPUs[gpuID]; !exists {
		return nil, fmt.Errorf("GPU %s is not registered", gpuID)
	}
	result := make([]model.XIDEvent, 0)
	for _, event := range m.XIDEvents {
		if event.GPUID == gpuID && !event.OccurredAt.Before(since) {
			result = append(result, event)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].OccurredAt.Equal(result[j].OccurredAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].OccurredAt.After(result[j].OccurredAt)
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m MachineState) QueryRecentKernelLogs(targetID, gpuID string, since time.Time, limit int) ([]model.KernelLogEntry, error) {
	if targetID != m.TargetID {
		return nil, fmt.Errorf("target %s is not this machine", targetID)
	}
	if _, exists := m.GPUs[gpuID]; !exists {
		return nil, fmt.Errorf("GPU %s is not registered", gpuID)
	}
	result := make([]model.KernelLogEntry, 0)
	for _, entry := range m.KernelLogs {
		if entry.GPUID == gpuID && !entry.OccurredAt.Before(since) {
			result = append(result, entry)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].OccurredAt.Equal(result[j].OccurredAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].OccurredAt.After(result[j].OccurredAt)
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
