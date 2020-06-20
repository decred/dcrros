package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrros/internal/version"
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

const (
	defaultConfigFilename = "dcrros.conf"
	defaultLogLevel       = "info"
	defaultActiveNet      = cnMainNet
	defaultBindAddr       = ":8088"
)

var (
	defaultConfigDir  = dcrutil.AppDataDir("dcrros", false)
	defaultDataDir    = filepath.Join(defaultConfigDir, "data")
	defaultLogDir     = filepath.Join(defaultConfigDir, "logs", string(defaultActiveNet))
	defaultConfigFile = filepath.Join(defaultConfigDir, defaultConfigFilename)

	errCmdDone = errors.New("cmd is done while parsing config options")
)

type config struct {
	ShowVersion bool `short:"V" long:"version" description:"Display version information and exit"`

	ConfigFile string   `short:"C" long:"configfile" description:"Path to configuration file"`
	Listeners  []string `long:"listen" description:"Add an interface/port to listen for connections (default all interfaces port: 9128, testnet: 19128, simnet: 29128)"`

	// Network
	MainNet bool `long:"mainnet" description:"Use the main network"`
	TestNet bool `long:"testnet" description:"Use the test network"`
	SimNet  bool `long:"simnet" description:"Use the simulation test network"`

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

func loadConfig() (*config, []string, error) {
	// Default config.
	cfg := config{}

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

	// If the config file path has not been modified by user, then
	// we'll use the default config file path.
	if preCfg.ConfigFile == "" {
		preCfg.ConfigFile = defaultConfigFile
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

	// Add the default listener if none were specified. The default
	// listener is all addresses on the listen port for the network we are
	// to connect to.
	if len(cfg.Listeners) == 0 {
		cfg.Listeners = []string{
			net.JoinHostPort("", cfg.activeNet.defaultListenPort()),
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
