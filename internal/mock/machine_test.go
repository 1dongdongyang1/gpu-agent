package mock

import "testing"

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
