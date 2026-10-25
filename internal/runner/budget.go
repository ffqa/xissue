package runner

import (
	"context"
	"sync"
)

// budget limits concurrent agent processes by count and estimated memory.
type budget struct {
	mu       sync.Mutex
	cond     *sync.Cond
	maxCount int
	maxMem   int // MB, 0 = unlimited
	count    int
	mem      int
	waiting  int
}

func newBudget(maxCount, maxMem int) *budget {
	b := &budget{maxCount: maxCount, maxMem: maxMem}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// acquire blocks until there is room for one more process using memMB.
// A single process larger than the whole budget is still allowed when nothing else runs.
func (b *budget) acquire(ctx context.Context, memMB int) error {
	stop := context.AfterFunc(ctx, func() {
		b.mu.Lock()
		b.cond.Broadcast()
		b.mu.Unlock()
	})
	defer stop()
	b.mu.Lock()
	defer b.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fitsCount := b.count < b.maxCount
		fitsMem := b.maxMem <= 0 || b.count == 0 || b.mem+memMB <= b.maxMem
		if fitsCount && fitsMem {
			b.count++
			b.mem += memMB
			return nil
		}
		b.waiting++
		b.cond.Wait()
		b.waiting--
	}
}

func (b *budget) running() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

func (b *budget) waitingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.waiting
}

func (b *budget) release(memMB int) {
	b.mu.Lock()
	b.count--
	b.mem -= memMB
	b.cond.Broadcast()
	b.mu.Unlock()
}
