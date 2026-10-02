package batchtrace

import "testing"

// checkWitness 核查一份完整分配是否满足全部业务约束：
// 每笔领料全额满足、批次不超量、只走候选且时间可用的来源、
// 不低于确认数量。返回异常批次流向目标领料的合计数量。
func checkWitness(t *testing.T, l *Ledger, witness []Assignment, anomalous, targets map[string]bool) int64 {
	t.Helper()
	perIssue := make(map[string]int64, len(l.issues))
	perBatch := make(map[string]int64, len(l.batches))
	confirmed := make(map[[2]string]int64, len(l.confirms))
	for _, c := range l.confirms {
		confirmed[[2]string{c.BatchID, c.IssueID}] += c.Quantity
	}
	assigned := make(map[[2]string]int64)
	var hit int64
	for _, a := range witness {
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
		is := l.issues[ii]
		found := false
		for _, c := range is.Candidates {
			if c == a.BatchID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("批次 %q 不在领料 %q 的候选范围内", a.BatchID, a.IssueID)
		}
		if !usableAt(l.batches[bi], is.Time) {
			t.Fatalf("批次 %q 在领料 %q 发生时刻不可用", a.BatchID, a.IssueID)
		}
		perIssue[a.IssueID] += a.Quantity
		perBatch[a.BatchID] += a.Quantity
		assigned[[2]string{a.BatchID, a.IssueID}] += a.Quantity
		if anomalous[a.BatchID] && targets[a.IssueID] {
			hit += a.Quantity
		}
	}
	for _, is := range l.issues {
		if perIssue[is.ID] != is.Quantity {
			t.Fatalf("领料 %q 未全额满足: 需 %d 实 %d", is.ID, is.Quantity, perIssue[is.ID])
		}
	}
	for _, b := range l.batches {
		if perBatch[b.ID] > b.Quantity {
			t.Fatalf("批次 %q 超量分配: 供 %d 分 %d", b.ID, b.Quantity, perBatch[b.ID])
		}
	}
	for k, q := range confirmed {
		if assigned[k] < q {
			t.Fatalf("确认记录 (%s,%s) 要求至少 %d，实际 %d", k[0], k[1], q, assigned[k])
		}
	}
	return hit
}

// 验收场景：批次 B1=5、B2=3；目标领料 I1=I2=3（均可用 B1、B2）；
// 非目标领料 I3=2（只能用 B1）。时间均有效，无确认记录。
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
	it, err := l.Query([]string{"B1"}, []string{"I1", "I2"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if it.Min != 3 || it.Max != 3 {
		t.Fatalf("联合区间应为 [3,3]，得到 [%d,%d]", it.Min, it.Max)
	}
	anomalous := map[string]bool{"B1": true}
	targets := map[string]bool{"I1": true, "I2": true}
	if got := checkWitness(t, l, it.MinWitness, anomalous, targets); got != it.Min {
		t.Fatalf("下界核查依据实际达成 %d，声明 %d", got, it.Min)
	}
	if got := checkWitness(t, l, it.MaxWitness, anomalous, targets); got != it.Max {
		t.Fatalf("上界核查依据实际达成 %d，声明 %d", got, it.Max)
	}
}

func TestAcceptanceSingleTarget(t *testing.T) {
	l := acceptanceLedger(t)
	for _, target := range []string{"I1", "I2"} {
		it, err := l.Query([]string{"B1"}, []string{target})
		if err != nil {
			t.Fatalf("Query(%s): %v", target, err)
		}
		if it.Min != 0 || it.Max != 3 {
			t.Fatalf("单查 %s 区间应为 [0,3]，得到 [%d,%d]", target, it.Min, it.Max)
		}
		anomalous := map[string]bool{"B1": true}
		targets := map[string]bool{target: true}
		if got := checkWitness(t, l, it.MinWitness, anomalous, targets); got != it.Min {
			t.Fatalf("%s 下界核查依据实际达成 %d，声明 %d", target, got, it.Min)
		}
		if got := checkWitness(t, l, it.MaxWitness, anomalous, targets); got != it.Max {
			t.Fatalf("%s 上界核查依据实际达成 %d，声明 %d", target, got, it.Max)
		}
	}
}
