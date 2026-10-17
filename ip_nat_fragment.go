package connect

// natFragmentReassemblyMaxRetainedBytes bounds the fragment bytes one NAT send
// shard retains per family while it waits for a datagram to complete.
//
// The reassemblers charge each retained fragment's own bytes (ip_fragment.go:
// the packet's length, not the pool buffer's capacity), so the budget reads
// as the logical in-flight bytes. 64 KiB is upstream's own
// unbudgeted default (connect b31ec82df ip_nat_memory.go keeps 16 KiB only
// under a memory budget; this line has no per-nat budget, so it takes the
// larger value). It admits a 9 KiB fragmented reply (the measured 9,124-byte
// peer TLS flight, about 10 KiB charged now that the fragment's own bytes are
// charged) and payloads up to about 60 KiB, while the datagram (16) and
// fragment (64) caps remain the tighter concurrency bounds in most cases.
const natFragmentReassemblyMaxRetainedBytes = 64 * 1024
