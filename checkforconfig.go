package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/skratchdot/open-golang/open"
)

func checkForConfig() {

	stat, err := os.Stat(confPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		Log.Error("Unable to access configuration file '%s': %s", confPath, err.Error())
		exit(1)
	}
	if err == nil && stat.IsDir() {
		Log.Error("Configuration file '%s' is a directory", confPath)
		exit(1)
	}

	if errors.Is(err, os.ErrNotExist) {

		fmt.Println()
		Log.Warn("Configuration file '%s' not found. Creating configuration file ...", confPath)
		defaultConfig := []byte(defaultConfig())
		if err := os.WriteFile(confPath, defaultConfig, 0644); err != nil {
			Log.Error("Error creating configuration file: %s", err.Error())
			exit(1)
		} else {
			Log.Succ("Configuration file '%s' successfully created. Please edit default values.", confPath)
			if err := open.Run(confPath); err != nil {
				Log.Warn("Unable to open configuration file: %s", err.Error())
			}
		}

		fmt.Println()
		Log.Info("Registering the 'nzblnk' URL protocol ...")

		registerProtocol()

	}
}
