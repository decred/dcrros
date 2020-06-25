package types

import (
	rtypes "github.com/coinbase/rosetta-sdk-go/types"
)

// OpType is a type convertible to rosetta's OperationType type.
type OpType string

// RType converts the given type to a rosetta OperationType.
func (tp OpType) RType() string {
	return string(tp)
}

const (
	// Note: After adding new types, also modify AllOpTypes().

	OpTypeDebit  OpType = "debit"
	OpTypeCredit OpType = "credit"
)

// AllOpTypes returns all OpTypes in a structure suitable for use in an Allow
// response.
func AllOpTypes() []string {
	return []string{
		OpTypeDebit.RType(),
		OpTypeCredit.RType(),
	}
}

// OpStatus is a type convertible to rosetta's OperationStatus type.
type OpStatus string

// RStatus converts the given opStatus to a rosetta OperationStatus.
func (st OpStatus) RStatus() *rtypes.OperationStatus {
	return &rtypes.OperationStatus{
		Status:     string(st),
		Successful: true,
	}
}

const (
	// Note: After adding new types also modify AllOpStatus.

	OpStatusSuccess  OpStatus = "success"
	OpStatusReversed OpStatus = "reversed"
)

// AllOpStatus returns all known operation status in a format suitable for
// inclusion in an Allow response.
func AllOpStatus() []*rtypes.OperationStatus {
	return []*rtypes.OperationStatus{
		OpStatusSuccess.RStatus(),
		OpStatusReversed.RStatus(),
	}
}
