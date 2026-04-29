// Package svc adapts the relay run loop to Windows Service Control
// Manager (SCM) semantics. The svc.go file holds the cross-platform
// surface; svc_windows.go and svc_other.go provide the actual entry
// points.
package svc

const ServiceName = "httpssh-relay"
