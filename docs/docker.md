# Running `dcrros` via Docker

The current [Rosetta](https://rosetta-api.org) documentation requires that deployments be packaged as [Docker](https://docker.com) containers, therefore the recommended way for running a deployment `dcrros` instance if through docker.

The [Dockerfile](/Dockerfile) contained in this repository allows operators to deploy a `dcrros` instance that automatically runs a backing `dcrd` instance, such that all services necessary to query the Decred network via this Rosetta middleware are readily available and properly configured.

This main Dockerfile is entirely self-contained and does **not** rely on any other artifacts of the source repository to run. In other words, it's possible to directly download the contents of this file, build the container and run the image from anywhere. Here's an example application of this principle:

```shell
$ docker build --tag dcrros:stable https://raw.githubusercontent.com/decred/dcrros/master/Dockerfile
$ docker run --rm -P dcrros:stable
```

The container will automatically run `dcrros` which will itself run and monitor a `dcrd` instance. This `dcrd` instance will connect to other P2P nodes of the Decred network and perform its full node duties. In the mean time, `dcrros` will create a cache of data in a format more amenable to returning as results of Rosetta queries.


# Docker run options

The rest of this section will discuss available options for configuring the `dcrros` deployment and relevant `dcrd` configuration parameters. For full details on `dcrd`, please see the [dcrd repository](https://github.com/decerd/dcrd).

For brevity, unless otherwise noted we'll assume the image specified by the main Dockerfile has been created with the `dcrros:stable` tag. We'll also omit common options (such as `-P`, `--rm`) when discussing specific configuration alternatives.

## Configuration Precedence

All Decred-built tools follow the same of standard for specifying configuration options. The general order to determine precedence of configuration options is the following (later stages override earlier ones):

1. Built-in defaults inside the binary
2. Config file
3. Command line options

In other words, an option (such as `--dbtype`) specified via the command line overrides one that was loaded by the service while parsing its config file which in turn overrides the built-in default value.

When modifying the config file for persistent configuration, please make sure _not_ to override those values via a command line argument.

## Selecting the Network (testnet, simnet)

The network for a `dcrros` deployment can be selected by simply specifying `--testnet` or `--simnet` when starting the container:

```shell
# For testnet
$ docker run dcrros:stable --testnet

# For simnet
$ docker run dcrros:stable --simnet
``` 

If unspecified, then the container runs on **mainnet** by default.

## Persisting data (volume)

All data for `dcrros` and `dcrd` are saved in the `/data` dir of the container. Therefore, data can be persisted and externally accessed by mounting a volume on that path:

```shell
$ docker run -v /path/to/dcr/data:/data dcrros:stable 
```

Data for the chain is stored in the `dcrd` subdir, while data for the Rosetta middleware is stored in `dcrros`.

## Building the development version

Besides the main Dockerfile which builds the stable version, this repository also contains a [Dockerfile.dev](/Dockerfile.dev) script which can be used to ease testing and development against the master version of the middleware and node.

It can be used in the following way, from inside a cloned copy of this repository:

```shell
$ docker build --tag dcrros:dev -f Dockerfile.dev .
$ docker run --rm -P dcrros:dev
```

## Specifying extra dcrd arguments

The docker build of `dcrros` automatically runs a `dcrd` process with default arguments to enable some remote control of it.

To pass extra arguments to the underlying `dcrd` instance, specify a series of `--dcrdextraarg` parameters when running the container (notice the correct quoting):

```shell
$ docker run --rm dcrros:stable --dcrdextraarg="--memprofile=/data/mem.pprof" --dcrdextraarg="--maxpeers=30"
```

## Interacting with the underlying dcrd

The underlying `dcrd` node can be interacted with by running it with a known rpc user. accessing the container and issuing `dcrctl` commands:

```shell
$ docker run --name dr --rm dcrros:stable --dcrduser=USER --dcrdpass=PASSWORD 

# On a different terminal
$ docker exec -it dr /bin/bash

# Now inside a container shell
$ dcrctl -c /data/dcrd/rpc.cert -u USER -P PASSWORD getinfo
```

Specify `--testnet`/`--simnet` if running on a different network. You may replace `USER` and `PASSWORD` by something else.

## Do not run the underlying dcrd

The `ENTRYPOINT` of the main docker images already instruct `dcrros` to run an underlying `dcrd` instance and control the lifetime of this process.

Advanced users may want to run `dcrros` exclusively and connect it to a different, external node. That can be accomplished by using:

```shell
$ docker run --rm dcrros:stable --rundcrd="" --dcrduser=USER --dcrdpass=PASSWORD --dcrdconnect=dcrd-node.example.com:9292 --dcrdcertpath /path/to/rpc.cert
```
