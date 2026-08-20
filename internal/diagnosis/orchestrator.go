package diagnosis

import (
	"context"
	"fmt"
	"time"

	"github.com/1dongdongyang1/gpu-agent/internal/idgen"
	"github.com/1dongdongyang1/gpu-agent/internal/model"
	"github.com/1dongdongyang1/gpu-agent/internal/tools"
)

type Planner interface {
	Decide(state model.DiagnosisState) (model.PlannerDecision, error)
}

type Reporter interface {
	Build(state model.DiagnosisState) (model.DiagnosisReport, error)
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

type Orchestrator struct {
	planner  Planner
	policy   tools.Policy
	executor tools.Executor
	reporter Reporter
	ids      idgen.Generator
	clock    Clock
}

func NewOrchestrator(planner Planner, policy tools.Policy, executor tools.Executor, reporter Reporter, ids idgen.Generator, clock Clock) Orchestrator {
	return Orchestrator{planner: planner, policy: policy, executor: executor, reporter: reporter, ids: ids, clock: clock}
}

func (o Orchestrator) Run(ctx context.Context, state model.DiagnosisState) (model.DiagnosisState, model.DiagnosisReport, error) {
	if state.Status != model.DiagnosisInitialized {
		return state, model.DiagnosisReport{}, fmt.Errorf("diagnosis must start in initialized status")
	}
	if err := state.Validate(); err != nil {
		return state, model.DiagnosisReport{}, fmt.Errorf("invalid initial state: %w", err)
	}
	state.Status = model.DiagnosisRunning
	startedAt := o.clock.Now()

	for state.Status == model.DiagnosisRunning {
		if reason := forcedStop(ctx, state, startedAt, o.clock.Now()); reason != "" {
			stop(&state, reason, "an external loop guard stopped the investigation")
			break
		}
		decision, err := o.planner.Decide(state)
		if err != nil || decision.Validate() != nil {
			stop(&state, model.StopPlannerInvalidOutput, "planner returned an invalid decision")
			break
		}
		state.Decisions = append(state.Decisions, decision)

		switch decision.Type {
		case model.DecisionCallTool:
			o.executeDecision(&state, decision)
		case model.DecisionFinish:
			if !evidenceExists(state, decision.EvidenceRefs) {
				stop(&state, model.StopPlannerInvalidOutput, "finish decision referenced unavailable evidence")
			} else {
				stop(&state, model.StopEvidenceSufficient, decision.Reason)
			}
		case model.DecisionEscalate:
			stop(&state, model.StopEscalated, decision.Reason)
		}
	}

	if err := state.Validate(); err != nil {
		return state, model.DiagnosisReport{}, fmt.Errorf("invalid reporting state: %w", err)
	}
	report, err := o.reporter.Build(state)
	if err != nil {
		return state, model.DiagnosisReport{}, fmt.Errorf("build report: %w", err)
	}
	state.Status = model.DiagnosisFinished
	if err := state.Validate(); err != nil {
		return state, model.DiagnosisReport{}, fmt.Errorf("invalid final state: %w", err)
	}
	return state, report, nil
}

func (o Orchestrator) executeDecision(state *model.DiagnosisState, decision model.PlannerDecision) {
	call := model.ToolCall{
		ID:                 o.ids.Next("call"),
		DecisionID:         decision.ID,
		ToolName:           decision.ToolName,
		TargetID:           state.Scope.TargetID,
		RequestedArguments: decision.Arguments,
		Status:             model.ToolCallPending,
	}
	normalized, fingerprint, policyErr := o.policy.Check(*state, call)
	if policyErr != nil {
		call.Status = model.ToolCallRejected
		call.Error = policyErr
		state.ToolCalls = append(state.ToolCalls, call)
		return
	}
	call.ExecutedArguments = normalized
	call.Fingerprint = fingerprint
	call.Status = model.ToolCallExecuting
	observation := o.executor.Execute(call)
	if observation.Status == model.ObservationFailed {
		call.Status = model.ToolCallFailed
		call.Error = observation.Error
	} else {
		call.Status = model.ToolCallSucceeded
	}
	state.ToolCalls = append(state.ToolCalls, call)
	state.Observations = append(state.Observations, observation)
}

func stop(state *model.DiagnosisState, reason model.StopReason, detail string) {
	state.Termination = &model.Termination{Reason: reason, Detail: detail}
	state.Status = model.DiagnosisReporting
}

func evidenceExists(state model.DiagnosisState, refs []model.EvidenceRef) bool {
	for _, ref := range refs {
		found := false
		for _, observation := range state.Observations {
			if observation.ID != ref.ObservationID {
				continue
			}
			for _, fact := range observation.Facts {
				if fact.ID == ref.FactID {
					found = true
					break
				}
			}
		}
		if !found {
			return false
		}
	}
	return len(refs) > 0
}

func forcedStop(ctx context.Context, state model.DiagnosisState, startedAt, now time.Time) model.StopReason {
	if ctx.Err() != nil || now.Sub(startedAt) >= state.Limits.MaxDuration {
		return model.StopTimeout
	}
	if state.PlannerRounds() >= state.Limits.MaxPlannerRounds {
		return model.StopPlannerRoundsExhausted
	}
	if state.ExecutionAttempts() >= state.Limits.MaxExecutionAttempts {
		return model.StopExecutionBudgetExhausted
	}
	if consecutiveStatus(state.ToolCalls, model.ToolCallRejected) >= state.Limits.MaxConsecutiveRejectedCalls {
		return model.StopRepeatedPolicyRejection
	}
	if consecutiveFailures(state.ToolCalls) >= state.Limits.MaxConsecutiveFailures {
		return model.StopConsecutiveFailures
	}
	return ""
}

func consecutiveStatus(calls []model.ToolCall, status model.ToolCallStatus) int {
	count := 0
	for i := len(calls) - 1; i >= 0 && calls[i].Status == status; i-- {
		count++
	}
	return count
}

func consecutiveFailures(calls []model.ToolCall) int {
	count := 0
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Status != model.ToolCallFailed && calls[i].Status != model.ToolCallTimeout {
			break
		}
		count++
	}
	return count
}
