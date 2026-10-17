// Claimed, ClaimPaid and ClaimPaymentDeferred event decoding for one
// settlement-vault receipt. Ported from the sn miner's sn.go because the
// daemon's receipt tests and reconciliations classify these events. The v33
// provider's sn.go still carries the older STSubnet printMinerClaimed; the
// claim command family owns any future merge of the two decoders.
package main

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/stabi"
)

type minerClaimReceiptEvents struct {
	Claimed  []*stabi.STSettlementVaultClaimed
	Paid     []*stabi.STSettlementVaultClaimPaid
	Deferred []*stabi.STSettlementVaultClaimPaymentDeferred
}

func decodeMinerClaimReceipt(receipt *types.Receipt, contract common.Address) minerClaimReceiptEvents {
	result := minerClaimReceiptEvents{}
	if receipt == nil {
		return result
	}
	for _, lg := range receipt.Logs {
		if lg.Address != contract {
			continue
		}
		if ev, err := stSettlementVault.UnpackClaimedEvent(lg); err == nil {
			result.Claimed = append(result.Claimed, ev)
			continue
		}
		if ev, err := stSettlementVault.UnpackClaimPaidEvent(lg); err == nil {
			result.Paid = append(result.Paid, ev)
			continue
		}
		if ev, err := stSettlementVault.UnpackClaimPaymentDeferredEvent(lg); err == nil {
			result.Deferred = append(result.Deferred, ev)
		}
	}
	return result
}
