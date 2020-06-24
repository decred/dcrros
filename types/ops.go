package types

type OpType string

func (tp OpType) String() string {
	return string(tp)
}

const (
	OpTypeDebit  OpType = "debit"
	OpTypeCredit OpType = "credit"
)

type OpStatus string

func (st OpStatus) String() string {
	return string(st)
}

const (
	OpStatusSuccess  OpStatus = "success"
	OpStatusReversed OpStatus = "reversed"
)
