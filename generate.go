//go:build windows

package main

//go:generate go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@fc50c1956fff0b5bb87215d44b7f73c06996d8e9 -64 -manifest app.manifest -o rsrc.syso
