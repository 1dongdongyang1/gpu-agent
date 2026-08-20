package planner

import (
	"fmt"

	"github.com/1dongdongyang1/gpu-agent/internal/idgen"
	"github.com/1dongdongyang1/gpu-agent/internal/model"
	"github.com/1dongdongyang1/gpu-agent/internal/tools"
)

const highMemoryRatio = 0.90

type Deterministic struct {
	ids idgen.Generator
}

func NewDeterministic(ids idgen.Generator) Deterministic { return Deterministic{ids: ids} }

func (p Deterministic) Decide(state model.DiagnosisState) (model.PlannerDecision, error) {
	statusObservation := latestObservation(state, model.ObservationDataGPUStatus)
	if statusObservation == nil {
		return model.PlannerDecision{
			ID:        p.ids.Next("decision"),
			Type:      model.DecisionCallTool,
			ToolName:  tools.QueryGPUStatus,
			Arguments: model.ToolArguments{QueryGPUStatus: &model.QueryGPUStatusArgs{}},
			Reason:    "collect the minimum GPU status baseline before drawing a conclusion",
		}, nil
	}

	highGPU := firstHighMemoryGPU(statusObservation)
	if highGPU == "" {
		return model.PlannerDecision{
			ID:           p.ids.Next("decision"),
			Type:         model.DecisionFinish,
			Reason:       "the deterministic high-memory rule did not identify a GPU requiring process inspection",
			EvidenceRefs: refsForGPUKeys(*statusObservation, "", "memory_total_mb", "memory_used_mb"),
		}, nil
	}

	processObservation := latestProcessObservation(state, highGPU)
	if processObservation == nil {
		return model.PlannerDecision{
			ID:        p.ids.Next("decision"),
			Type:      model.DecisionCallTool,
			ToolName:  tools.QueryGPUProcesses,
			Arguments: model.ToolArguments{QueryGPUProcesses: &model.QueryGPUProcessesArgs{GPUID: highGPU}},
			Reason:    fmt.Sprintf("%s memory usage is at least %.0f%%; identify direct process consumers", highGPU, highMemoryRatio*100),
		}, nil
	}

	refs := refsForGPUKeys(*statusObservation, highGPU, "memory_total_mb", "memory_used_mb")
	refs = append(refs, refsForLargestProcess(*processObservation)...)
	return model.PlannerDecision{
		ID:           p.ids.Next("decision"),
		Type:         model.DecisionFinish,
		Reason:       "high GPU memory usage and the largest direct process consumer have been observed",
		EvidenceRefs: refs,
	}, nil
}

func latestObservation(state model.DiagnosisState, dataType model.ObservationDataType) *model.Observation {
	for i := len(state.Observations) - 1; i >= 0; i-- {
		observation := &state.Observations[i]
		if observation.Status == model.ObservationSucceeded && observation.Data != nil && observation.Data.Type == dataType {
			return observation
		}
	}
	return nil
}

func latestProcessObservation(state model.DiagnosisState, gpuID string) *model.Observation {
	for i := len(state.Observations) - 1; i >= 0; i-- {
		observation := &state.Observations[i]
		if observation.Status != model.ObservationSucceeded || observation.Data == nil || observation.Data.GPUProcesses == nil {
			continue
		}
		for _, process := range observation.Data.GPUProcesses.Processes {
			if process.GPUID == gpuID {
				return observation
			}
		}
	}
	return nil
}

func firstHighMemoryGPU(observation *model.Observation) string {
	if observation == nil || observation.Data == nil || observation.Data.GPUStatus == nil {
		return ""
	}
	for _, gpu := range observation.Data.GPUStatus.GPUs {
		if gpu.MemoryTotalMB > 0 && float64(gpu.MemoryUsedMB)/float64(gpu.MemoryTotalMB) >= highMemoryRatio {
			return gpu.GPUID
		}
	}
	return ""
}

func refsForGPUKeys(observation model.Observation, gpuID string, keys ...string) []model.EvidenceRef {
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		wanted[key] = struct{}{}
	}
	refs := make([]model.EvidenceRef, 0)
	for _, fact := range observation.Facts {
		if fact.SubjectType != "gpu" || (gpuID != "" && fact.SubjectID != gpuID) {
			continue
		}
		if _, ok := wanted[fact.Key]; ok {
			refs = append(refs, model.EvidenceRef{ObservationID: observation.ID, FactID: fact.ID})
		}
	}
	return refs
}

func refsForLargestProcess(observation model.Observation) []model.EvidenceRef {
	if observation.Data == nil || observation.Data.GPUProcesses == nil || len(observation.Data.GPUProcesses.Processes) == 0 {
		return nil
	}
	largest := observation.Data.GPUProcesses.Processes[0]
	for _, process := range observation.Data.GPUProcesses.Processes[1:] {
		if process.MemoryUsedMB > largest.MemoryUsedMB {
			largest = process
		}
	}
	subjectID := fmt.Sprintf("PID-%d", largest.PID)
	refs := make([]model.EvidenceRef, 0, 3)
	for _, fact := range observation.Facts {
		if fact.SubjectID == subjectID {
			refs = append(refs, model.EvidenceRef{ObservationID: observation.ID, FactID: fact.ID})
		}
	}
	return refs
}
