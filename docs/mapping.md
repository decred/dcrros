# DCR <-> Rosetta Mapping

## Operation Types

DCR tx inputs are identified by type `debit` while outputs are identified by type `credit`.

## Fees

Transaction fees are not currently explicitly returned by the API. They must be calculated by clients as the difference between the sum of credit amounts and debit amounts.

## Addresses

Mapping between Rosetta (RTA) concepts and Decred (DCR).

RTA Account => DCR Address (encoded in the regular fashion)

'Bare' (i.e., non-P2SH encoded) multisig and non-version-0 PkScripts are encoded in a different format:

```
0x[2-byte-version][pkscript]
```

For example, the testnet output [caf9baa6aa2f73ab06408d64482ef0502dcad7e4283dd99f80fd03dc89c5ca1b:0](https://testnet.dcrdata.org/tx/caf9baa6aa2f73ab06408d64482ef0502dcad7e4283dd99f80fd03dc89c5ca1b/out/0) generates the following data:

```json
{
  "block_identifier": {
    "index": "439889",
    "hash": "00000073f6fdf7a5058a91f8ac4291f0608825bce96237b0c6534ea8ac85780c"
  },
  ...
  {
    "transaction_identifier": {
      "hash": "caf9baa6aa2f73ab06408d64482ef0502dcad7e4283dd99f80fd03dc89c5ca1b"
    },
    "operations": [
      ...
      {
        "operation_identifier": {
          "index": 1,
        },
        "type": "credit",
        "status": "success",
        "account": {
          "address": "0x000176a914936061ad3f1cc6591a15a81a0c561a10a459fbcd88ac"
	  "metadata": {
          	"script_version": 1
	  }
        },
        "amount": {
          "value": "8803",
          "currency": {
            "symbol": "DCR",
            "decimals": 8
          }
        },
        "metadata": {
          "output_index": 0,
        }
      }
    ]
  } 
}
```

Notice the address is specified as an hexadecimal string `0x000176a914936061ad3f1cc6591a15a81a0c561a10a459fbcd88ac`.

## Block Disapproval

Disapproved DCR blocks revert the **regular** (i.e., non-stake) transactions of the parent block. This is encoded in RTA blocks as operations with **type** `reversed`. Note that the **status** of operations are still returned as `success` and the the amount field is returned as a negative value, such that the Rosetta invariant of summing operation amounts correctly adds up to the current address balance.

For example, the testnet block [x]() has the following operation:

While the next block [x]() disapproves the parent block and thus has the following operation:

And the following block [x]() re-includes the previously disapproved transaction and thus has again the operation:
