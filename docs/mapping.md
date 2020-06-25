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
- Do not use the float64's values in calcFinalBalance()?
  - This would involve querying the prev outs ourselves instead of relying on searchrawtransactions endpoint

# TODO Endpoints

- [x] /account/balance
- [x] /block
- [x] /block/transaction
- [x] /construction/metadata
- [x] /construction/submit
- [x] /mempool
- [x] /mempool/transaction
- [x] /network/list
- [x] /network/options
  - [x] Fill in Allow struct
- [x] /network/status



