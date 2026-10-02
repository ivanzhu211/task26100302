package batchtrace_test

import (
	"errors"
	"fmt"

	"batchtrace"
)

// ExampleLedger_Query 演示接收一份完整账本并查询追溯区间。
func ExampleLedger_Query() {
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
	if err != nil {
		panic(err)
	}

	// 批次 B1 发现异常，追溯两笔目标领料的联合区间。
	it, err := ledger.Query([]string{"B1"}, []string{"I1", "I2"})
	if errors.Is(err, batchtrace.ErrInfeasible) {
		fmt.Println("账本不存在合法分配，无法解释")
		return
	}
	if err != nil {
		panic(err)
	}
	fmt.Printf("联合区间: [%d, %d]\n", it.Min, it.Max)
	for _, a := range it.MinWitness {
		fmt.Printf("下界依据: %s -> %s = %d\n", a.BatchID, a.IssueID, a.Quantity)
	}

	// 只查其中一笔时区间更宽；查询互不影响，账本不被修改。
	it2, _ := ledger.Query([]string{"B1"}, []string{"I1"})
	fmt.Printf("单查 I1: [%d, %d]\n", it2.Min, it2.Max)

	// Output:
	// 联合区间: [3, 3]
	// 下界依据: B1 -> I1 = 3
	// 下界依据: B2 -> I2 = 3
	// 下界依据: B1 -> I3 = 2
	// 单查 I1: [0, 3]
}
