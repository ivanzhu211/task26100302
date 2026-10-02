package batchtrace_test

import (
	"errors"
	"fmt"

	"batchtrace"
)

// 演示：接收完整账本，查询异常批次流向目标领料的合计数量上下界。
func Example() {
	// 一份账本只涉及一个仓库的一种物料：
	// 全部入库批次 + 全部领料 + 已确认来源。
	ledger, err := batchtrace.NewLedger(
		[]batchtrace.Batch{
			// 数量、到货时刻、失效时刻；[到货, 失效) 内可领用。
			{ID: "B1", Quantity: 5, Arrival: 0, Expiry: 100},
			{ID: "B2", Quantity: 3, Arrival: 0, Expiry: 100},
		},
		[]batchtrace.Issue{
			// 数量、发生时刻、候选来源批次集合。
			{ID: "I1", Quantity: 3, Time: 10, Candidates: []string{"B1", "B2"}},
			{ID: "I2", Quantity: 3, Time: 10, Candidates: []string{"B1", "B2"}},
			{ID: "I3", Quantity: 2, Time: 10, Candidates: []string{"B1"}},
		},
		nil, // 无已确认来源
	)
	if err != nil {
		panic(err)
	}

	// 质量人员发现批次 B1 异常：它流向目标领料 I1、I2 的合计数量区间。
	bounds, err := ledger.Query([]string{"B1"}, []string{"I1", "I2"})
	if errors.Is(err, batchtrace.ErrInfeasible) {
		panic("账本不存在合法分配，无法解释")
	}
	if err != nil {
		panic(err)
	}
	fmt.Printf("B1 流向 {I1,I2} 的合计数量: [%d, %d]\n", bounds.Min, bounds.Max)
	fmt.Printf("下界核查依据: %v\n", bounds.MinAllocation)
	fmt.Printf("上界核查依据: %v\n", bounds.MaxAllocation)
	// Output:
	// B1 流向 {I1,I2} 的合计数量: [3, 3]
	// 下界核查依据: [{I1 B1 3} {I2 B2 3} {I3 B1 2}]
	// 上界核查依据: [{I1 B2 3} {I2 B1 3} {I3 B1 2}]
}
