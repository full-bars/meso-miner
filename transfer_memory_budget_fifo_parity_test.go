package connect

import "testing"

func TestTransferMemoryBudgetFIFORepeatedReleases(t *testing.T) {
	budget := NewTransferMemoryBudget(10)
	budget.Reserve(10)
	large := newTransferMemoryBudgetWaiter()
	defer large.reset()
	largeNotify := large.subscribe(budget, 8)
	smalls := make([]*transferMemoryBudgetWaiter, 3)
	notifies := make([]<-chan struct{}, len(smalls))
	for i := range smalls {
		smalls[i] = newTransferMemoryBudgetWaiter()
		defer smalls[i].reset()
		notifies[i] = smalls[i].subscribe(budget, 3)
	}
	for i := 0; i < 3; i++ {
		budget.Release(3)
		for _, notify := range notifies {
			select {
			case <-notify:
				t.Fatalf("small waiter overtook large waiter on release %d", i)
			default:
			}
		}
	}
	select {
	case <-largeNotify:
	default:
		t.Fatal("large waiter did not wake")
	}
}

func TestTransferMemoryBudgetFIFOImpossibleWaiter(t *testing.T) {
	budget := NewTransferMemoryBudget(10)
	budget.Reserve(10)
	impossible := newTransferMemoryBudgetWaiter()
	small := newTransferMemoryBudgetWaiter()
	defer impossible.reset()
	defer small.reset()
	impossibleNotify := impossible.subscribe(budget, 20)
	smallNotify := small.subscribe(budget, 3)
	budget.Release(3)
	select {
	case <-impossibleNotify:
		t.Fatal("impossible waiter woke")
	default:
	}
	select {
	case <-smallNotify:
	default:
		t.Fatal("impossible waiter blocked a fitting request")
	}
}

func TestTransferMemoryBudgetFIFOSiblingCeiling(t *testing.T) {
	root := NewTransferMemoryBudget(20)
	left := NewTransferMemoryBudgetWithParent(10, root)
	right := NewTransferMemoryBudgetWithParent(10, root)
	if !left.TryReserve(10) || !right.TryReserve(10) {
		t.Fatal("initial reservations failed")
	}
	l, r := newTransferMemoryBudgetWaiter(), newTransferMemoryBudgetWaiter()
	defer l.reset()
	defer r.reset()
	ln, rn := l.subscribe(left, 5), r.subscribe(right, 5)
	right.Release(5)
	select {
	case <-ln:
		t.Fatal("left woke without capacity")
	default:
	}
	select {
	case <-rn:
	default:
		t.Fatal("left ceiling blocked independent sibling")
	}
	left.Release(5)
	select {
	case <-ln:
	default:
		t.Fatal("left did not wake")
	}
}

func TestTransferMemoryBudgetFIFOSurplus(t *testing.T) {
	budget := NewTransferMemoryBudget(20)
	budget.Reserve(20)
	large, small := newTransferMemoryBudgetWaiter(), newTransferMemoryBudgetWaiter()
	defer large.reset()
	defer small.reset()
	ln, sn := large.subscribe(budget, 8), small.subscribe(budget, 3)
	budget.Release(11)
	select {
	case <-ln:
	default:
		t.Fatal("head did not wake")
	}
	select {
	case <-sn:
	default:
		t.Fatal("surplus did not wake follower")
	}
}
