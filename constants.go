package main

import "time"

// File and directory permissions
const (
	// FilePermissions is the standard permission for config files (owner read/write only)
	FilePermissions = 0600
	// DirPermissions is the standard permission for config directories (owner rwx only)
	DirPermissions = 0700
)

// Buffer sizes
const (
	// BufferSize is the standard buffer size for PTY I/O
	BufferSize = 4096
)

// SSH defaults
const (
	// DefaultSSHPort is the standard SSH port
	DefaultSSHPort = 22
	// AlternativeSSHPort is a common alternative SSH port
	AlternativeSSHPort = 2222
)

// Timeout constants
const (
	// UploadTimeout is the maximum time to wait for a file upload
	UploadTimeout = 5 * time.Minute
	// PairingTimeout is the maximum time to wait for pairing completion
	PairingTimeout = 5 * time.Minute
	// HTTPClientTimeout is the timeout for HTTP requests to the relay
	HTTPClientTimeout = 30 * time.Second
	// ReconnectDelay is the delay between WebSocket reconnection attempts
	ReconnectDelay = 2 * time.Second
	// RelayConnectDelay is the delay between relay connection retries
	RelayConnectDelay = 5 * time.Second
	// PingInterval is the interval for WebSocket keepalive pings
	PingInterval = 10 * time.Second
	// PairingPollInterval is the interval for polling pairing status
	PairingPollInterval = 2 * time.Second
	// UploadCleanupInterval is the interval for cleaning up abandoned uploads
	UploadCleanupInterval = 1 * time.Minute
	// SSHConnectTimeout is the timeout for SSH connection checks
	SSHConnectTimeout = time.Second
	// SSHQuickCheckTimeout is the timeout for quick SSH availability checks
	SSHQuickCheckTimeout = 500 * time.Millisecond
)
