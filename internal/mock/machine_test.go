package mock

import (
	"testing"
	"time"

	"github.com/1dongdongyang1/gpu-agent/internal/model"
)

func TestHighMemoryScenarioIsConsistent(t *testing.T) {
	scenario := HighMemoryScenario()
	if err := scenario.Validate(); err != nil {
		t.Fatalf("HighMemoryScenario() is invalid: %v", err)
	}

	statuses, err := scenario.Machine.QueryGPUStatus("host-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0].GPUID != "GPU-0" || statuses[1].GPUID != "GPU-1" {
		t.Fatalf("unexpected stable GPU order: %+v", statuses)
	}

	processes, err := scenario.Machine.QueryGPUProcesses("host-01", "GPU-0")
	if err != nil {
		t.Fatal(err)
	}
	if len(processes) != 2 || processes[0].PID != 4321 || processes[0].MemoryUsedMB != 22000 {
		t.Fatalf("unexpected GPU-0 processes: %+v", processes)
	}
}

func TestMachineStateRejectsInconsistentProcessMemory(t *testing.T) {
	machine := HighMemoryScenario().Machine
	machine.Processes = append(machine.Processes, Process{PID: 9999, GPUID: "GPU-0", Name: "extra", MemoryUsedMB: 2000})
	if err := machine.Validate(); err == nil {
		t.Fatal("process memory exceeding GPU used memory was accepted")
	}
}

func TestMachineQueriesDoNotExposeOtherTargetsOrUnknownGPUs(t *testing.T) {
	machine := HighMemoryScenario().Machine
	if _, err := machine.QueryGPUStatus("host-02"); err == nil {
		t.Fatal("query for another target was accepted")
	}
	if _, err := machine.QueryGPUProcesses("host-01", "GPU-9"); err == nil {
		t.Fatal("query for unknown GPU was accepted")
	}
}

func TestXIDScenarioIsConsistentAndStable(t *testing.T) {
	scenario := XIDScenario()
	if err := scenario.Validate(); err != nil {
		t.Fatalf("XIDScenario() is invalid: %v", err)
	}
	statuses, err := scenario.Machine.QueryGPUStatus("host-01")
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].GPUID != "GPU-0" || statuses[0].Availability != model.GPUUnavailable || statuses[0].MemoryTotalMB != 0 {
		t.Fatalf("unexpected unavailable GPU status: %+v", statuses[0])
	}
	cutoff := time.Date(2026, 8, 20, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	events, err := scenario.Machine.QueryXIDEvents("host-01", "GPU-0", cutoff, 20)
	if err != nil || len(events) != 1 || events[0].Code != 79 {
		t.Fatalf("unexpected Xid events: events=%+v err=%v", events, err)
	}
	logs, err := scenario.Machine.QueryRecentKernelLogs("host-01", "GPU-0", cutoff, 50)
	if err != nil || len(logs) != 1 || logs[0].RelatedXIDCode == nil || *logs[0].RelatedXIDCode != 79 {
		t.Fatalf("unexpected kernel logs: logs=%+v err=%v", logs, err)
	}
}

func TestMachineStateRejectsUnmatchedKernelXIDReference(t *testing.T) {
	machine := XIDScenario().Machine
	code := int64(31)
	machine.KernelLogs[0].RelatedXIDCode = &code
	if err := machine.Validate(); err == nil {
		t.Fatal("kernel log with unmatched Xid code was accepted")
	}
}
