package batchtrace

import (
	"errors"
	"testing"
)

// 已确认来源挤占其他领料的可用库存：
// B1=5、B2=3；I1=3（候选 B1,B2）、I2=3（候选 B1,B2）。
// 确认 B2->I2 至少 3，即 B2 被 I2 占满，I1 只能全部来自 B1。
// 异常批次为 B2 时，I1 的区间必为 [0,0]；异常批次为 B1 时 I1 必为 [3,3]。
func TestConfirmationCrowdsOut(t *testing.T) {
	l, err := NewLedger(
		[]Batch{
			{ID: "B1", Quantity: 5, Arrival: 0, Expiry: 100},
			{ID: "B2", Quantity: 3, Arrival: 0, Expiry: 100},
		},
		[]Issue{
			{ID: "I1", Quantity: 3, Time: 10, Candidates: []string{"B1", "B2"}},
			{ID: "I2", Quantity: 3, Time: 10, Candidates: []string{"B1", "B2"}},
		},
		[]Confirmation{{BatchID: "B2", IssueID: "I2", Quantity: 3}},
	)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	it, err := l.Query([]string{"B2"}, []string{"I1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if it.Min != 0 || it.Max != 0 {
		t.Fatalf("B2 被确认量占满，I1 区间应为 [0,0]，得到 [%d,%d]", it.Min, it.Max)
	}
	anom := map[string]bool{"B2": true}
	tg := map[string]bool{"I1": true}
	checkWitness(t, l, it.MinWitness, anom, tg)
	checkWitness(t, l, it.MaxWitness, anom, tg)

	it, err = l.Query([]string{"B1"}, []string{"I1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if it.Min != 3 || it.Max != 3 {
		t.Fatalf("I1 只能全部来自 B1，区间应为 [3,3]，得到 [%d,%d]", it.Min, it.Max)
	}
	anom = map[string]bool{"B1": true}
	checkWitness(t, l, it.MinWitness, anom, tg)
	checkWitness(t, l, it.MaxWitness, anom, tg)
}

// 到货及失效边界：到货时刻可领用，失效时刻不可领用。
// B1 到货=10 失效=20；I1 在 t=10（可领 B1），I2 在 t=20（不可领 B1）。
func TestArrivalExpiryBoundary(t *testing.T) {
	l, err := NewLedger(
		[]Batch{
			{ID: "B1", Quantity: 4, Arrival: 10, Expiry: 20},
			{ID: "B2", Quantity: 4, Arrival: 0, Expiry: 100},
		},
		[]Issue{
			{ID: "I1", Quantity: 3, Time: 10, Candidates: []string{"B1", "B2"}},
			{ID: "I2", Quantity: 3, Time: 20, Candidates: []string{"B1", "B2"}},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	// I2 在失效时刻发生，B1 不可用，只能全部来自 B2。
	it, err := l.Query([]string{"B1"}, []string{"I2"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if it.Min != 0 || it.Max != 0 {
		t.Fatalf("I2 在 B1 失效时刻发生，区间应为 [0,0]，得到 [%d,%d]", it.Min, it.Max)
	}
	anom := map[string]bool{"B1": true}
	tg := map[string]bool{"I2": true}
	checkWitness(t, l, it.MinWitness, anom, tg)
	checkWitness(t, l, it.MaxWitness, anom, tg)

	// I1 在 B1 到货时刻发生，B1 可用；但 I2 独占 B2 中的 3 单位，
	// B2 只剩 1 单位可给 I1，故 B1 至少流向 I1 2 单位，区间 [2,3]。
	it, err = l.Query([]string{"B1"}, []string{"I1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if it.Min != 2 || it.Max != 3 {
		t.Fatalf("I1 在 B1 到货时刻发生，区间应为 [2,3]，得到 [%d,%d]", it.Min, it.Max)
	}
	tg = map[string]bool{"I1": true}
	checkWitness(t, l, it.MinWitness, anom, tg)
	checkWitness(t, l, it.MaxWitness, anom, tg)
}

// 总入库量充足但局部候选来源不足：I1 只能用 B1（量 2），但 I1 需 3。
// 整本账不可行，查询必须返回 ErrInfeasible 而不是伪造区间。
func TestLocallyInfeasible(t *testing.T) {
	l, err := NewLedger(
		[]Batch{
			{ID: "B1", Quantity: 2, Arrival: 0, Expiry: 100},
			{ID: "B2", Quantity: 100, Arrival: 0, Expiry: 100},
		},
		[]Issue{
			{ID: "I1", Quantity: 3, Time: 10, Candidates: []string{"B1"}},
			{ID: "I2", Quantity: 3, Time: 10, Candidates: []string{"B2"}},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	_, err = l.Query([]string{"B1"}, []string{"I2"})
	if !errors.Is(err, ErrInfeasible) {
		t.Fatalf("局部候选不足应报 ErrInfeasible，得到 %v", err)
	}
}

// 确认记录超出批次供应量导致整本账不可行。
func TestConfirmationExceedsBatch(t *testing.T) {
	l, err := NewLedger(
		[]Batch{{ID: "B1", Quantity: 4, Arrival: 0, Expiry: 100}},
		[]Issue{
			{ID: "I1", Quantity: 3, Time: 10, Candidates: []string{"B1"}},
			{ID: "I2", Quantity: 3, Time: 10, Candidates: []string{"B1"}},
		},
		[]Confirmation{
			{BatchID: "B1", IssueID: "I1", Quantity: 3},
			{BatchID: "B1", IssueID: "I2", Quantity: 3},
		},
	)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	_, err = l.Query([]string{"B1"}, []string{"I1"})
	if !errors.Is(err, ErrInfeasible) {
		t.Fatalf("确认量超出批次供应应报 ErrInfeasible，得到 %v", err)
	}
}

// 确认量不绕过候选范围与时间限制：静态校验必须拒绝。
func TestConfirmationValidation(t *testing.T) {
	// 确认批次不在候选范围内
	_, err := NewLedger(
		[]Batch{{ID: "B1", Quantity: 5, Arrival: 0, Expiry: 100}},
		[]Issue{{ID: "I1", Quantity: 3, Time: 10, Candidates: []string{"B1"}}},
		[]Confirmation{{BatchID: "B2", IssueID: "I1", Quantity: 1}},
	)
	if err == nil {
		t.Fatal("确认引用未知批次应校验失败")
	}
	// 确认违反时间限制
	_, err = NewLedger(
		[]Batch{{ID: "B1", Quantity: 5, Arrival: 0, Expiry: 10}},
		[]Issue{{ID: "I1", Quantity: 3, Time: 10, Candidates: []string{"B1"}}},
		[]Confirmation{{BatchID: "B1", IssueID: "I1", Quantity: 1}},
	)
	if err == nil {
		t.Fatal("失效时刻的确认应校验失败")
	}
	// 确认量超过领料量
	_, err = NewLedger(
		[]Batch{{ID: "B1", Quantity: 50, Arrival: 0, Expiry: 100}},
		[]Issue{{ID: "I1", Quantity: 3, Time: 10, Candidates: []string{"B1"}}},
		[]Confirmation{{BatchID: "B1", IssueID: "I1", Quantity: 4}},
	)
	if err == nil {
		t.Fatal("确认量超过领料量应校验失败")
	}
}

// 大数量（1e12 级）精确计算：不允许浮点误差。
func TestLargeQuantitiesExact(t *testing.T) {
	const q = 1_000_000_000_000
	l, err := NewLedger(
		[]Batch{
			{ID: "B1", Quantity: q, Arrival: 0, Expiry: 100},
			{ID: "B2", Quantity: q, Arrival: 0, Expiry: 100},
		},
		[]Issue{
			{ID: "I1", Quantity: q, Time: 10, Candidates: []string{"B1", "B2"}},
			{ID: "I2", Quantity: q, Time: 10, Candidates: []string{"B2"}},
		},
		[]Confirmation{{BatchID: "B1", IssueID: "I1", Quantity: q - 1}},
	)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	it, err := l.Query([]string{"B1"}, []string{"I1", "I2"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// I2 只能由 B2 供给且恰好吃光 B2，I1 的 q 只能全部来自 B1，
	// 故异常批次 B1 流向目标的合计必为 q（确认量 q-1 含于其中）。
	if it.Min != q || it.Max != q {
		t.Fatalf("区间应为 [%d,%d]，得到 [%d,%d]", q, q, it.Min, it.Max)
	}
	anom := map[string]bool{"B1": true}
	tg := map[string]bool{"I1": true, "I2": true}
	if got := checkWitness(t, l, it.MinWitness, anom, tg); got != q {
		t.Fatalf("下界核查依据实际达成 %d", got)
	}
	if got := checkWitness(t, l, it.MaxWitness, anom, tg); got != q {
		t.Fatalf("上界核查依据实际达成 %d", got)
	}
}

// 查询不扣减库存、互不影响：同一账本连续两次查询结果一致。
func TestQueriesAreIndependent(t *testing.T) {
	l := acceptanceLedger(t)
	first, err := l.Query([]string{"B1"}, []string{"I1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	second, err := l.Query([]string{"B1"}, []string{"I1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if first.Min != second.Min || first.Max != second.Max {
		t.Fatalf("重复查询结果不一致: [%d,%d] vs [%d,%d]",
			first.Min, first.Max, second.Min, second.Max)
	}
	// 账本未被查询修改
	if len(l.batches) != 2 || l.batches[0].Quantity != 5 {
		t.Fatal("查询修改了账本")
	}
}
