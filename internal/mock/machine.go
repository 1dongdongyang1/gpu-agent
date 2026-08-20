package mock

import (
	"fmt"
	"sort"

	"github.com/1dongdongyang1/gpu-agent/internal/model"
)

type GPU struct {
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
	TargetID  string
	GPUs      map[string]GPU
	Processes []Process
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
		if gpu.MemoryTotalMB <= 0 {
			return fmt.Errorf("GPU %s total memory must be positive", id)
		}
		if gpu.MemoryUsedMB < 0 || gpu.MemoryUsedMB > gpu.MemoryTotalMB {
			return fmt.Errorf("GPU %s used memory is outside [0,total]", id)
		}
		if gpu.Utilization < 0 || gpu.Utilization > 100 {
			return fmt.Errorf("GPU %s utilization is outside [0,100]", id)
		}
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
	return nil
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
