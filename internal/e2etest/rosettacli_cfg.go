// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
)

func genRosettaCLICfg(rootAppData string) error {
	dataDir := filepath.Join(rootAppData, "rosetta-cli-data")
	data := `
{
  "network": {
    "blockchain": "decred",
    "network": "simnet"
  },
  "online_url": "http://127.0.0.1:20100",
  "data_directory": "` + dataDir + `",
  "http_timeout": 10,
  "max_retries": 5,
  "retry_elapsed_time": 0,
  "max_online_connections": 120,
  "max_sync_concurrency": 64,
  "tip_delay": 300,
  "log_configuration": false,
  "data": {
    "active_reconciliation_concurrency": 16,
    "inactive_reconciliation_concurrency": 4,
    "inactive_reconciliation_frequency": 250,
    "log_blocks": false,
    "log_transactions": false,
    "log_balance_changes": false,
    "log_reconciliations": false,
    "ignore_reconciliation_error": false,
    "exempt_accounts": "",
    "bootstrap_balances": "",
    "interesting_accounts": "",
    "reconciliation_disabled": false,
    "inactive_discrepency_search_disabled": false,
    "balance_tracking_disabled": false,
    "coin_tracking_disabled": false,
    "results_output_file": "",
    "historical_balance_enabled": true,
    "end_conditions": {
      "reconciliation_coverage": {
        "coverage": 1.0,
        "tip": true
      }
    }
  },
  "construction": {
    "offline_url": "http://localhost:20103",
    "max_offline_connections": 0,
    "stale_depth": 0,
    "broadcast_limit": 0,
    "ignore_broadcast_failures": false,
    "clear_broadcasts": false,
    "broadcast_behind_tip": false,
    "block_broadcast_limit": 0,
    "rebroadcast_all": false,
    "end_conditions": {
      "create_account": 2
    },
    "prefunded_accounts": [{
      "privkey": "77561fd84f7e86e68b65e868324c390e9a5246f6eb9bf8f56c14d86511316f60",
      "account_identifier": {
        "address": "SsaJzoa8kcRQEuoy3L4oV65YiioNmDuvdVk",
	"metadata": {
          "script_version": 0
	}
      },
      "curve_type": "secp256k1",
      "currency": {
        "symbol": "DCR",
	"decimals": 8
      }
    }],
    "workflows": [
      {
        "name": "request_funds",
        "concurrency": 1,
        "scenarios": [
          {
            "name": "find_account",
            "actions": [
              {
                "input": "{\"symbol\":\"DCR\",\"decimals\":8}",
                "type": "set_variable",
                "output_path": "currency"
              },
              {
                "input": "{\"minimum_balance\": {\"value\": \"0\", \"currency\": {{currency}}}, \"create_limit\": 1}",
                "type": "find_balance",
                "output_path": "random_account"
              }
            ]
          },
          {
            "name": "request",
            "actions": [
              {
                "input": "{\"account_identifier\": {{random_account.account_identifier}}, \"minimum_balance\": {\"value\": \"100000000\", \"currency\": {{currency}}}}",
                "type": "find_balance",
                "output_path": "loaded_account"
              }
            ]
          }
        ]
      },
      {
        "name": "create_account",
        "concurrency": 1,
        "scenarios": [
          {
            "name": "create_account",
            "actions": [
              {
                "input": "{\"network\": \"simnet\", \"blockchain\": \"decred\"}",
                "type": "set_variable",
                "output_path": "network"
              },
              {
                "input": "{\"curve_type\": \"secp256k1\"}",
                "type": "generate_key",
                "output_path": "key"
              },
              {
                "input": "{\"network_identifier\": {{network}}, \"public_key\": {{key.public_key}}, \"metadata\": {\"script_version\": 0}}",
                "type": "derive",
                "output_path": "account"
              },
              {
                "input": "{\"account_identifier\": {{account.account_identifier}}, \"keypair\": {{key}}}",
                "type": "save_account"
              }
            ]
          }
        ]
      },
      {
        "name": "transfer",
        "concurrency": 2,
        "scenarios": [
          {
            "name": "transfer_dry_run",
            "actions": [
              {
                "input": "{\"network\":\"simnet\", \"blockchain\":\"decred\"}",
                "type": "set_variable",
                "output_path": "transfer_dry_run.network"
              },
              {
                "input": "{\"symbol\":\"DCR\", \"decimals\":8}",
                "type": "set_variable",
                "output_path": "currency"
              },
              {
                "input": "\"6030\"",
                "type": "set_variable",
                "output_path": "dust_amount"
              },
              {
                "input": "\"2980\"",
                "type": "set_variable",
                "output_path": "max_fee_amount"
              },
              {
                "input": "{\"operation\":\"addition\", \"left_value\": {{dust_amount}}, \"right_value\": {{max_fee_amount}}}",
                "type": "math",
                "output_path": "send_buffer"
              },
              {
                "input": "\"24000\"",
                "type": "set_variable",
                "output_path": "reserved_amount"
              },
              {
                "input": "{\"require_coin\":true, \"minimum_balance\":{\"value\": {{reserved_amount}}, \"currency\": {{currency}}}}",
                "type": "find_balance",
                "output_path": "sender"
              },
              {
                "input": "{\"operation\":\"subtraction\", \"left_value\": {{sender.balance.value}}, \"right_value\": {{send_buffer}}}",
                "type": "math",
                "output_path": "available_amount"
              },
              {
                "input": "{\"minimum\": {{dust_amount}}, \"maximum\": {{available_amount}}}",
                "type": "random_number",
                "output_path": "recipient_amount"
              },
              {
                "input": "{\"recipient_amount\":{{recipient_amount}}}",
                "type": "print_message"
              },
              {
                "input": "{\"operation\":\"subtraction\", \"left_value\": {{sender.balance.value}}, \"right_value\": {{recipient_amount}}}",
                "type": "math",
                "output_path": "total_change_amount"
              },
              {
                "input": "{\"operation\":\"subtraction\", \"left_value\": {{total_change_amount}}, \"right_value\": {{max_fee_amount}}}",
                "type": "math",
                "output_path": "change_amount"
              },
              {
                "input": "{\"change_amount\":{{change_amount}}}",
                "type": "print_message"
              },
              {
                "input": "{\"operation\":\"subtraction\", \"left_value\": \"0\", \"right_value\":{{sender.balance.value}}}",
                "type": "math",
                "output_path": "sender_amount"
              },
              {
                "input": "{\"not_account_identifier\":[{{sender.account_identifier}}], \"not_coins\":[{{sender.coin}}], \"minimum_balance\":{\"value\": \"0\", \"currency\": {{currency}}}, \"create_limit\": 100, \"create_probability\": 50}",
                "type": "find_balance",
                "output_path": "recipient"
              },
              {
                "input": "\"1\"",
                "type": "set_variable",
                "output_path": "transfer_dry_run.confirmation_depth"
              },
              {
                "input": "\"true\"",
                "type": "set_variable",
                "output_path": "transfer_dry_run.dry_run"
              },
              {
                "input": "[{\"operation_identifier\":{\"index\":0},\"type\":\"debit\",\"account\":{{sender.account_identifier}},\"amount\":{\"value\":{{sender_amount}},\"currency\":{{currency}}}, \"coin_change\":{\"coin_action\":\"coin_spent\", \"coin_identifier\":{{sender.coin}}}},{\"operation_identifier\":{\"index\":1},\"type\":\"credit\",\"account\":{{recipient.account_identifier}},\"amount\":{\"value\":{{recipient_amount}},\"currency\":{{currency}}}}, {\"operation_identifier\":{\"index\":2},\"type\":\"credit\",\"account\":{{sender.account_identifier}},\"amount\":{\"value\":{{change_amount}},\"currency\":{{currency}}}}]",
                "type": "set_variable",
                "output_path": "transfer_dry_run.operations"
              },
              {
                "input": "{{transfer_dry_run.operations}}",
                "type": "print_message"
              }
            ]
          },
          {
            "name": "transfer",
            "actions": [
              {
                "input": "{\"currency\":{{currency}}, \"amounts\":{{transfer_dry_run.suggested_fee}}}",
                "type": "find_currency_amount",
                "output_path": "suggested_fee"
              },
              {
                "input": "{\"operation\":\"subtraction\", \"left_value\": {{total_change_amount}}, \"right_value\": {{suggested_fee.value}}}",
                "type": "math",
                "output_path": "change_amount"
              },
              {
                "input": "{\"operation\":\"subtraction\", \"left_value\": {{change_amount}}, \"right_value\": {{dust_amount}}}",
                "type": "math",
                "output_path": "change_minus_dust"
              },
              {
                "input": "{{change_minus_dust}}",
                "type": "assert"
              },
              {
                "input": "{\"network\":\"simnet\", \"blockchain\":\"decred\"}",
                "type": "set_variable",
                "output_path": "transfer.network"
              },
              {
                "input": "\"1\"",
                "type": "set_variable",
                "output_path": "transfer.confirmation_depth"
              },
              {
                "input": "[{\"operation_identifier\":{\"index\":0},\"type\":\"debit\",\"account\":{{sender.account_identifier}},\"amount\":{\"value\":{{sender_amount}},\"currency\":{{currency}}}, \"coin_change\":{\"coin_action\":\"coin_spent\", \"coin_identifier\":{{sender.coin}}}},{\"operation_identifier\":{\"index\":1},\"type\":\"credit\",\"account\":{{recipient.account_identifier}},\"amount\":{\"value\":{{recipient_amount}},\"currency\":{{currency}}}}, {\"operation_identifier\":{\"index\":2},\"type\":\"credit\",\"account\":{{sender.account_identifier}},\"amount\":{\"value\":{{change_amount}},\"currency\":{{currency}}}}]",
                "type": "set_variable",
                "output_path": "transfer.operations"
              },
              {
                "input": "{{transfer.operations}}",
                "type": "print_message"
              }
            ]
          }
        ]
      },
      {
        "name": "return_funds",
        "concurrency": 10,
        "scenarios": [
          {
            "name": "transfer_dry_run",
            "actions": [
              {
                "input": "{\"network\":\"simnet\", \"blockchain\":\"decred\"}",
                "type": "set_variable",
                "output_path": "transfer_dry_run.network"
              },
              {
                "input": "{\"symbol\":\"DCR\", \"decimals\":8}",
                "type": "set_variable",
                "output_path": "currency"
              },
              {
                "input": "\"2980\"",
                "type": "set_variable",
                "output_path": "max_fee_amount"
              },
              {
                "input": "\"18000\"",
                "type": "set_variable",
                "output_path": "reserved_amount"
              },
              {
                "input": "{\"require_coin\":true, \"minimum_balance\":{\"value\": {{reserved_amount}}, \"currency\": {{currency}}}}",
                "type": "find_balance",
                "output_path": "sender"
              },
              {
                "input": "{\"operation\":\"subtraction\", \"left_value\": {{sender.balance.value}}, \"right_value\": {{max_fee_amount}}}",
                "type": "math",
                "output_path": "recipient_amount"
              },
              {
                "input": "{\"recipient_amount\":{{recipient_amount}}}",
                "type": "print_message"
              },
              {
                "input": "{\"operation\":\"subtraction\", \"left_value\": \"0\", \"right_value\":{{sender.balance.value}}}",
                "type": "math",
                "output_path": "sender_amount"
              },
              {
                "input": "\"1\"",
                "type": "set_variable",
                "output_path": "transfer_dry_run.confirmation_depth"
              },
              {
                "input": "\"true\"",
                "type": "set_variable",
                "output_path": "transfer_dry_run.dry_run"
              },
              {
                "input": "{\"address\": \"Ssp7J7TUmi5iPhoQnWYNGQbeGhu6V3otJcS\", \"metadata\":{\"script_version\": 0}}",
                "type": "set_variable",
                "output_path": "recipient"
              },
              {
                "input": "[{\"operation_identifier\":{\"index\":0},\"type\":\"debit\",\"account\":{{sender.account_identifier}},\"amount\":{\"value\":{{sender_amount}},\"currency\":{{currency}}}, \"coin_change\":{\"coin_action\":\"coin_spent\", \"coin_identifier\":{{sender.coin}}}},{\"operation_identifier\":{\"index\":1},\"type\":\"credit\",\"account\":{{recipient}},\"amount\":{\"value\":{{recipient_amount}},\"currency\":{{currency}}}}]",
                "type": "set_variable",
                "output_path": "transfer_dry_run.operations"
              },
              {
                "input": "{{transfer_dry_run.operations}}",
                "type": "print_message"
              }
            ]
          },
          {
            "name": "transfer",
            "actions": [
              {
                "input": "{\"currency\":{{currency}}, \"amounts\":{{transfer_dry_run.suggested_fee}}}",
                "type": "find_currency_amount",
                "output_path": "suggested_fee"
              },
              {
                "input": "{\"operation\":\"subtraction\", \"left_value\": {{sender.balance.value}}, \"right_value\": {{suggested_fee.value}}}",
                "type": "math",
                "output_path": "recipient_amount"
              },
              {
                "input": "\"6030\"",
                "type": "set_variable",
                "output_path": "dust_amount"
              },
              {
                "input": "{\"operation\":\"subtraction\", \"left_value\": {{recipient_amount}}, \"right_value\": {{dust_amount}}}",
                "type": "math",
                "output_path": "recipient_minus_dust"
              },
              {
                "input": "{{recipient_minus_dust}}",
                "type": "assert"
              },
              {
                "input": "{\"network\":\"simnet\", \"blockchain\":\"decred\"}",
                "type": "set_variable",
                "output_path": "transfer.network"
              },
              {
                "input": "\"1\"",
                "type": "set_variable",
                "output_path": "transfer.confirmation_depth"
              },
              {
                "input": "[{\"operation_identifier\":{\"index\":0},\"type\":\"debit\",\"account\":{{sender.account_identifier}},\"amount\":{\"value\":{{sender_amount}},\"currency\":{{currency}}}, \"coin_change\":{\"coin_action\":\"coin_spent\", \"coin_identifier\":{{sender.coin}}}},{\"operation_identifier\":{\"index\":1},\"type\":\"credit\",\"account\":{{recipient}},\"amount\":{\"value\":{{recipient_amount}},\"currency\":{{currency}}}}]",
                "type": "set_variable",
                "output_path": "transfer.operations"
              },
              {
                "input": "{{transfer.operations}}",
                "type": "print_message"
              }
            ]
          }
        ]
      }
    ]
  }
}
`

	cfgFilePath := filepath.Join(rootAppData, "rosetta-cli.json")
	return ioutil.WriteFile(cfgFilePath, []byte(data), os.ModePerm)
}
