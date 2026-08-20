# GPU Read-only Diagnosis Agent

这是一个面向秋招演示的 Go + Mock GPU 只读故障诊断 Agent。当前第一条闭环使用确定性 Planner 验证受控 Agent 循环、统一 Mock、只读工具、证据链、终止机制和诊断报告。

当前可运行路径：

```text
模糊 GPU 异常告警
→ query_gpu_status
→ 发现 GPU-0 显存使用率超过演示阈值
→ query_gpu_processes(GPU-0)
→ 确认 PID-4321 是本次观察中的主要直接占用来源
→ 输出事实、推断、未知项、Evidence 和人工建议
```

系统没有任意 Shell 或状态修改工具，不会终止进程、reset GPU、重载驱动或重启主机，也不会仅凭单次高显存观察确认内存泄漏。

## 运行

需要 Go 1.25 或兼容版本：

```bash
go run ./cmd/gpu-agent --scenario high-memory
```

CLI 输出 JSON，其中包含工具调用路径、循环计数、终止原因和带 `ObservationID + FactID` 引用的报告。

## 测试

```bash
go test ./...
go test -race ./...
go vet ./...
```

端到端测试会检查：

- 精确工具顺序和调用次数；
- `finished / evidence_sufficient / issue_identified` 状态组合；
- Scope、白名单和只读安全边界；
- Mock 中 GPU 与进程显存的一致性；
- EvidenceRef 的 Observation/Fact 归属；
- 报告不越过证据边界；
- 同一 Alert 和 Mock State 产生相同路径、Facts 和报告。

完整的协议与设计边界见 [`docs/agent-runtime-and-diagnosis-state.md`](docs/agent-runtime-and-diagnosis-state.md)。
