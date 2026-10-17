package connect

import "github.com/urnetwork/connect/protocol"

// The receiver-visible identity of a transfer lane. The callback carries the
// source separately; peers reproduce these fields when constructing a reply.
//
// This is upstream's definition (transfer.go), carried in its own file until
// this line's transfer.go is brought forward, so that the fragment files can be
// taken from upstream unedited. It is an opaque map key on the fragment path.
type TransferKey struct {
	ForceStream         bool
	CompanionContract   bool
	EncryptionRole      protocol.SequenceRole
	EncryptionCompanion bool
	// LogicalLane is the bounded receiver-visible ordering lane. Zero is the
	// legacy/control lane; values 1..8 are accepted only after capability
	// negotiation and are reproduced on replies.
	LogicalLane uint32
}
