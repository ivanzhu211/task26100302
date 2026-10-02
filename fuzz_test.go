package batchtrace

import (
	"math/rand"
	"testing"
)

// bruteForce 对小型账本枚举全部合法分配，返回异常批次流向目标领料
// 合计数量的真实最小/最大值；无解时 ok=false。
func bruteForce(l *Ledger, anomalous, targets map[string]bool) (lo, hi int64, ok bool) {
	type edge struct {
		bi, ii int
		lower  int64
		cap    int64
		hit    bool
	}
	confirmed := make(map[[2]int]int64)
	for _, c := range l.confirms {
		confirmed[[2]int{l.batchIndex[c.BatchID], l.issueIndex[c.IssueID]}] += c.Quantity
	}
	var edges []edge
	for ii, is := range l.issues {
		for _, bid := range is.Candidates {
			bi := l.batchIndex[bid]
			if !usableAt(l.batches[bi], is.Time) {
				continue
			}
			lw := confirmed[[2]int{bi, ii}]
			cap := is.Quantity
			if l.batches[bi].Quantity < cap {
				cap = l.batches[bi].Quantity
			}
			edges = append(edges, edge{
				bi: bi, ii: ii, lower: lw, cap: cap,
				hit: anomalous[bid] && targets[is.ID],
			})
		}
	}
	need := make([]int64, len(l.issues))
	for i, is := range l.issues {
		need[i] = is.Quantity
	}
	supply := make([]int64, len(l.batches))
	for i, b := range l.batches {
		supply[i] = b.Quantity
	}
	found := false
	var rec func(k int, acc int64)
	rec = func(k int, acc int64) {
		if k == len(edges) {
			for _, n := range need {
				if n != 0 {
					return
				}
			}
			if !found || acc < lo {
				lo = acc
			}
			if !found || acc > hi {
				hi = acc
			}
			found = true
			return
		}
		e := edges[k]
		for v := e.lower; v <= e.cap && v <= need[e.ii] && v <= supply[e.bi]; v++ {
			need[e.ii] -= v
			supply[e.bi] -= v
			add := int64(0)
			if e.hit {
				add = v
			}
			rec(k+1, acc+add)
			need[e.ii] += v
			supply[e.bi] += v
		}
	}
	rec(0, 0)
	return lo, hi, found
}

// TestFuzzSmallLedgers 用随机小账本对照暴力枚举结果，
// 验证区间端点是全部合法分配上的真实极值。
func TestFuzzSmallLedgers(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	var compared, infeasible, skipped int
	for trial := 0; trial < 1000; trial++ {
		nb := 1 + rng.Intn(3)
		ni := 1 + rng.Intn(4)
		wideWindow := rng.Intn(2) == 0 // 一半用例放宽时间窗，提高可行比例
		batches := make([]Batch, nb)
		for i := range batches {
			arrival := int64(rng.Intn(4))
			if wideWindow {
				arrival = 0
			}
			batches[i] = Batch{
				ID:       batchID(i),
				Quantity: 2 + int64(rng.Intn(6)),
				Arrival:  arrival,
				Expiry:   arrival + 1 + int64(rng.Intn(4)),
			}
			if wideWindow {
				batches[i].Expiry = 6
			}
		}
		issues := make([]Issue, ni)
		for i := range issues {
			var cands []string
			for _, b := range batches {
				if rng.Intn(2) == 0 {
					cands = append(cands, b.ID)
				}
			}
			if len(cands) == 0 {
				cands = append(cands, batches[rng.Intn(nb)].ID)
			}
			issues[i] = Issue{
				ID:         issueID(i),
				Quantity:   1 + int64(rng.Intn(3)),
				Time:       int64(rng.Intn(6)),
				Candidates: cands,
			}
		}
		var confirms []Confirmation
		if rng.Intn(2) == 0 {
			for _, is := range issues {
				for _, bid := range is.Candidates {
					if rng.Intn(4) != 0 {
						continue
					}
					b := batches[0]
					for _, bb := range batches {
						if bb.ID == bid {
							b = bb
						}
					}
					if !usableAt(b, is.Time) {
						continue
					}
					upper := is.Quantity
					if b.Quantity < upper {
						upper = b.Quantity
					}
					if upper < 1 {
						continue
					}
					confirms = append(confirms, Confirmation{
						BatchID:  bid,
						IssueID:  is.ID,
						Quantity: 1 + int64(rng.Intn(int(upper))),
					})
				}
			}
		}
		l, err := NewLedger(batches, issues, confirms)
		if err != nil {
			skipped++
			continue // 静态校验拒绝的输入跳过
		}
		anomalous := map[string]bool{}
		for _, b := range batches {
			if rng.Intn(2) == 0 {
				anomalous[b.ID] = true
			}
		}
		targets := map[string]bool{}
		for _, is := range issues {
			if rng.Intn(2) == 0 {
				targets[is.ID] = true
			}
		}
		var anomList, targList []string
		for id := range anomalous {
			anomList = append(anomList, id)
		}
		for id := range targets {
			targList = append(targList, id)
		}

		lo, hi, ok := bruteForce(l, anomalous, targets)
		it, qerr := l.Query(anomList, targList)
		if !ok {
			if qerr == nil {
				t.Fatalf("trial %d: 暴力枚举无解，Query 却返回 [%d,%d]", trial, it.Min, it.Max)
			}
			infeasible++
			continue
		}
		if qerr != nil {
			t.Fatalf("trial %d: 暴力枚举有解 [%d,%d]，Query 报错 %v", trial, lo, hi, qerr)
		}
		if it.Min != lo || it.Max != hi {
			t.Fatalf("trial %d: 区间应为 [%d,%d]，得到 [%d,%d]", trial, lo, hi, it.Min, it.Max)
		}
		if got := checkWitness(t, l, it.MinWitness, anomalous, targets); got != lo {
			t.Fatalf("trial %d: 下界核查依据实际达成 %d，应为 %d", trial, got, lo)
		}
		if got := checkWitness(t, l, it.MaxWitness, anomalous, targets); got != hi {
			t.Fatalf("trial %d: 上界核查依据实际达成 %d，应为 %d", trial, got, hi)
		}
		compared++
	}
	t.Logf("对比 %d 例，无解 %d 例，静态校验跳过 %d 例", compared, infeasible, skipped)
	if compared+infeasible < 300 {
		t.Fatalf("有效用例过少: 对比 %d，无解 %d", compared, infeasible)
	}
}
