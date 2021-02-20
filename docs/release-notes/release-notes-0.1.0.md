# dcrros v0.1.0

This is the first official release for dcrros. It is compatible with and embeds
version [1.6.0](https://github.com/decred/decred-binaries/releases/tag/v1.6.0)
of the core Decred software.

While `dcrros` has an extensive suite of tests, please note this product hasn't
seen wide deployment in production yet, therefore please exercise caution and
perform further checks to ensure it's meeting technical requirements of any
application.

## Features

This first release includes the following features:

- Support of Rosetta Specification version [1.4.10](https://www.rosetta-api.org/versions.html)
- Compatible with Decred core software version [1.6.0](https://github.com/decred/decred-binaries/releases/tag/v1.6.0)
- Full support for the Data API (including historical balances)
- Full support for the Construction API (including offline capabilities)
- Ability for dcrros to run and control a dcrd instance
- Docker deployment of the stable version
- E2E test infrastructure, including running `rosetta-cli` checks

## Running

The recommended way of running `dcrros`, according to Rosetta specs, is through
docker:

```shell
$ docker build --tag dcrros:stable https://raw.githubusercontent.com/decred/dcrros/v0.1.0/Dockerfile
$ docker run --rm -p 9128:9128/tcp dcrros:stable
```

For additional information on configuration options when running `dcrros` via
docker, please see the [Docker Options](/docs/docker.md) document.
