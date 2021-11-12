# Decred/Rosetta middleware service

[![ISC License](https://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)
[![ISC License](https://img.shields.io/badge/rosetta-1.4.10-blue.svg)](https://rosetta-api.org)
[![ISC License](https://img.shields.io/badge/rosetta-cli-0.7.2-blue.svg)](https://github.com/coinbase/rosetta-cli)

`dcrros` (Decred/Rosetta) is a middleware service that provides access to the [Decred](https://www.decred.org) network via a [Rosetta-compatible](https://rosetta-api.org) API.

This version is currently compatible to Rosetta version **1.4.10** and passes
the `check:data` and `check:construction` validations done by the `rosetta-cli`
tool version **0.7.2**.

`dcrros` works as an API conversion layer and cache for the data required by Rosetta implementations. It requires a running `dcrd` node to use for authoritative blockchain data. For technical information about the mapping between Decred and Rosetta concepts, please see the [mapping](/docs/mapping.md) document.

# Running via Docker

The recommended way to run `dcrros` as specified by the Rosetta documentation, is by running via [Docker](https://docker.com).

The [Dockerfile](/Dockerfile) contained in this repository contains everything needed to run a `dcrros` container along with an embedded `dcrd`. The simplest command to build and run a mainnet instance using the current stable version and without any requirements in the host OS other than Docker is to use:

```shell
$ docker build --tag dcrros:stable https://raw.githubusercontent.com/decred/dcrros/master/Dockerfile
$ docker run --rm -p 9128:9128/tcp dcrros:stable
```

If the port of the `dcrros` service is exposed (like in the previous command), the [rosetta-cli](https://github.com/coinbase/rosetta-cli) tool can be used to perform some simple checks on the service, such as (note the use of an appropriate `--configuration-file`):

```shell
$ rosetta-cli --configuration-file docs/roscli-mainnet.json view:block 0
```
 
For additional configuration options, including how to run on the test network, please see the [Docker Options](/docs/docker.md) document.

# Developing

`dcrros` is currently developed on go version 1.16+. It follows the same conventions as other Decred tools, such as [dcrd](https://github.com/decred/dcrd) and [dcrwallet](https://github.com/decred/dcrwallet).

A guide for using simnet for development purposes is available in
[docs/simnet-development.md](/docs/simnet-development.md).

# E2E Testing

An End-to-End test suite, including running the Rosetta CLI's `check:data` and `check:construction` tests is included in [/internal/e2etest](/internal/e2etest). This test suite runs on a local simnet instance that exercises a large number of common on-chain operations found in the Decred chain.

The tests can also be run via Docker by using the included [Dockerfile.e2e](/Dockerfile.e2e):

```shell
$ docker build --tag dcrros:e2e -f Dockerfile.e2e .
$ docker run --rm dcrros:e2e
```

# License

`dcrros` is licensed under the [copyfree](http://copyfree.org) ISC License.
