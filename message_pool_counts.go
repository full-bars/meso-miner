package connect

// MessagePoolCounts returns the cumulative taken/returned/created message counts
// summed across all pools. taken-returned is the number of pool messages held by
// consumers, so growth across a load-then-teardown cycle attributes a lost return.
func MessagePoolCounts() (taken uint64, returned uint64, created uint64) {
	for _, bucket := range MessagePoolSummary() {
		taken += bucket.Taken
		returned += bucket.Returned
		created += bucket.Created
	}
	return taken, returned, created
}
