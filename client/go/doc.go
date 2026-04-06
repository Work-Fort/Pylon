// SPDX-License-Identifier: Apache-2.0

// Package client provides a typed Go client for the Pylon service registry.
// It connects to Pylon's HTTP API to discover WorkFort services at runtime.
//
// Usage:
//
//	c := client.New("http://pylon:18000", serviceToken)
//	svc, err := c.ServiceByName(ctx, "hive")
//	if err != nil { log.Fatal(err) }
//	hiveClient := hive.New(svc.BaseURL, serviceToken)
package client
