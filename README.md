# 制造业批次追溯核算库（batchtrace）

供上层直接调用的 Go 库，账本保存在进程内存中。解决：工厂离线补录的生产领料只有数量与候选来源、没有准确批号时，计算一组异常入库批次流向一组目标领料的合计数量上下界，并各附一份覆盖整份账本的完整分配作为核查依据。

## 能力

- `NewLedger(batches, issues, confirmations)`：接收完整账本（单仓库单物料的全部入库批次、全部领料、已确认来源），做输入校验。
- `ledger.Query(abnormalBatches, targetIssues)`：返回 `Bounds{Min, Max, MinAllocation, MaxAllocation}`。两端分别是全部合法分配中的真实最小值与最大值；两份核查依据都是覆盖整份账本（含查询范围外领料）的完整分配。若整份账本不存在合法分配，返回 `ErrInfeasible`，不给出任何区间。

## 业务规则

- 数量为最小计量单位的正整数（`int64`，精确计算，无浮点）；时间为整数时刻。
- 批次可领用窗口为 `[Arrival, Expiry)`：到货时刻可领用，失效时刻不可领用。
- 领料只能使用候选集合内的批次；候选之外不可用。
- 确认记录为 `(领料, 批次)` 的已确认下界：最终分配不得少于该数量，且同样受候选范围与时间限制（违反者在 `NewLedger` 即被拒绝）。
- 合法分配须全额满足每笔领料、任何批次累计分配不超其数量；未用库存可留存；所有领料（含查询范围外）共同占用库存。
- 查询不扣减库存、不固定历史；同一账本上的不同查询相互独立。

## 实现

归约为运输问题上的最小费用最大流：源点→批次（容量=剩余供给）、批次→领料（候选且时间有效，费用 0/1）、领料→汇点（容量=剩余需求）。确认记录按边下界预处理（预先扣减供给与需求）。下界 = 异常∩目标边费用为 1 的最小费用流；上界 = 其余边费用为 1 的最小费用流。求解器为 primal-dual（Dijkstra 势 + 容许网络阻塞流），`int64` 精确运算。

## 调用示例

```go
ledger, err := batchtrace.NewLedger(
    []batchtrace.Batch{
        {ID: "B1", Quantity: 5, Arrival: 0, Expiry: 100},
        {ID: "B2", Quantity: 3, Arrival: 0, Expiry: 100},
    },
    []batchtrace.Issue{
        {ID: "I1", Quantity: 3, Time: 10, Candidates: []string{"B1", "B2"}},
        {ID: "I2", Quantity: 3, Time: 10, Candidates: []string{"B1", "B2"}},
        {ID: "I3", Quantity: 2, Time: 10, Candidates: []string{"B1"}},
    },
    nil,
)
if err != nil { /* 输入非法 */ }

bounds, err := ledger.Query([]string{"B1"}, []string{"I1", "I2"})
if errors.Is(err, batchtrace.ErrInfeasible) { /* 账本无法解释 */ }
// bounds.Min == 3, bounds.Max == 3
// bounds.MinAllocation / bounds.MaxAllocation 为覆盖整份账本的核查依据
```

完整可运行示例见 `example_test.go`。

## 测试与性能验证

```sh
go test ./...                # 全部单元测试与验收场景
go test -run TestPerfFullScale -v .   # 完整规模耗时与内存验证
```

`TestPerfFullScale`（固定随机种子，可重复运行）：500 入库批次、1500 笔领料、30000 条候选关系、单笔数量达 1e12。本机实测一次查询（上下界 + 两份完整分配）约 12ms，堆占用约 10 MiB，远低于 10 秒 / 256 MiB 的限制。
