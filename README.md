# GPU Read-only Diagnosis Agent

这是一个面向秋招展示的本地 Go + Mock GPU 只读故障诊断 Agent。它根据告警和已有 Observation，在白名单语义化工具中逐步选择只读检查，最后输出区分事实、推断、未知项和人工建议的证据化报告。

系统没有任意 Shell 或状态修改工具，不会终止进程、reset GPU、重载驱动、隔离节点或重启主机。所有恢复操作都由运维人员复核证据后决定。

## 当前实现

已经实现并通过自动测试：

- `DiagnosisState` 生命周期、预算和追加式历史；
- `PlannerDecision → ToolCall → Observation → ObservedFact → EvidenceRef → DiagnosisReport` 证据链；
- 单 Target Scope、只读工具白名单、参数范围、重复调用和执行预算策略；
- 统一 Mock Machine State、固定 Clock 和稳定 ID；
- Deterministic Planner、Orchestrator、Loop Guard 和报告校验；
- 高显存与 Xid / 掉卡两个 CLI 场景；
- Scope 越界、策略拒绝、工具失败、证据不足和报告越权等失败测试。

当前 Tool Registry 包含：

```text
query_gpu_status
query_gpu_processes
query_driver_status
query_xid_events
query_recent_kernel_logs
```

## 场景一：高显存

```bash
go run ./cmd/gpu-agent --scenario high-memory
```

预期路径：

```text
query_gpu_status
→ query_gpu_processes(GPU-0)
→ finish
```

报告只确认 GPU-0 的高显存快照以及 PID-4321 是本次观察中的主要直接占用来源，不确认内存泄漏，也不决定是否终止进程。

## 场景二：Xid / 掉卡

```bash
go run ./cmd/gpu-agent --scenario xid-drop
```

预期路径：

```text
query_gpu_status
→ query_driver_status
→ query_xid_events(GPU-0)
→ query_recent_kernel_logs(GPU-0)
→ finish
```

报告确认 GPU-0 unavailable、驱动与 NVML 可用、Xid 79 和对应 NVRM 日志；只有限推断这些证据与 GPU-0 的主机通信丢失一致。永久硬件损坏、具体底层原因、业务影响和恢复安全性仍属于未知项。

## 测试

项目当前使用 Go 1.25：

```bash
go test ./...
go test -race ./...
go vet ./...
```

端到端测试检查：

- 精确工具顺序、调用次数和终止原因；
- 同一 Alert 与 Mock State 得到相同路径、Facts 和报告；
- unavailable GPU 不产生伪造的实时指标；
- Mock 中设备、进程、Xid 与内核日志保持一致；
- EvidenceRef 解析到正确 Observation 和 Fact；
- 驱动不可用、无 Xid、日志不匹配等路径能够升级人工；
- 报告不确认内存泄漏、永久硬件损坏或已执行恢复动作。

## 当前未实现

- 高温、正常和未知异常场景；
- SOP Router 和结构化 SOP Runner；
- 实际 `PlannerContext` JSON；
- LLM Provider 和受限 LLM Planner；
- 真实 GPU、SSH 或生产系统接入。

## 下一步

下一轮先设计并实现第三条高温场景，再实现最小 SOP Router。等同类模糊告警能够在不同 Mock State 下产生多条合理调查路径，并具备稳定评测后，再接入实现相同 `Planner` 接口的受限 LLM Planner。

详细协议、字段和新对话交接顺序见 [`docs/agent-runtime-and-diagnosis-state.md`](docs/agent-runtime-and-diagnosis-state.md)。项目范围与协作边界见 [`AGENTS.md`](AGENTS.md)。
