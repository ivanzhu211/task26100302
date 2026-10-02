package batchtrace

import (
	"math/rand"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// 完整规模性能验证：500 入库批次、1500 笔领料、30000 条候选来源关系、
// 单笔数量达 1e12。一次查询（上下界 + 两份完整分配）应在 10 秒内完成，
// 峰值堆内存不超过 256 MiB。固定随机种子，可重复运行。
func TestPerfFullScale(t *testing.T) {
	const (
		numBatches = 500
		numIssues  = 1500
		numEdges   = 30000
		maxQty     = int64(1_000_000_000_000)
	)
	rng := rand.New(rand.NewSource(20261002))

	batches := make([]Batch, numBatches)
	for i := range batches {
		if i%5 < 2 { // 40% 批次全程可领，保证局部候选充足
			batches[i] = Batch{
				ID:       batchID(i),
				Quantity: maxQty/2 + rng.Int63n(maxQty/2),
				Arrival:  0,
				Expiry:   1000,
			}
		} else {
			batches[i] = Batch{
				ID:       batchID(i),
				Quantity: maxQty/2 + rng.Int63n(maxQty/2), // [5e11, 1e12)
				Arrival:  rng.Int63n(300),
				Expiry:   700 + rng.Int63n(300),
			}
		}
	}
	// 总需求约为总供给的 60%，保证整体充裕。
	totalSupply := int64(0)
	for _, b := range batches {
		totalSupply += b.Quantity
	}
	target := totalSupply * 3 / 5
	perIssue := target / numIssues

	issues := make([]Issue, numIssues)
	edges := 0
	for i := range issues {
		issues[i] = Issue{
			ID:         issueID(i),
			Quantity:   perIssue/2 + rng.Int63n(perIssue),
			Time:       100 + rng.Int63n(800),
			Candidates: make([]string, 0, numEdges/numIssues),
		}
		for j := 0; j < numEdges/numIssues-2; j++ {
			issues[i].Candidates = append(issues[i].Candidates, batchID(rng.Intn(numBatches)))
			edges++
		}
		// 每笔领料保证至少 2 个时间有效的候选，避免随机构造出局部不可行。
		for j := 0; j < 2; j++ {
			bi := rng.Intn(numBatches)
			for !usableAt(batches[bi], issues[i].Time) {
				bi = rng.Intn(numBatches)
			}
			issues[i].Candidates = append(issues[i].Candidates, batchID(bi))
			edges++
		}
	}

	l, err := NewLedger(batches, issues, nil)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	abnormalSet := make(map[string]bool)
	abnormal := make([]string, 0, 250)
	// 先把全程可领的批次全部标为异常，迫使下界显著大于 0。
	for i, b := range batches {
		if i%5 < 2 {
			abnormalSet[b.ID] = true
			abnormal = append(abnormal, b.ID)
		}
	}
	for len(abnormal) < 250 {
		id := batchID(rng.Intn(numBatches))
		if !abnormalSet[id] {
			abnormalSet[id] = true
			abnormal = append(abnormal, id)
		}
	}
	targetSet := make(map[string]bool)
	targets := make([]string, 0, 500)
	for len(targets) < 500 {
		id := issueID(rng.Intn(numIssues))
		if !targetSet[id] {
			targetSet[id] = true
			targets = append(targets, id)
		}
	}

	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	start := time.Now()

	bounds, err := l.Query(abnormal, targets)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	elapsed := time.Since(start)
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	heapInUse := memAfter.HeapInuse

	t.Logf("规模: %d 批次, %d 领料, %d 候选关系", numBatches, numIssues, edges)
	t.Logf("查询耗时: %v", elapsed)
	t.Logf("查询后堆占用: %.1f MiB (累计分配 %.1f MiB)",
		float64(heapInUse)/(1<<20), float64(memAfter.TotalAlloc-memBefore.TotalAlloc)/(1<<20))
	t.Logf("区间: [%d, %d]，下界依据 %d 条分配，上界依据 %d 条分配",
		bounds.Min, bounds.Max, len(bounds.MinAllocation), len(bounds.MaxAllocation))

	if elapsed > 10*time.Second {
		t.Fatalf("查询耗时 %v 超过 10 秒", elapsed)
	}
	if heapInUse > 256<<20 {
		t.Fatalf("堆占用 %.1f MiB 超过 256 MiB", float64(heapInUse)/(1<<20))
	}
	if bounds.Min > bounds.Max {
		t.Fatalf("区间下界 %d 大于上界 %d", bounds.Min, bounds.Max)
	}
	// 两份核查依据必须分别达到所报端点并满足全部业务约束。
	if got := verifyAllocation(t, l, bounds.MinAllocation, abnormalSet, targetSet); got != bounds.Min {
		t.Fatalf("下界核查依据异常流量 = %d，与所报下界 %d 不符", got, bounds.Min)
	}
	if got := verifyAllocation(t, l, bounds.MaxAllocation, abnormalSet, targetSet); got != bounds.Max {
		t.Fatalf("上界核查依据异常流量 = %d，与所报上界 %d 不符", got, bounds.Max)
	}
}

func batchID(i int) string {
	return "B" + strconv.Itoa(i)
}

func issueID(i int) string {
	return "I" + strconv.Itoa(i)
}
