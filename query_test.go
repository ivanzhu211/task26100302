package batchtrace

import "testing"

// verifyAllocation 核查一份完整分配是否满足全部业务约束，
// 并返回异常批次->目标领料的合计数量。
func verifyAllocation(t *testing.T, l *Ledger, alloc []Allocation, abnormal, target map[string]bool) int64 {
	t.Helper()
	issueSum := make(map[string]int64, len(l.issues))
	batchSum := make(map[string]int64, len(l.batches))
	pairSum := make(map[[2]string]int64)
	xSum := int64(0)
	for _, a := range alloc {
		if a.Quantity <= 0 {
			t.Fatalf("分配数量非正: %+v", a)
		}
		bi, ok := l.batchIndex[a.BatchID]
		if !ok {
			t.Fatalf("分配引用未知批次 %q", a.BatchID)
		}
		ii, ok := l.issueIndex[a.IssueID]
		if !ok {
			t.Fatalf("分配引用未知领料 %q", a.IssueID)
		}
		if !l.candidateSet[ii][bi] {
			t.Fatalf("分配 (%q,%q) 超出候选来源范围", a.IssueID, a.BatchID)
		}
		if !usableAt(l.batches[bi], l.issues[ii].Time) {
			t.Fatalf("分配 (%q,%q) 不满足时间限制", a.IssueID, a.BatchID)
		}
		issueSum[a.IssueID] += a.Quantity
		batchSum[a.BatchID] += a.Quantity
		pairSum[[2]string{a.IssueID, a.BatchID}] += a.Quantity
		if abnormal[a.BatchID] && target[a.IssueID] {
			xSum += a.Quantity
		}
	}
	// 每笔领料必须全额满足（包括查询范围之外的领料）。
	for _, is := range l.issues {
		if issueSum[is.ID] != is.Quantity {
			t.Fatalf("领料 %q 分配 %d，需求 %d", is.ID, issueSum[is.ID], is.Quantity)
		}
	}
	// 每个批次累计分配不得超过其数量。
	for _, b := range l.batches {
		if batchSum[b.ID] > b.Quantity {
			t.Fatalf("批次 %q 分配 %d 超过库存 %d", b.ID, batchSum[b.ID], b.Quantity)
		}
	}
	// 确认记录：最终分配不得少于确认数量。
	for _, c := range l.confirmations {
		if pairSum[[2]string{c.IssueID, c.BatchID}] < c.Quantity {
			t.Fatalf("确认 (%q,%q) 要求 >= %d，实际 %d",
				c.IssueID, c.BatchID, c.Quantity, pairSum[[2]string{c.IssueID, c.BatchID}])
		}
	}
	return xSum
}

func strSet(ids ...string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// 验收场景：批次 B1=5、B2=3；目标领料 I1=3、I2=3（都可用两个批次）；
// 非目标领料 I3=2 只能用 B1。时间均有效，无确认记录。
// 第一批标为异常：两笔目标领料联合区间必须为 [3,3]；只查任意一笔为 [0,3]。
func acceptanceLedger(t *testing.T) *Ledger {
	t.Helper()
	l, err := NewLedger(
		[]Batch{
			{ID: "B1", Quantity: 5, Arrival: 0, Expiry: 100},
			{ID: "B2", Quantity: 3, Arrival: 0, Expiry: 100},
		},
		[]Issue{
			{ID: "I1", Quantity: 3, Time: 10, Candidates: []string{"B1", "B2"}},
			{ID: "I2", Quantity: 3, Time: 10, Candidates: []string{"B1", "B2"}},
			{ID: "I3", Quantity: 2, Time: 10, Candidates: []string{"B1"}},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	return l
}

func TestAcceptanceJointTargets(t *testing.T) {
	l := acceptanceLedger(t)
	bounds, err := l.Query([]string{"B1"}, []string{"I1", "I2"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if bounds.Min != 3 || bounds.Max != 3 {
		t.Fatalf("联合区间 = [%d,%d]，期望 [3,3]", bounds.Min, bounds.Max)
	}
	abnormal := strSet("B1")
	target := strSet("I1", "I2")
	if got := verifyAllocation(t, l, bounds.MinAllocation, abnormal, target); got != bounds.Min {
		t.Fatalf("下界核查依据的异常流量 = %d，与所报下界 %d 不符", got, bounds.Min)
	}
	if got := verifyAllocation(t, l, bounds.MaxAllocation, abnormal, target); got != bounds.Max {
		t.Fatalf("上界核查依据的异常流量 = %d，与所报上界 %d 不符", got, bounds.Max)
	}
}

func TestAcceptanceSingleTarget(t *testing.T) {
	l := acceptanceLedger(t)
	for _, targetID := range []string{"I1", "I2"} {
		bounds, err := l.Query([]string{"B1"}, []string{targetID})
		if err != nil {
			t.Fatalf("Query(%s): %v", targetID, err)
		}
		if bounds.Min != 0 || bounds.Max != 3 {
			t.Fatalf("单查 %s 区间 = [%d,%d]，期望 [0,3]", targetID, bounds.Min, bounds.Max)
		}
		abnormal := strSet("B1")
		target := strSet(targetID)
		if got := verifyAllocation(t, l, bounds.MinAllocation, abnormal, target); got != bounds.Min {
			t.Fatalf("%s 下界核查依据异常流量 = %d，期望 %d", targetID, got, bounds.Min)
		}
		if got := verifyAllocation(t, l, bounds.MaxAllocation, abnormal, target); got != bounds.Max {
			t.Fatalf("%s 上界核查依据异常流量 = %d，期望 %d", targetID, got, bounds.Max)
		}
	}
}

// 已确认来源挤占其他领料可用库存：
// B1=5、B2=5；I1=5、I2=5 都可用两批次。确认 I2 从 B1 拿 5。
// 则 I1 只能用 B2：异常={B1}、目标={I1} 的区间必须为 [0,0]。
// 若确认改为 I2 从 B1 拿 2，则 B1 剩 3 可给 I1：区间 [0,3]。
func TestConfirmationSqueezesStock(t *testing.T) {
	mk := func(t *testing.T, confirmedQty int64) *Ledger {
		l, err := NewLedger(
			[]Batch{
				{ID: "B1", Quantity: 5, Arrival: 0, Expiry: 100},
				{ID: "B2", Quantity: 5, Arrival: 0, Expiry: 100},
			},
			[]Issue{
				{ID: "I1", Quantity: 5, Time: 10, Candidates: []string{"B1", "B2"}},
				{ID: "I2", Quantity: 5, Time: 10, Candidates: []string{"B1", "B2"}},
			},
			[]Confirmation{{IssueID: "I2", BatchID: "B1", Quantity: confirmedQty}},
		)
		if err != nil {
			t.Fatalf("NewLedger: %v", err)
		}
		return l
	}

	l := mk(t, 5)
	bounds, err := l.Query([]string{"B1"}, []string{"I1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if bounds.Min != 0 || bounds.Max != 0 {
		t.Fatalf("确认挤占后区间 = [%d,%d]，期望 [0,0]", bounds.Min, bounds.Max)
	}
	verifyAllocation(t, l, bounds.MinAllocation, strSet("B1"), strSet("I1"))
	verifyAllocation(t, l, bounds.MaxAllocation, strSet("B1"), strSet("I1"))

	l = mk(t, 2)
	bounds, err = l.Query([]string{"B1"}, []string{"I1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if bounds.Min != 0 || bounds.Max != 3 {
		t.Fatalf("确认 2 单位后区间 = [%d,%d]，期望 [0,3]", bounds.Min, bounds.Max)
	}
	if got := verifyAllocation(t, l, bounds.MaxAllocation, strSet("B1"), strSet("I1")); got != 3 {
		t.Fatalf("上界核查依据异常流量 = %d，期望 3", got)
	}
}

// 确认记录本身计入异常流量：
// B1=5；I1=5 只可用 B1。确认 I1 从 B1 拿 2。异常={B1}、目标={I1}：
// 上下界都必须是 5（确认 2 + 剩余 3 也只能来自 B1）。
func TestConfirmationCountsTowardBounds(t *testing.T) {
	l, err := NewLedger(
		[]Batch{{ID: "B1", Quantity: 5, Arrival: 0, Expiry: 100}},
		[]Issue{{ID: "I1", Quantity: 5, Time: 10, Candidates: []string{"B1"}}},
		[]Confirmation{{IssueID: "I1", BatchID: "B1", Quantity: 2}},
	)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	bounds, err := l.Query([]string{"B1"}, []string{"I1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if bounds.Min != 5 || bounds.Max != 5 {
		t.Fatalf("区间 = [%d,%d]，期望 [5,5]", bounds.Min, bounds.Max)
	}
	verifyAllocation(t, l, bounds.MinAllocation, strSet("B1"), strSet("I1"))
}

// 到货及失效边界：到货时刻可领用，失效时刻不可领用。
// B1 到货=10 失效=20；I1 时刻=10（可领），I2 时刻=20（不可领）。
// B2 全程可用。I1=I2=3，B1=3、B2=6。
// 异常={B1}：目标={I1} 区间 [0,3]；目标={I2} 区间必须 [0,0]（B1 在 20 已失效）。
func TestTimeBoundaries(t *testing.T) {
	l, err := NewLedger(
		[]Batch{
			{ID: "B1", Quantity: 3, Arrival: 10, Expiry: 20},
			{ID: "B2", Quantity: 6, Arrival: 0, Expiry: 100},
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

	bounds, err := l.Query([]string{"B1"}, []string{"I1"})
	if err != nil {
		t.Fatalf("Query(I1): %v", err)
	}
	if bounds.Min != 0 || bounds.Max != 3 {
		t.Fatalf("I1 区间 = [%d,%d]，期望 [0,3]", bounds.Min, bounds.Max)
	}
	verifyAllocation(t, l, bounds.MaxAllocation, strSet("B1"), strSet("I1"))

	bounds, err = l.Query([]string{"B1"}, []string{"I2"})
	if err != nil {
		t.Fatalf("Query(I2): %v", err)
	}
	if bounds.Min != 0 || bounds.Max != 0 {
		t.Fatalf("I2 区间 = [%d,%d]，期望 [0,0]（失效时刻不可领）", bounds.Min, bounds.Max)
	}
	verifyAllocation(t, l, bounds.MinAllocation, strSet("B1"), strSet("I2"))
}

// 总入库量充足但局部候选来源不足 -> 必须报告无法解释，不得伪造区间。
// B1=100、B2=100；I1=5 只可用 B1，I2=150 只可用 B2（B2 只有 100）。
func TestLocallyInfeasible(t *testing.T) {
	l, err := NewLedger(
		[]Batch{
			{ID: "B1", Quantity: 100, Arrival: 0, Expiry: 100},
			{ID: "B2", Quantity: 100, Arrival: 0, Expiry: 100},
		},
		[]Issue{
			{ID: "I1", Quantity: 5, Time: 10, Candidates: []string{"B1"}},
			{ID: "I2", Quantity: 150, Time: 10, Candidates: []string{"B2"}},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	_, err = l.Query([]string{"B1"}, []string{"I1"})
	if err != ErrInfeasible {
		t.Fatalf("期望 ErrInfeasible，得到 %v", err)
	}
}

// 确认数量超过批次库存 -> 无法解释。
func TestConfirmationExceedsBatch(t *testing.T) {
	l, err := NewLedger(
		[]Batch{{ID: "B1", Quantity: 3, Arrival: 0, Expiry: 100}},
		[]Issue{{ID: "I1", Quantity: 5, Time: 10, Candidates: []string{"B1"}}},
		[]Confirmation{{IssueID: "I1", BatchID: "B1", Quantity: 4}},
	)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	if _, err = l.Query([]string{"B1"}, []string{"I1"}); err != ErrInfeasible {
		t.Fatalf("期望 ErrInfeasible，得到 %v", err)
	}
}

// 违反候选范围或时间限制的确认记录 -> 输入校验拒绝。
func TestInvalidConfirmationsRejected(t *testing.T) {
	batches := []Batch{{ID: "B1", Quantity: 10, Arrival: 10, Expiry: 20}}
	issues := []Issue{{ID: "I1", Quantity: 5, Time: 5, Candidates: []string{"B1"}}} // 时刻 5 未到货

	// 时间非法的确认。
	if _, err := NewLedger(batches, issues,
		[]Confirmation{{IssueID: "I1", BatchID: "B1", Quantity: 1}}); err == nil {
		t.Fatal("时间非法的确认记录应被拒绝")
	}
	// 超出候选范围的确认。
	issues2 := []Issue{{ID: "I1", Quantity: 5, Time: 15, Candidates: []string{"B1"}}}
	batches2 := []Batch{
		{ID: "B1", Quantity: 10, Arrival: 0, Expiry: 100},
		{ID: "B2", Quantity: 10, Arrival: 0, Expiry: 100},
	}
	if _, err := NewLedger(batches2, issues2,
		[]Confirmation{{IssueID: "I1", BatchID: "B2", Quantity: 1}}); err == nil {
		t.Fatal("超出候选范围的确认记录应被拒绝")
	}
}

// 同一账本上的不同查询相互独立：查询不扣减库存、不固定历史。
// 连续两次相同查询结果一致；先查 I1 再查 I2 与直接查 I2 一致。
func TestQueriesAreIndependent(t *testing.T) {
	l := acceptanceLedger(t)
	first, err := l.Query([]string{"B1"}, []string{"I1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	second, err := l.Query([]string{"B1"}, []string{"I2"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	third, err := l.Query([]string{"B1"}, []string{"I1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if first.Min != third.Min || first.Max != third.Max {
		t.Fatalf("重复查询结果不一致: [%d,%d] vs [%d,%d]", first.Min, first.Max, third.Min, third.Max)
	}
	if second.Min != 0 || second.Max != 3 {
		t.Fatalf("I2 区间 = [%d,%d]，期望 [0,3]", second.Min, second.Max)
	}
}

// 大数量精确性：1e12 级别，不允许浮点误差。
func TestLargeQuantities(t *testing.T) {
	const big = int64(1_000_000_000_000)
	l, err := NewLedger(
		[]Batch{
			{ID: "B1", Quantity: big, Arrival: 0, Expiry: 100},
			{ID: "B2", Quantity: big, Arrival: 0, Expiry: 100},
		},
		[]Issue{
			{ID: "I1", Quantity: big, Time: 10, Candidates: []string{"B1", "B2"}},
			{ID: "I2", Quantity: big - 1, Time: 10, Candidates: []string{"B2"}},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	bounds, err := l.Query([]string{"B1"}, []string{"I1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// B2 要承担 I2 的 big-1，只剩 1 可给 I1，故 B1->I1 ∈ [big-1, big]。
	if bounds.Min != big-1 || bounds.Max != big {
		t.Fatalf("区间 = [%d,%d]，期望 [%d,%d]", bounds.Min, bounds.Max, big-1, big)
	}
	if got := verifyAllocation(t, l, bounds.MinAllocation, strSet("B1"), strSet("I1")); got != big-1 {
		t.Fatalf("核查依据异常流量 = %d，期望 %d", got, big-1)
	}
}
