// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"decred.org/dcrros/backend"
	"decred.org/dcrros/internal/version"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/rpcclient/v6"
	"github.com/decred/slog"
	"github.com/jessevdk/go-flags"
)

type chainNetwork string

const (
	cnMainNet chainNetwork = "mainnet"
	cnTestNet chainNetwork = "testnet"
	cnSimNet  chainNetwork = "simnet"
)

// defaultListenPort is the default port to use with net.JoinHostPort().
func (c chainNetwork) defaultListenPort() string {
	switch c {
	case cnMainNet:
		return "9128"
	case cnTestNet:
		return "19128"
	case cnSimNet:
		return "29128"
	default:
		panic("unknown chainNetwork")
	}
}

// defaultDcrdCfg returns the default rpc connect address for the given
// network.
func (c chainNetwork) defaultDcrdRPCConnect() string {
	switch c {
	case cnMainNet:
		return "localhost:9109"
	case cnTestNet:
		return "localhost:19109"
	case cnSimNet:
		return "localhost:19556"
	default:
		panic("unknown chainNetwork")
	}
}

const (
	defaultConfigFilename = "dcrros.conf"
	defaultLogLevel       = "info"
	defaultActiveNet      = cnMainNet
	defaultDBType         = backend.DBTypeBadger
	defaultDataDirname    = "data"
	defaultLogDirname     = "logs"

	defaultCacheSizeBlocks = 100
	defaultCacheSizeRawTxs = 250
)

var (
	defaultConfigDir    = dcrutil.AppDataDir("dcrros", false)
	defaultDataDir      = filepath.Join(defaultConfigDir, defaultDataDirname)
	defaultLogDir       = filepath.Join(defaultConfigDir, defaultLogDirname, string(defaultActiveNet))
	defaultConfigFile   = filepath.Join(defaultConfigDir, defaultConfigFilename)
	defaultDcrdDir      = dcrutil.AppDataDir("dcrd", false)
	defaultDcrdCertPath = filepath.Join(defaultDcrdDir, "rpc.cert")

	errCmdDone = errors.New("cmd is done while parsing config options")
)

type config struct {
	ShowVersion bool `short:"V" long:"version" description:"Display version information and exit"`

	// General Config

	AppData    string `short:"A" long:"appdata" description:"Path to application home directory"`
	ConfigFile string `short:"C" long:"configfile" description:"Path to configuration file"`
	DebugLevel string `short:"d" long:"debuglevel" description:"Logging level for all subsystems {trace, debug, info, warn, error, critical} -- You may also specify <subsystem>=<level>,<subsystem2>=<level>,... to set the log level for individual subsystems -- Use show to list available subsystems"`

	// Network

	MainNet bool `long:"mainnet" description:"Use the main network"`
	TestNet bool `long:"testnet" description:"Use the test network"`
	SimNet  bool `long:"simnet" description:"Use the simulation test network"`

	// Dcrd Connection Options

	DcrdConnect   string `short:"c" long:"dcrdconnect" description:"Network address of the RPC interface of the dcrd node to connect to (default: localhost port 9109, testnet: 19109, simnet: 19556)"`
	DcrdCertPath  string `long:"dcrdcertpath" description:"File path location of the dcrd RPC certificate"`
	DcrdCertBytes string `long:"dcrdcertbytes" description:"The pem-encoded RPC certificate for dcrd"`
	DcrdUser      string `short:"u" long:"dcrduser" description:"RPC username to authenticate with dcrd"`
	DcrdPass      string `short:"P" long:"dcrdpass" description:"RPC password to authenticate with dcrd"`

	// Listeners

	Listeners []string `long:"listen" description:"Add an interface/port to listen for connections (default all interfaces port: 9128, testnet: 19128, simnet: 29128)"`
	Profile   string   `long:"profile" description:"Enable HTTP profiling on given [addr:]port -- NOTE port must be between 1024 and 65536"`

	// Embedded dcrd

	RunDcrd       string   `long:"rundcrd" description:"Run the given dcrd binary and terminate dcrros if dcrd is killed"`
	DcrdExtraArgs []string `long:"dcrdextraarg" description:"Extra arguments to provide to dcrd when running it"`
	// Tuning

	DBType          string `long:"dbtype" description:"Database backend to use for the Block Chain"`
	CacheSizeBlocks uint   `long:"cachesizeblocks" description:"Number of blocks to hold in the in-memory block cache"`
	CacheSizeRawTxs uint   `long:"cachesizerawtxs" description:"Number of txs to hold in the in-memory tx cache"`

	// The rest of the members of this struct are filled by loadConfig().

	activeNet chainNetwork
}

// listeners returns the interface listeners where connections to the http
// server should be accepted.
func (c *config) listeners() ([]net.Listener, error) {
	var list []net.Listener
	for _, addr := range c.Listeners {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			// Cancel listening on the other addresses since we'll
			// return an error.
			for _, l := range list {
				// Ignore close errors since we'll be returning
				// an error anyway.
				l.Close()
			}
			return nil, fmt.Errorf("unable to listen on %s: %v", addr, err)
		}
		list = append(list, l)
	}
	return list, nil
}

func (c *config) dcrdConnConfig() (*rpcclient.ConnConfig, error) {
	// Load the appropriate dcrd rpc.cert file.
	if len(c.DcrdCertBytes) == 0 && c.DcrdCertPath != "" {
		f, err := ioutil.ReadFile(c.DcrdCertPath)
		if err != nil {
			return nil, fmt.Errorf("unable to load dcrd cert "+
				"file: %v", err)
		}
		c.DcrdCertBytes = string(f)
	}

	return &rpcclient.ConnConfig{
		Host:         c.DcrdConnect,
		Endpoint:     "ws",
		User:         c.DcrdUser,
		Pass:         c.DcrdPass,
		Certificates: []byte(c.DcrdCertBytes),
	}, nil
}

func (c *config) dcrdArgs() []string {
	args := []string{
		"-u=" + c.DcrdUser,
		"-P=" + c.DcrdPass,
		"--txindex",
	}
	if c.TestNet {
		args = append(args, "--testnet")
	}
	if c.SimNet {
		args = append(args, "--simnet")
	}

	args = append(args, c.DcrdExtraArgs...)

	return args
}

func (c *config) serverConfig() (*backend.ServerConfig, error) {
	var chain *chaincfg.Params
	switch c.activeNet {
	case cnMainNet:
		chain = chaincfg.MainNetParams()
	case cnTestNet:
		chain = chaincfg.TestNet3Params()
	case cnSimNet:
		chain = chaincfg.SimNetParams()
	default:
		return nil, fmt.Errorf("unknown active net: %s", c.activeNet)
	}

	dbType := backend.DBType(c.DBType)
	supported := backend.SupportedDBTypes()
	found := false
	for _, sup := range supported {
		if sup == dbType {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("unknown db type: %s (supports %v)",
			dbType, supported)
	}
	dbDir := filepath.Join(defaultDataDir, string(c.activeNet), "db")

	if dbType == backend.DBTypeBadger {
		err := os.MkdirAll(dbDir, 0700)
		if err != nil {
			// Show a nicer error message if it's because a symlink is
			// linked to a directory that does not exist (probably because
			// it's not mounted).
			if e, ok := err.(*os.PathError); ok && os.IsExist(err) {
				if link, lerr := os.Readlink(e.Path); lerr == nil {
					str := "is symlink %s -> %s mounted?"
					err = fmt.Errorf(str, e.Path, link)
				}
			}

			return nil, fmt.Errorf("failed to create db dir: %v", err)
		}
	}

	dcrdCfg, err := c.dcrdConnConfig()
	if err != nil {
		return nil, err
	}
	return &backend.ServerConfig{
		ChainParams:     chain,
		DcrdCfg:         dcrdCfg,
		DBType:          dbType,
		DBDir:           dbDir,
		CacheSizeBlocks: c.CacheSizeBlocks,
		CacheSizeRawTxs: c.CacheSizeRawTxs,
	}, nil
}

// cleanAndExpandPath expands environment variables and leading ~ in the passed
// path, cleans the result, and returns it.
func cleanAndExpandPath(path string) string {
	// Nothing to do when no path is given.
	if path == "" {
		return path
	}

	// NOTE: The os.ExpandEnv doesn't work with Windows cmd.exe-style
	// %VARIABLE%, but the variables can still be expanded via POSIX-style
	// $VARIABLE.
	path = os.ExpandEnv(path)

	if !strings.HasPrefix(path, "~") {
		return filepath.Clean(path)
	}

	// Expand initial ~ to the current user's home directory, or ~otheruser
	// to otheruser's home directory.  On Windows, both forward and backward
	// slashes can be used.
	path = path[1:]

	var pathSeparators string
	if runtime.GOOS == "windows" {
		pathSeparators = string(os.PathSeparator) + "/"
	} else {
		pathSeparators = string(os.PathSeparator)
	}

	userName := ""
	if i := strings.IndexAny(path, pathSeparators); i != -1 {
		userName = path[:i]
		path = path[i:]
	}

	homeDir := ""
	var u *user.User
	var err error
	if userName == "" {
		u, err = user.Current()
	} else {
		u, err = user.Lookup(userName)
	}
	if err == nil {
		homeDir = u.HomeDir
	}
	// Fallback to CWD if user lookup fails or user has no home directory.
	if homeDir == "" {
		homeDir = "."
	}

	return filepath.Join(homeDir, path)
}

// validLogLevel returns whether or not logLevel is a valid debug log level.
func validLogLevel(logLevel string) bool {
	_, ok := slog.LevelFromString(logLevel)
	return ok
}

// supportedSubsystems returns a sorted slice of the supported subsystems for
// logging purposes.
func supportedSubsystems() []string {
	// Convert the subsystemLoggers map keys to a slice.
	subsystems := make([]string, 0, len(subsystemLoggers))
	for subsysID := range subsystemLoggers {
		subsystems = append(subsystems, subsysID)
	}

	// Sort the subsystems for stable display.
	sort.Strings(subsystems)
	return subsystems
}

// parseAndSetDebugLevels attempts to parse the specified debug level and set
// the levels accordingly.  An appropriate error is returned if anything is
// invalid.
func parseAndSetDebugLevels(debugLevel string) error {
	// When the specified string doesn't have any delimiters, treat it as
	// the log level for all subsystems.
	if !strings.Contains(debugLevel, ",") && !strings.Contains(debugLevel, "=") {
		// Validate debug log level.
		if !validLogLevel(debugLevel) {
			str := "the specified debug level [%v] is invalid"
			return fmt.Errorf(str, debugLevel)
		}

		// Change the logging level for all subsystems.
		setLogLevels(debugLevel)

		return nil
	}

	// Split the specified string into subsystem/level pairs while detecting
	// issues and update the log levels accordingly.
	for _, logLevelPair := range strings.Split(debugLevel, ",") {
		if !strings.Contains(logLevelPair, "=") {
			str := "the specified debug level contains an invalid " +
				"subsystem/level pair [%v]"
			return fmt.Errorf(str, logLevelPair)
		}

		// Extract the specified subsystem and log level.
		fields := strings.Split(logLevelPair, "=")
		subsysID, logLevel := fields[0], fields[1]

		// Validate subsystem.
		if _, exists := subsystemLoggers[subsysID]; !exists {
			str := "the specified subsystem [%v] is invalid -- " +
				"supported subsystems %v"
			return fmt.Errorf(str, subsysID, supportedSubsystems())
		}

		// Validate log level.
		if !validLogLevel(logLevel) {
			str := "the specified debug level [%v] is invalid"
			return fmt.Errorf(str, logLevel)
		}

		setLogLevel(subsysID, logLevel)
	}

	return nil
}

func randString() string {
	bts := make([]byte, 20)
	if _, err := rand.Read(bts); err != nil {
		panic("unable to read random values")
	}
	return base64.StdEncoding.EncodeToString(bts)
}

func loadConfig() (*config, []string, error) {
	// Default config.
	cfg := config{
		ConfigFile:      defaultConfigFile,
		DcrdCertPath:    defaultDcrdCertPath,
		DebugLevel:      defaultLogLevel,
		DBType:          string(defaultDBType),
		CacheSizeBlocks: defaultCacheSizeBlocks,
		CacheSizeRawTxs: defaultCacheSizeRawTxs,
	}

	// Pre-parse the command line options to see if an alternative config
	// file was specified.  Any errors aside from the
	// help message error can be ignored here since they will be caught by
	// the final parse below.
	preCfg := cfg
	preParser := flags.NewParser(&preCfg, flags.HelpFlag)
	_, err := preParser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); ok && e.Type == flags.ErrHelp {
			fmt.Fprintln(os.Stderr, err)
			return nil, nil, errCmdDone
		}
	}

	// Show the version and exit if the version flag was specified.
	appName := filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	usageMessage := fmt.Sprintf("Use %s -h to show usage", appName)
	if preCfg.ShowVersion {
		fmt.Printf("%s version %s (Go version %s %s/%s)\n",
			appName, version.String(),
			runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return nil, nil, errCmdDone
	}

	// Special show command to list supported subsystems and exit.
	if preCfg.DebugLevel == "show" {
		fmt.Println("Supported subsystems", supportedSubsystems())
		return nil, nil, errCmdDone
	}

	// Update the home directory for dcrros if specified. Since the home
	// directory is updated, other variables need to be updated to reflect
	// the new changes.
	if preCfg.AppData != "" {
		cfg.AppData, _ = filepath.Abs(cleanAndExpandPath(preCfg.AppData))

		if preCfg.ConfigFile == defaultConfigFile {
			defaultConfigFile = filepath.Join(cfg.AppData,
				defaultConfigFilename)
			preCfg.ConfigFile = defaultConfigFile
			cfg.ConfigFile = defaultConfigFile
		} else {
			cfg.ConfigFile = preCfg.ConfigFile
		}
		defaultDataDir = filepath.Join(cfg.AppData, defaultDataDirname)
		defaultLogDir = filepath.Join(cfg.AppData, defaultLogDirname, string(defaultActiveNet))
	}

	// Load additional config from file.
	var configFileError error
	parser := flags.NewParser(&cfg, flags.Default)

	err = flags.NewIniParser(parser).ParseFile(preCfg.ConfigFile)
	if err != nil {
		if _, ok := err.(*os.PathError); !ok {
			fmt.Fprintf(os.Stderr, "Error parsing config "+
				"file: %v\n", err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return nil, nil, err
		}
		configFileError = err
	}

	// Parse command line options again to ensure they take precedence.
	remainingArgs, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			fmt.Fprintln(os.Stderr, usageMessage)
		}
		return nil, nil, err
	}

	// Create the home directory if it doesn't already exist.
	funcName := "loadConfig"
	err = os.MkdirAll(defaultDataDir, 0700)
	if err != nil {
		// Show a nicer error message if it's because a symlink is
		// linked to a directory that does not exist (probably because
		// it's not mounted).
		if e, ok := err.(*os.PathError); ok && os.IsExist(err) {
			if link, lerr := os.Readlink(e.Path); lerr == nil {
				str := "is symlink %s -> %s mounted?"
				err = fmt.Errorf(str, e.Path, link)
			}
		}

		str := "%s: Failed to create home directory: %v"
		err := fmt.Errorf(str, funcName, err)
		fmt.Fprintln(os.Stderr, err)
		return nil, nil, err
	}

	// Multiple networks can't be selected simultaneously.  Count number of
	// network flags passed and assign active network params.
	numNets := 0
	cfg.activeNet = defaultActiveNet
	if cfg.MainNet {
		numNets++
		cfg.activeNet = cnMainNet
	}
	if cfg.TestNet {
		numNets++
		cfg.activeNet = cnTestNet
	}
	if cfg.SimNet {
		numNets++
		cfg.activeNet = cnSimNet
	}
	if numNets > 1 {
		str := "%s: mainnet, testnet and simnet params can't be " +
			"used together -- choose one of the three"
		err := fmt.Errorf(str, funcName)
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Initialize log rotation.  After log rotation has been initialized,
	// the logger variables may be used.
	logDir := strings.Replace(defaultLogDir, string(defaultActiveNet),
		string(cfg.activeNet), 1)
	logPath := filepath.Join(logDir, "dcrros.log")
	initLogRotator(logPath)
	setLogLevels(defaultLogLevel)

	// Parse, validate, and set debug log level(s).
	if err := parseAndSetDebugLevels(cfg.DebugLevel); err != nil {
		err := fmt.Errorf("%s: %v", funcName, err.Error())
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, usageMessage)
		return nil, nil, err
	}

	// Add the default listener if none were specified. The default
	// listener is all addresses on the listen port for the network we are
	// to connect to.
	if len(cfg.Listeners) == 0 {
		cfg.Listeners = []string{
			net.JoinHostPort("", cfg.activeNet.defaultListenPort()),
		}
	}

	// Validate format of profile, can be an address:port, or just a port.
	if cfg.Profile != "" {
		// If profile is just a number, then add a default host of
		// "127.0.0.1" such that Profile is a valid tcp address.
		if _, err := strconv.Atoi(cfg.Profile); err == nil {
			cfg.Profile = net.JoinHostPort("127.0.0.1", cfg.Profile)
		}

		// Check the Profile is a valid address.
		_, portStr, err := net.SplitHostPort(cfg.Profile)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid profile host/port: %v", err)
		}

		// Finally, check the port is in range.
		if port, _ := strconv.Atoi(portStr); port < 1024 || port > 65535 {
			return nil, nil, fmt.Errorf("profile address %s: port "+
				"must be between 1024 and 65535", cfg.Profile)
		}
	}

	// When using --rundcrd the user shouldn't specify a connection option
	// as we'll specify one for them.
	if cfg.DcrdConnect != "" && cfg.RunDcrd != "" {
		return nil, nil, fmt.Errorf("cannot use both --dcrdconnect and " +
			"--rundcrd at the same time")
	}

	// Determine the default dcrd connect address based on the selected
	// network.
	if cfg.DcrdConnect == "" {
		cfg.DcrdConnect = cfg.activeNet.defaultDcrdRPCConnect()
	}

	// When running dcrd ourselves, define a user and password for the rpc
	// interface.
	if cfg.RunDcrd != "" {
		if cfg.DcrdUser == "" {
			cfg.DcrdUser = randString()
		}
		if cfg.DcrdPass == "" {
			cfg.DcrdPass = randString()
		}
	}

	// Warn about missing config file only after all other configuration is
	// done.  This prevents the warning on help messages and invalid
	// options.  Note this should go directly before the return.
	if configFileError != nil {
		log.Warnf("%v", configFileError)
	}

	return &cfg, remainingArgs, nil
}
