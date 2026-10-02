package batchtrace

import (
	"math/rand"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// 规模参数：500 入库批次、1500 笔领料、30000 条候选来源关系。
const (
	scaleBatches     = 500
	scaleIssues      = 1500
	scaleCandidates  = 30000
	scaleMaxQuantity = 1_000_000_000_000 // 1e12
	candsPerIssue    = scaleCandidates / scaleIssues
)

// genScaleLedger 确定性生成满规模账本。
// 通过「先为每笔领料指定一个时间可用的主来源并据此放大量库存」
// 保证账本必然可行；候选集合再随机扩充到指定规模。
func genScaleLedger(seed int64) ([]Batch, []Issue, []Confirmation) {
	rng := rand.New(rand.NewSource(seed))

	batches := make([]Batch, scaleBatches)
	for i := range batches {
		arrival := rng.Int63n(5000)
		batches[i] = Batch{
			ID:      batchID(i),
			Arrival: arrival,
			Expiry:  arrival + 1000 + rng.Int63n(5000),
		}
	}

	issues := make([]Issue, scaleIssues)
	tally := make([]int64, scaleBatches)
	for i := range issues {
		// 找一个至少有一个可用批次的时刻
		var t int64
		var valid []int
		for {
			t = rng.Int63n(10000)
			valid = valid[:0]
			for bi, b := range batches {
				if b.Arrival <= t && t < b.Expiry {
					valid = append(valid, bi)
				}
			}
			if len(valid) > 0 {
				break
			}
		}
		// 主来源保证可行性
		primary := valid[rng.Intn(len(valid))]
		qty := 1 + rng.Int63n(1_000_000_000)
		if i%150 == 0 {
			qty = scaleMaxQuantity // 部分大数量领料
		}
		tally[primary] += qty

		candSet := map[int]bool{primary: true}
		for len(candSet) < candsPerIssue && len(candSet) < len(valid) {
			candSet[valid[rng.Intn(len(valid))]] = true
		}
		cands := make([]string, 0, len(candSet))
		for bi := range candSet {
			cands = append(cands, batchID(bi))
		}
		issues[i] = Issue{
			ID:         issueID(i),
			Quantity:   qty,
			Time:       t,
			Candidates: cands,
		}
	}

	// 库存 = 主来源占用 + 富余，部分批次放大到 1e12 级
	for i := range batches {
		slack := rng.Int63n(1_000_000_000)
		if i%50 == 0 {
			slack += scaleMaxQuantity
		}
		batches[i].Quantity = tally[i] + slack
	}

	// 少量确认记录，落在候选与时间范围内，且不超过批次与领料数量
	batchQty := make(map[string]int64, len(batches))
	for _, b := range batches {
		batchQty[b.ID] = b.Quantity
	}
	var confirms []Confirmation
	issueUsed := make(map[string]int64)
	batchUsed := make(map[string]int64)
	for i := 0; i < 200; i++ {
		is := issues[rng.Intn(len(issues))]
		bid := is.Candidates[rng.Intn(len(is.Candidates))]
		// 同一领料确认合计不超过其一半，同一批次确认合计不超过其库存
		upper := is.Quantity/2 - issueUsed[is.ID]
		if bq := batchQty[bid] - batchUsed[bid]; bq < upper {
			upper = bq
		}
		if upper < 1 {
			continue
		}
		qty := 1 + rng.Int63n(upper)
		issueUsed[is.ID] += qty
		batchUsed[bid] += qty
		confirms = append(confirms, Confirmation{
			BatchID:  bid,
			IssueID:  is.ID,
			Quantity: qty,
		})
	}
	return batches, issues, confirms
}

func batchID(i int) string {
	return "B" + itoa(i)
}

func issueID(i int) string {
	return "I" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	p := len(buf)
	for i > 0 {
		p--
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[p:])
}

// maxRSS 返回进程峰值常驻内存（字节）。
func maxRSS() int64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	if runtime.GOOS == "darwin" {
		return ru.Maxrss // 字节
	}
	return ru.Maxrss * 1024 // Linux 为 KiB
}

// TestScalePerformance 在完整规模下验证一次查询（含上下界与两份
// 完整分配）的耗时与峰值内存，并核查两份分配满足全部业务约束。
func TestScalePerformance(t *testing.T) {
	batches, issues, confirms := genScaleLedger(42)
	l, err := NewLedger(batches, issues, confirms)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	anomalous := make([]string, 0, 100)
	for i := 0; i < scaleBatches; i += 5 {
		anomalous = append(anomalous, batchID(i))
	}
	targets := make([]string, 0, 300)
	for i := 0; i < scaleIssues; i += 5 {
		targets = append(targets, issueID(i))
	}
	targetSet := make(map[string]bool, len(targets))
	for _, id := range targets {
		targetSet[id] = true
	}
	anomSet := make(map[string]bool, len(anomalous))
	for _, id := range anomalous {
		anomSet[id] = true
	}

	runtime.GC()
	start := time.Now()
	it, err := l.Query(anomalous, targets)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	peak := maxRSS()

	if it.Min > it.Max {
		t.Fatalf("区间下界 %d 大于上界 %d", it.Min, it.Max)
	}
	if got := checkWitness(t, l, it.MinWitness, anomSet, targetSet); got != it.Min {
		t.Fatalf("下界核查依据实际达成 %d，声明 %d", got, it.Min)
	}
	if got := checkWitness(t, l, it.MaxWitness, anomSet, targetSet); got != it.Max {
		t.Fatalf("上界核查依据实际达成 %d，声明 %d", got, it.Max)
	}

	const memLimit = 256 << 20 // 256 MiB
	t.Logf("规模: %d 批次 / %d 领料 / %d 候选关系", scaleBatches, scaleIssues, scaleCandidates)
	t.Logf("查询耗时: %v (限 10s)", elapsed)
	t.Logf("进程峰值内存: %.1f MiB (限 256 MiB)", float64(peak)/float64(uint64(1)<<20))
	t.Logf("区间: [%d, %d]", it.Min, it.Max)
	if elapsed > 10*time.Second {
		t.Fatalf("查询耗时 %v 超过 10 秒", elapsed)
	}
	if peak > memLimit {
		t.Fatalf("峰值内存 %d 字节超过 256 MiB", peak)
	}
}

// BenchmarkScaleQuery 供 go test -bench 重复测量完整规模查询。
func BenchmarkScaleQuery(b *testing.B) {
	batches, issues, confirms := genScaleLedger(42)
	l, err := NewLedger(batches, issues, confirms)
	if err != nil {
		b.Fatalf("NewLedger: %v", err)
	}
	anomalous := make([]string, 0, 100)
	for i := 0; i < scaleBatches; i += 5 {
		anomalous = append(anomalous, batchID(i))
	}
	targets := make([]string, 0, 300)
	for i := 0; i < scaleIssues; i += 5 {
		targets = append(targets, issueID(i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := l.Query(anomalous, targets); err != nil {
			b.Fatalf("Query: %v", err)
		}
	}
}
