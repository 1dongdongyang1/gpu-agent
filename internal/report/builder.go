package report

import (
	"fmt"
	"sort"

	"github.com/1dongdongyang1/gpu-agent/internal/model"
)

const highMemoryRatio = 0.90

type Builder struct{}

func NewBuilder() Builder { return Builder{} }

func (Builder) Build(state model.DiagnosisState) (model.DiagnosisReport, error) {
	if state.Status != model.DiagnosisReporting || state.Termination == nil {
		return model.DiagnosisReport{}, fmt.Errorf("report requires a terminated reporting state")
	}
	report := model.DiagnosisReport{
		DiagnosisID: state.DiagnosisID,
		Termination: *state.Termination,
	}

	if state.Termination.Reason != model.StopEvidenceSufficient && state.Termination.Reason != model.StopNoIssueFound {
		report.Outcome = outcomeForTermination(state.Termination.Reason)
		report.Unknowns = []model.Unknown{{
			Text:                  "The current observations are insufficient for a bounded diagnosis.",
			Reason:                state.Termination.Detail,
			RelatedToolCallIDs:    relatedFailedCalls(state),
			RelatedObservationIDs: relatedFailedObservations(state),
		}}
		if err := Validate(state, report); err != nil {
			return model.DiagnosisReport{}, err
		}
		return report, nil
	}

	statusObservation, highGPU := highMemoryObservation(state)
	if statusObservation == nil || highGPU == nil {
		report.Outcome = model.OutcomeNoIssueFound
		if latest := latestSuccessfulStatus(state); latest != nil {
			refs := refsForFacts(*latest, func(f model.ObservedFact) bool { return f.Key == "memory_used_mb" || f.Key == "memory_total_mb" })
			report.ConfirmedFindings = []model.ConfirmedFinding{{Text: "No GPU met the deterministic 90% memory inspection threshold in the observed status snapshot.", EvidenceRefs: refs}}
		}
		if err := Validate(state, report); err != nil {
			return model.DiagnosisReport{}, err
		}
		return report, nil
	}

	processObservation, largestProcess := largestProcessObservation(state, highGPU.GPUID)
	if processObservation == nil || largestProcess == nil {
		return model.DiagnosisReport{}, fmt.Errorf("evidence_sufficient state lacks process evidence for %s", highGPU.GPUID)
	}

	statusRefs := refsForFacts(*statusObservation, func(f model.ObservedFact) bool {
		return f.SubjectID == highGPU.GPUID && (f.Key == "memory_used_mb" || f.Key == "memory_total_mb")
	})
	processSubject := fmt.Sprintf("PID-%d", largestProcess.PID)
	processRefs := refsForFacts(*processObservation, func(f model.ObservedFact) bool { return f.SubjectID == processSubject })
	combinedRefs := append(append([]model.EvidenceRef{}, statusRefs...), processRefs...)

	report.Outcome = model.OutcomeIssueIdentified
	report.ConfirmedFindings = []model.ConfirmedFinding{
		{
			Text:         fmt.Sprintf("%s used %d MiB of %d MiB GPU memory.", highGPU.GPUID, highGPU.MemoryUsedMB, highGPU.MemoryTotalMB),
			EvidenceRefs: statusRefs,
		},
		{
			Text:         fmt.Sprintf("PID-%d (%s) used %d MiB on %s in this observation.", largestProcess.PID, largestProcess.ProcessName, largestProcess.MemoryUsedMB, largestProcess.GPUID),
			EvidenceRefs: processRefs,
		},
	}
	report.Inferences = []model.Inference{
		{
			Text:         fmt.Sprintf("%s had GPU memory pressure under the deterministic 90%% demonstration rule.", highGPU.GPUID),
			Confidence:   model.ConfidenceHigh,
			EvidenceRefs: statusRefs,
		},
		{
			Text:         fmt.Sprintf("PID-%d was the largest direct GPU memory consumer observed on %s.", largestProcess.PID, highGPU.GPUID),
			Confidence:   model.ConfidenceHigh,
			EvidenceRefs: combinedRefs,
		},
	}
	report.Unknowns = []model.Unknown{
		{Text: "Whether the observed memory use matches the tenant workload's intent.", Reason: "The host-level diagnosis does not know tenant workload expectations."},
		{Text: "Whether memory use is continuously growing or caused by a memory leak.", Reason: "Only one point-in-time observation was collected."},
	}
	report.Recommendations = []model.Recommendation{{
		Text:         fmt.Sprintf("Have an operator confirm whether PID-%d belongs to the expected workload before considering any recovery action.", largestProcess.PID),
		Reason:       "The process is the largest observed direct consumer, but this read-only agent cannot determine workload intent or perform state changes.",
		EvidenceRefs: processRefs,
	}}

	if err := Validate(state, report); err != nil {
		return model.DiagnosisReport{}, err
	}
	return report, nil
}

func outcomeForTermination(reason model.StopReason) model.DiagnosisOutcome {
	if reason == model.StopEscalated {
		return model.OutcomeEscalated
	}
	return model.OutcomeInconclusive
}

func highMemoryObservation(state model.DiagnosisState) (*model.Observation, *model.GPUStatus) {
	observation := latestSuccessfulStatus(state)
	if observation == nil {
		return nil, nil
	}
	for i := range observation.Data.GPUStatus.GPUs {
		gpu := &observation.Data.GPUStatus.GPUs[i]
		if gpu.MemoryTotalMB > 0 && float64(gpu.MemoryUsedMB)/float64(gpu.MemoryTotalMB) >= highMemoryRatio {
			return observation, gpu
		}
	}
	return observation, nil
}

func latestSuccessfulStatus(state model.DiagnosisState) *model.Observation {
	for i := len(state.Observations) - 1; i >= 0; i-- {
		observation := &state.Observations[i]
		if observation.Status == model.ObservationSucceeded && observation.Data != nil && observation.Data.GPUStatus != nil {
			return observation
		}
	}
	return nil
}

func largestProcessObservation(state model.DiagnosisState, gpuID string) (*model.Observation, *model.GPUProcess) {
	for i := len(state.Observations) - 1; i >= 0; i-- {
		observation := &state.Observations[i]
		if observation.Status != model.ObservationSucceeded || observation.Data == nil || observation.Data.GPUProcesses == nil {
			continue
		}
		var candidates []model.GPUProcess
		for _, process := range observation.Data.GPUProcesses.Processes {
			if process.GPUID == gpuID {
				candidates = append(candidates, process)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].MemoryUsedMB == candidates[j].MemoryUsedMB {
				return candidates[i].PID < candidates[j].PID
			}
			return candidates[i].MemoryUsedMB > candidates[j].MemoryUsedMB
		})
		return observation, &candidates[0]
	}
	return nil, nil
}

func refsForFacts(observation model.Observation, include func(model.ObservedFact) bool) []model.EvidenceRef {
	refs := make([]model.EvidenceRef, 0)
	for _, fact := range observation.Facts {
		if include(fact) {
			refs = append(refs, model.EvidenceRef{ObservationID: observation.ID, FactID: fact.ID})
		}
	}
	return refs
}

func relatedFailedCalls(state model.DiagnosisState) []string {
	ids := make([]string, 0)
	for _, call := range state.ToolCalls {
		if call.Status == model.ToolCallRejected || call.Status == model.ToolCallFailed || call.Status == model.ToolCallTimeout {
			ids = append(ids, call.ID)
		}
	}
	return ids
}

func relatedFailedObservations(state model.DiagnosisState) []string {
	ids := make([]string, 0)
	for _, observation := range state.Observations {
		if observation.Status != model.ObservationSucceeded {
			ids = append(ids, observation.ID)
		}
	}
	return ids
}
