module decred.org/dcrros

go 1.18

require (
	decred.org/dcrwallet/v2 v2.0.0
	github.com/coinbase/rosetta-sdk-go v0.7.2
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/blockchain/stake/v5 v5.0.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.3
	github.com/decred/dcrd/chaincfg/v3 v3.1.1
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.1.0
	github.com/decred/dcrd/dcrjson/v4 v4.0.0
	github.com/decred/dcrd/dcrutil/v4 v4.0.0
	github.com/decred/dcrd/lru v1.1.1
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.0.0
	github.com/decred/dcrd/rpcclient/v8 v8.0.0
	github.com/decred/dcrd/txscript/v4 v4.0.0
	github.com/decred/dcrd/wire v1.5.0
	github.com/decred/dcrtest/dcrdtest v0.0.0-20221125134442-85f922c0880e
	github.com/decred/slog v1.2.0
	github.com/dgraph-io/badger/v2 v2.2007.4
	github.com/jessevdk/go-flags v1.5.0
	github.com/jrick/logrotate v1.0.0
	github.com/jrick/wsrpc/v2 v2.3.4
	github.com/matheusd/middlelogger v0.1.1
	github.com/stretchr/testify v1.7.0
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
)

require (
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/dchest/siphash v1.2.2 // indirect
	github.com/decred/base58 v1.0.4 // indirect
	github.com/decred/dcrd v1.2.1-0.20221123192607-fc017ce3bb3b // indirect
	github.com/decred/dcrd/addrmgr/v2 v2.0.0 // indirect
	github.com/decred/dcrd/bech32 v1.1.2 // indirect
	github.com/decred/dcrd/blockchain/standalone/v2 v2.1.0 // indirect
	github.com/decred/dcrd/certgen v1.1.1 // indirect
	github.com/decred/dcrd/connmgr/v3 v3.1.0 // indirect
	github.com/decred/dcrd/container/apbf v1.0.0 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.0 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.1 // indirect
	github.com/decred/dcrd/database/v3 v3.0.0 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.2 // indirect
	github.com/decred/dcrd/gcs/v4 v4.0.0 // indirect
	github.com/decred/dcrd/hdkeychain/v3 v3.1.0 // indirect
	github.com/decred/dcrd/math/uint256 v1.0.0 // indirect
	github.com/decred/dcrd/peer/v3 v3.0.0 // indirect
	github.com/decred/dcrd/rpc/jsonrpc/types/v3 v3.0.0 // indirect
	github.com/decred/go-socks v1.1.0 // indirect
	github.com/dgraph-io/ristretto v0.0.3 // indirect
	github.com/dgryski/go-farm v0.0.0-20200201041132-a6ae2369ad13 // indirect
	github.com/dustin/go-humanize v1.0.0 // indirect
	github.com/golang/protobuf v1.4.3 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/gorilla/mux v1.8.0 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/jrick/bitset v1.0.0 // indirect
	github.com/klauspost/compress v1.12.3 // indirect
	github.com/mitchellh/mapstructure v1.4.3 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7 // indirect
	golang.org/x/net v0.0.0-20210805182204-aaa1db679c0d // indirect
	golang.org/x/sys v0.0.0-20211216021012-1d35b9e2eb4e // indirect
	google.golang.org/protobuf v1.25.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c // indirect
)

replace (
	github.com/decred/dcrd/blockchain/stake/v5 => github.com/decred/dcrd/blockchain/stake/v5 v5.0.0-20221022042529-0a0cc3b3bf92
	github.com/decred/dcrd/blockchain/v5 => github.com/decred/dcrd/blockchain/v5 v5.0.0-20221022042529-0a0cc3b3bf92
	github.com/decred/dcrd/gcs/v4 => github.com/decred/dcrd/gcs/v4 v4.0.0-20221022042529-0a0cc3b3bf92
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 => github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.0.0-20221022042529-0a0cc3b3bf92
	github.com/decred/dcrd/rpcclient/v8 => github.com/decred/dcrd/rpcclient/v8 v8.0.0-20221022042529-0a0cc3b3bf92
)
