# dcrros End-to-End Test Suite

This test suite aims to setup a sample Decred simnet chain exercising most of
of the actual observable effects of the mainnet chain, create a set of
dcrros instances and perform the rosetta-cli `check:data` and
`check:construction` set of tests.

It requires compatible versions of `dcrd`, `dcrwallet`, `dcrros` and
`rosetta-cli` to be installed in the environment.

## Usage

```shell
$ cd internal/e2etest
go run .
```

