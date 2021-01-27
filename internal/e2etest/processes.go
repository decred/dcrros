// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cleanRootAppData creates and cleans the root data dir for an 2e2 test run.
func cleanRootAppData(rootAppData string) error {
	if _, err := os.Stat(rootAppData); os.IsNotExist(err) {
		err := os.MkdirAll(rootAppData, os.ModePerm)
		if err != nil {
			return err
		}
	}
	paths := []string{
		"main-dcrd",
		"main-dcrd.log",
		"main-dcrw",
		"main-dcrw.log",
		"dcrros-online",
		"dcrros-online.log",
		"dcrros-offline",
		"dcrros-offline.log",
		"rosetta-cli-data",
		"rosetta-cli.json",
		"check-data.log",
		"check-construction.log",
	}
	for _, path := range paths {
		err := os.RemoveAll(filepath.Join(rootAppData, path))
		if err != nil {
			return err
		}
	}
	return nil
}

func getProcVersion(exe, arg string) (string, error) {
	cmd := exec.Command(exe, arg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func procVersions() (dcrd string, dcrw string, dcrros string, roscli string, err error) {
	if dcrd, err = getProcVersion("dcrd", "--version"); err != nil {
		return
	}
	if dcrw, err = getProcVersion("dcrwallet", "--version"); err != nil {
		return
	}
	if dcrros, err = getProcVersion("dcrros", "--version"); err != nil {
		return
	}
	if roscli, err = getProcVersion("rosetta-cli", "version"); err != nil {
		return
	}

	return
}
