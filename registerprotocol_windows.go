package main

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

func registerProtocol() {

	var k registry.Key
	var err error
	prefix := "SOFTWARE\\Classes\\"
	urlScheme := "nzblnk"
	basePath := prefix + urlScheme
	permission := uint32(registry.QUERY_VALUE | registry.SET_VALUE)
	baseKey := registry.CURRENT_USER

	// create key
	k, _, err = registry.CreateKey(baseKey, basePath, permission)

	// set description
	if err == nil {
		err = k.SetStringValue("", appName+" app")
	}
	if err == nil {
		err = k.SetStringValue("URL Protocol", "")
	}

	// set icon
	if err == nil {
		k, _, err = registry.CreateKey(baseKey, basePath+"\\DefaultIcon", permission)
	}
	if err == nil {
		err = k.SetStringValue("", appExec+",1")
	}

	// create tree
	if err == nil {
		_, _, err = registry.CreateKey(baseKey, basePath+"\\shell", permission)
	}
	if err == nil {
		_, _, err = registry.CreateKey(baseKey, basePath+"\\shell\\open", permission)
	}
	if err == nil {
		k, _, err = registry.CreateKey(baseKey, basePath+"\\shell\\open\\command", permission)
	}

	// set open command
	if err == nil {
		cmdString := fmt.Sprintf("\"%s\" \"%%1\"", appExec)
		err = k.SetExpandStringValue("", cmdString)
	}

	if err != nil {
		Log.Error("Unable to register 'nzblnk' URL protocol: %s", err.Error())
		exit(1)
	} else {
		Log.Succ("URL protocol 'nzblnk' successfully registered to '%s'", appExec)
		exit(0)
	}

}
