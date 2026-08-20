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
	unavailableGPU := firstUnavailableGPU(statusObservation)
	if unavailableGPU != "" {
		return p.decideUnavailableGPU(state, *statusObservation, unavailableGPU)
	}
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

func (p Deterministic) decideUnavailableGPU(state model.DiagnosisState, statusObservation model.Observation, gpuID string) (model.PlannerDecision, error) {
	driverObservation := latestObservation(state, model.ObservationDataDriverStatus)
	if driverObservation == nil {
		return model.PlannerDecision{
			ID:        p.ids.Next("decision"),
			Type:      model.DecisionCallTool,
			ToolName:  tools.QueryDriverStatus,
			Arguments: model.ToolArguments{QueryDriverStatus: &model.QueryDriverStatusArgs{}},
			Reason:    fmt.Sprintf("%s is unavailable; check whether the NVIDIA driver and NVML are globally available", gpuID),
		}, nil
	}
	driver := driverObservation.Data.DriverStatus
	statusRefs := refsForSubjectKeys(statusObservation, gpuID, "availability")
	driverRefs := refsForSubjectKeys(*driverObservation, "nvidia", "loaded", "version", "nvml_available")
	if !driver.Loaded || !driver.NVMLAvailable {
		return model.PlannerDecision{
			ID:           p.ids.Next("decision"),
			Type:         model.DecisionEscalate,
			Reason:       "driver or NVML is unavailable, so per-GPU Xid inspection cannot be treated as reliable",
			EvidenceRefs: append(statusRefs, driverRefs...),
			Unknowns:     []string{"GPU availability after driver recovery", "whether GPU-specific Xid events can be queried"},
		}, nil
	}

	xidObservation := latestGPUObservation(state, model.ObservationDataXIDEvents, gpuID)
	if xidObservation == nil {
		return model.PlannerDecision{
			ID:       p.ids.Next("decision"),
			Type:     model.DecisionCallTool,
			ToolName: tools.QueryXIDEvents,
			Arguments: model.ToolArguments{QueryXIDEvents: &model.QueryXIDEventsArgs{
				GPUID: gpuID, SinceMinutes: 30, Limit: 20,
			}},
			Reason: fmt.Sprintf("the driver is available; inspect recent Xid events for unavailable %s", gpuID),
		}, nil
	}

	kernelObservation := latestGPUObservation(state, model.ObservationDataKernelLogs, gpuID)
	if kernelObservation == nil {
		return model.PlannerDecision{
			ID:       p.ids.Next("decision"),
			Type:     model.DecisionCallTool,
			ToolName: tools.QueryRecentKernelLogs,
			Arguments: model.ToolArguments{QueryRecentKernelLogs: &model.QueryRecentKernelLogsArgs{
				GPUID: gpuID, SinceMinutes: 30, Limit: 50,
			}},
			Reason: fmt.Sprintf("correlate %s availability and Xid results with bounded recent kernel logs", gpuID),
		}, nil
	}

	refs := append(statusRefs, driverRefs...)
	refs = append(refs, refsForAllFacts(*xidObservation)...)
	refs = append(refs, refsForAllFacts(*kernelObservation)...)
	if hasMatchingXIDLog(*xidObservation, *kernelObservation) {
		return model.PlannerDecision{
			ID:           p.ids.Next("decision"),
			Type:         model.DecisionFinish,
			Reason:       "GPU unavailability, Xid event, and matching kernel evidence are sufficient for a bounded finding",
			EvidenceRefs: refs,
		}, nil
	}
	return model.PlannerDecision{
		ID:           p.ids.Next("decision"),
		Type:         model.DecisionEscalate,
		Reason:       "the bounded Xid and kernel queries did not provide mutually supporting evidence",
		EvidenceRefs: refs,
		Unknowns:     []string{"the cause of the unavailable GPU", "whether relevant events occurred outside the bounded time window"},
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
		if gpu.Availability == model.GPUOnline && gpu.MemoryTotalMB > 0 && float64(gpu.MemoryUsedMB)/float64(gpu.MemoryTotalMB) >= highMemoryRatio {
			return gpu.GPUID
		}
	}
	return ""
}

func firstUnavailableGPU(observation *model.Observation) string {
	if observation == nil || observation.Data == nil || observation.Data.GPUStatus == nil {
		return ""
	}
	for _, gpu := range observation.Data.GPUStatus.GPUs {
		if gpu.Availability == model.GPUUnavailable {
			return gpu.GPUID
		}
	}
	return ""
}

func latestGPUObservation(state model.DiagnosisState, dataType model.ObservationDataType, gpuID string) *model.Observation {
	for i := len(state.Observations) - 1; i >= 0; i-- {
		observation := &state.Observations[i]
		if observation.Status != model.ObservationSucceeded || observation.Data == nil || observation.Data.Type != dataType {
			continue
		}
		if dataType == model.ObservationDataXIDEvents && observation.Data.XIDEvents.GPUID == gpuID {
			return observation
		}
		if dataType == model.ObservationDataKernelLogs && observation.Data.KernelLogs.GPUID == gpuID {
			return observation
		}
	}
	return nil
}

func refsForSubjectKeys(observation model.Observation, subjectID string, keys ...string) []model.EvidenceRef {
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		wanted[key] = struct{}{}
	}
	refs := make([]model.EvidenceRef, 0)
	for _, fact := range observation.Facts {
		if fact.SubjectID != subjectID {
			continue
		}
		if _, ok := wanted[fact.Key]; ok {
			refs = append(refs, model.EvidenceRef{ObservationID: observation.ID, FactID: fact.ID})
		}
	}
	return refs
}

func refsForAllFacts(observation model.Observation) []model.EvidenceRef {
	refs := make([]model.EvidenceRef, 0, len(observation.Facts))
	for _, fact := range observation.Facts {
		refs = append(refs, model.EvidenceRef{ObservationID: observation.ID, FactID: fact.ID})
	}
	return refs
}

func hasMatchingXIDLog(xidObservation, kernelObservation model.Observation) bool {
	if xidObservation.Data == nil || xidObservation.Data.XIDEvents == nil || kernelObservation.Data == nil || kernelObservation.Data.KernelLogs == nil {
		return false
	}
	for _, event := range xidObservation.Data.XIDEvents.Events {
		for _, entry := range kernelObservation.Data.KernelLogs.Entries {
			if entry.GPUID == event.GPUID && entry.RelatedXIDCode != nil && *entry.RelatedXIDCode == event.Code {
				return true
			}
		}
	}
	return false
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
