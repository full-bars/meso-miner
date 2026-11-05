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
	if !budget.TryReserve(8) {
		t.Fatal("large waiter could not reserve its grant")
	}
	large.reset() // consuming the grant ends it
	// Only after the head has taken its grant can the follower receive one.
	budget.Release(2)
	select {
	case <-notifies[0]:
		if !budget.TryReserve(3) {
			t.Fatal("small waiter could not reserve its grant")
		}
	default:
		t.Fatal("small waiter did not wake after the large waiter")
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

// A cancelled head must not leave a follower it was blocking asleep.
func TestTransferMemoryBudgetFIFOCanceledHeadWakesFollower(t *testing.T) {
	budget := NewTransferMemoryBudget(10)
	budget.Reserve(7) // 3 available
	head, follower := newTransferMemoryBudgetWaiter(), newTransferMemoryBudgetWaiter()
	defer head.reset()
	defer follower.reset()
	head.subscribe(budget, 8)
	followerNotify := follower.subscribe(budget, 3)
	select {
	case <-followerNotify:
		t.Fatal("follower overtook the head")
	default:
	}
	head.reset()
	select {
	case <-followerNotify:
	default:
		t.Fatal("follower stayed asleep after the head was cancelled")
	}
}

// A notified head holds its grant across a second release until it resets.
func TestTransferMemoryBudgetFIFOGrantRetainedAcrossSecondRelease(t *testing.T) {
	budget := NewTransferMemoryBudget(10)
	budget.Reserve(10)
	head, follower := newTransferMemoryBudgetWaiter(), newTransferMemoryBudgetWaiter()
	defer head.reset()
	defer follower.reset()
	headNotify := head.subscribe(budget, 8)
	followerNotify := follower.subscribe(budget, 3)
	budget.Release(8)
	select {
	case <-headNotify:
	default:
		t.Fatal("head did not wake")
	}
	budget.Release(1) // second release before the head reserves
	select {
	case <-followerNotify:
		t.Fatal("follower woke against the head's outstanding grant")
	default:
	}
	if !budget.TryReserve(8) {
		t.Fatal("head could not reserve its grant")
	}
	head.reset()
	budget.Release(2) // 3 available, no grant outstanding
	select {
	case <-followerNotify:
	default:
		t.Fatal("follower did not wake once the head consumed its grant")
	}
}

// A granted waiter that gives up releases its grant to followers.
func TestTransferMemoryBudgetFIFOCanceledGrantWakesFollower(t *testing.T) {
	budget := NewTransferMemoryBudget(10)
	budget.Reserve(10)
	head, follower := newTransferMemoryBudgetWaiter(), newTransferMemoryBudgetWaiter()
	defer follower.reset()
	head.subscribe(budget, 8)
	followerNotify := follower.subscribe(budget, 3)
	budget.Release(9)
	head.reset()
	select {
	case <-followerNotify:
	default:
		t.Fatal("abandoned grant kept the follower asleep")
	}
}
