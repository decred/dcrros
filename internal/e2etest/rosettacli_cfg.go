// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
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
    "constructor_dsl_file": "construction.ros",
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
      "create_account": 2,
      "transfer": 10
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
    }]
  }
}
`

	constructData := `
request_funds(1){
  find_account{
    currency = {"symbol":"DCR", "decimals":8};
    random_account = find_balance({
      "minimum_balance":{
        "value": "0",
        "currency": {{currency}}
      },
      "create_limit":1
    });
  },

  // Create a separate scenario to request funds so that
  // the address we are using to request funds does not
  // get rolled back if funds do not yet exist.
  request{
    loaded_account = find_balance({
      "account_identifier": {{random_account.account_identifier}},
      "minimum_balance":{
        "value": "100000000",
        "currency": {{currency}}
      },
      "require_coin":true
    });
  }
}

create_account(1){
  create{
    network = {"network":"simnet", "blockchain":"decred"};
    key = generate_key({"curve_type": "secp256k1"});
    account = derive({
      "network_identifier": {{network}},
      "public_key": {{key.public_key}},
      "metadata": {"script_version": 0}
    });

    // If the account is not saved, the key will be lost!
    save_account({
      "account_identifier": {{account.account_identifier}},
      "keypair": {{key}}
    });
  }
}

transfer(10){
  transfer_dry_run{
    transfer_dry_run.network = {"network":"simnet", "blockchain":"decred"};
    currency = {"symbol":"DCR", "decimals":8};

    // We set the max_fee_amount to know how much buffer we should
    // leave for fee payment when selecting a sender account.
    dust_amount = "6030";
    max_fee_amount = "2980";
    send_buffer = {{dust_amount}} + {{max_fee_amount}};

    // We look for a coin of value >= the reserved_amount to create
    // a transfer with change (reserved_amount is max_fee_amount + dust_amount x 2).
    reserved_amount = "24000";
    sender = find_balance({
      "minimum_balance":{
        "value": {{reserved_amount}},
        "currency": {{currency}}
      },
      "require_coin": true
    });

    // The amount we send to the recipient is a random value
    // between the dust_amount and the value of the entire coin (minus
    // the amount reserved for fee payment and covering the dust minimum
    // of the change UTXO).
    receivable_amount = {{sender.balance.value}} - {{send_buffer}};
    recipient_amount = random_number({
      "minimum": {{dust_amount}},
      "maximum": {{receivable_amount}}
    });
    print_message({
      "recipient_amount":{{recipient_amount}}
    });

    // The change amount is what we aren't sending to the recipient
    // minus the maximum fee. Don't worry, we will adjust this
    // amount to avoid overpaying the fee after the dry run
    // completes.
    raw_change_amount = {{sender.balance.value}} - {{recipient_amount}};
    change_amount = {{raw_change_amount}} - {{max_fee_amount}};
    print_message({
      "change_amount":{{change_amount}}
    });

    // The last thing we need to do before creating the transaction
    // is to find a recipient with a *types.AccountIdentifier that
    // is not equal to the sender.
    recipient = find_balance({
      "not_account_identifier":[{{sender.account_identifier}}],
      "not_coins":[{{sender.coin}}],
      "minimum_balance":{
        "value": "0",
        "currency": {{currency}}
      },
      "create_limit": 100,
      "create_probability": 50
    });

    sender_amount = 0 - {{sender.balance.value}};
    transfer_dry_run.confirmation_depth = "1";
    transfer_dry_run.dry_run = true;
    transfer_dry_run.operations = [
      {
        "operation_identifier":{"index":0},
        "type":"debit",
        "account":{{sender.account_identifier}},
        "amount":{"value":{{sender_amount}},"currency":{{currency}}},
        "coin_change":{"coin_action":"coin_spent", "coin_identifier":{{sender.coin}}}
      },
      {
        "operation_identifier":{"index":1},
        "type":"credit",
        "account":{{recipient.account_identifier}},
        "amount":{"value":{{recipient_amount}},"currency":{{currency}}}
      },
      {
        "operation_identifier":{"index":2},
        "type":"credit",
        "account":{{sender.account_identifier}},
        "amount":{"value":{{change_amount}},"currency":{{currency}}}
      }
    ];
  },
  transfer{
    // The suggested_fee is returned in the /construction/metadata
    // response and saved to transfer_dry_run.suggested_fee.
    suggested_fee = find_currency_amount({
      "currency":{{currency}},
      "amounts":{{transfer_dry_run.suggested_fee}}
    });

    // We can access the variables of other scenarios, so we don't
    // need to recalculate raw_change_amount.
    change_amount = {{raw_change_amount}} - {{suggested_fee.value}};
    transfer.network = {{transfer_dry_run.network}};
    transfer.confirmation_depth = {{transfer_dry_run.confirmation_depth}};
    transfer.operations = [
      {
        "operation_identifier":{"index":0},
        "type":"debit",
        "account":{{sender.account_identifier}},
        "amount":{"value":{{sender_amount}},"currency":{{currency}}},
        "coin_change":{"coin_action":"coin_spent", "coin_identifier":{{sender.coin}}}
      },
      {
        "operation_identifier":{"index":1},
        "type":"credit",
        "account":{{recipient.account_identifier}},
        "amount":{"value":{{recipient_amount}},"currency":{{currency}}}
      },
      {
        "operation_identifier":{"index":2},
        "type":"credit",
        "account":{{sender.account_identifier}},
        "amount":{"value":{{change_amount}},"currency":{{currency}}}
      }
    ];
  }
}

return_funds(10){
  transfer_dry_run{
    transfer_dry_run.network = {"network":"simnet", "blockchain":"decred"};
    currency = {"symbol":"DCR", "decimals":8};

    // We look for a sender that is able to pay the
    // max_fee_amount + min_utxo size (reserved_amount is max_fee_amount + min_utxo size).
    max_fee_amount = "2980";
    reserved_amount = "18000";
    sender = find_balance({
      "minimum_balance":{
        "value": {{reserved_amount}},
        "currency": {{currency}}
      },
      "require_coin": true
    });

    // We send the maximum amount available to the recipient. Don't worry
    // we will modify this after the dry run to make sure we don't overpay.
    recipient_amount = {{sender.balance.value}} - {{max_fee_amount}};
    print_message({
      "recipient_amount":{{recipient_amount}}
    });

    // We load the recipient address from an ENV.
    // The reason we read from ENV variable is that the recipient address here
    // is the sender's address from transfer workflow which is not accessible
    // by this workflow.
    // When setting the ENV variable make sure to set it so that it has double
    // quotes around, otherwise it will give a parsing issue
    // Set it using export RECIPIENT=\"<sender's address>\"
    // Eventually we would want it to be automatically set by transfer workflow
    recipient_address = load_env("RECIPIENT");
    recipient = {"address": {{recipient_address}}};

    sender_amount = 0 - {{sender.balance.value}};
    transfer_dry_run.confirmation_depth = "1";
    transfer_dry_run.dry_run = true;
    transfer_dry_run.operations = [
      {
        "operation_identifier":{"index":0},
        "type":"debit",
        "account":{{sender.account_identifier}},
        "amount":{"value":{{sender_amount}},"currency":{{currency}}},
        "coin_change":{"coin_action":"coin_spent", "coin_identifier":{{sender.coin}}}
      },
      {
        "operation_identifier":{"index":1},
        "type":"credit",
        "account":{{recipient}},
        "amount":{"value":{{recipient_amount}},"currency":{{currency}}}
      }
    ];
  },
  transfer{
    // The suggested_fee is returned in the /construction/metadata
    // response and saved to transfer_dry_run.suggested_fee.
    suggested_fee = find_currency_amount({
      "currency":{{currency}},
      "amounts":{{transfer_dry_run.suggested_fee}}
    });

    // We calculate the recipient_amount using the new suggested_fee
    // and assert that it is above the minimum UTXO size.
    recipient_amount = {{sender.balance.value}} - {{suggested_fee.value}};
    dust_amount = "6030";
    recipient_minus_dust = {{recipient_amount}} - {{dust_amount}};
    assert({{recipient_minus_dust}});

    transfer.network = {{transfer_dry_run.network}};
    transfer.confirmation_depth = {{transfer_dry_run.confirmation_depth}};
    transfer.operations = [
      {
        "operation_identifier":{"index":0},
        "type":"debit",
        "account":{{sender.account_identifier}},
        "amount":{"value":{{sender_amount}},"currency":{{currency}}},
        "coin_change":{"coin_action":"coin_spent", "coin_identifier":{{sender.coin}}}
      },
      {
        "operation_identifier":{"index":1},
        "type":"credit",
        "account":{{recipient}},
        "amount":{"value":{{recipient_amount}},"currency":{{currency}}}
      }
    ];
  }
}
`

	cfgFilePath := filepath.Join(rootAppData, "rosetta-cli.json")
	if err := os.WriteFile(cfgFilePath, []byte(data), os.ModePerm); err != nil {
		return err
	}

	constFilePath := filepath.Join(rootAppData, "construction.ros")
	if err := os.WriteFile(constFilePath, []byte(constructData), os.ModePerm); err != nil {
		return err
	}

	return nil
}
