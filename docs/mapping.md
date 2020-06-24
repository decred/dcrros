# DCR <-> Rosetta Mapping

Mapping betweeen Rosetta (RTA) concepts and Decred (DCR)

RTA Account => DCR Address

# To Discuss

- Use debit/credit or input/output for types?
- Really use 'reversed' OpStatus for ops inside reversed txs?
- Support 'bare' (non-p2sh) multisig outputs?
- Handle ticket outputs as different types of balance?
  - TicketSubmissionBalance (voting power) 
  - TicketCommitmentBalance (locked amount)
- Dcrd errors are retriable by default? 

# TODO Endpoints

- [ ] /account/balance
- [x] /block
- [x] /block/transaction
- [ ] /construction/metadata
- [ ] /construction/submit
- [x] /mempool
- [x] /mempool/transaction
- [x] /network/list
- [x] /network/options
  - [ ] Fill in Allow struct
- [x] /network/status



