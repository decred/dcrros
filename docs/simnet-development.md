# Developing With Simnet

Using Decred's Simulation Network (`simnet`) is the easiest way to test during
development, given the developer completely controls block production.

Assuming compatible versions of `dcrd` and `dcrwallet` are installed and that
the code repositories for `dcrd` and `dcrros` are checked out, perform the
following process to run rosetta-cli checks:

First: run dcrd's tmux simnet setup on a separate terminal (this sets up 2 dcrd
nodes, and voting wallets):

```shell
$ cd ../dcrd
$ ./contrib/dcr_tmux_simnet_setup.sh
```

Next, run dcrros and perform the checks (from inside `dcrros` dir):

```shell
$ go run . -C ./docs/simnet-tmux-dcrros.conf
$ rosetta-cli --configuration-file docs/roscli-simnet.json check:data 
```

To run `check:construction`, the simnet chain needs to be kept advancing and the
test account needs to be funded so that the validation tool can perform its
assertions.

To keep mining on simnet, access the `dcrd1` control terminal on the simnet
window and run:

```shell
$ while true ; do ./mine && sleep 1s ; done
```

Then start the `check:construction` tool:

```shell
$ rosetta-cli --configuration-file docs/roscli-simnet.json check:construction
```

The check tool will ask for funds on a generated address, like so:

```
2021/11/12 12:22:09 1 account(s) insufficiently funded. Please fund the address [Ssbs62a2SPtLcX3BvPKk35R72dVtMSqQYpV]
```

Send funds to the specified address by accessing the `wallet1` screen on the
tmux simnet window, then running:

```shell
$ ./ctl sendtoaddress Ssbs62a2SPtLcX3BvPKk35R72dVtMSqQYpV 1
```
