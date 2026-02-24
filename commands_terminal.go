package main

import (
	"fmt"
	"strings"
	"time"
)

// handleControlMessage processes control messages from mobile
func (d *Daemon) handleControlMessage(msg string) {
	parts := strings.SplitN(msg, ":", 2)
	if len(parts) < 1 {
		return
	}

	cmd := parts[0]
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	switch cmd {
	case "resize":
		d.handleResizeCommand(args)

	case "info-request":
		d.sendCLIInfo()

	case "ssh-setup-key":
		keyParts := strings.SplitN(args, ":", 3)
		if len(keyParts) == 3 {
			username := keyParts[0]
			mobileId := keyParts[1]
			keyBase64 := keyParts[2]
			go d.installSSHKey(username, mobileId, keyBase64)
		}

	case "file-upload":
		fileParts := strings.SplitN(args, ":", 2)
		if len(fileParts) == 2 {
			fileName := fileParts[0]
			fileBase64 := fileParts[1]
			go d.saveUploadedFile(fileName, fileBase64)
		} else {
			d.sendControlMessage("file-upload-result:error:Invalid file upload format")
		}

	case "file-upload-start":
		d.handleChunkedUploadStart(args)

	case "file-upload-chunk":
		d.handleChunkedUploadChunk(args)

	case "file-upload-cancel":
		d.handleChunkedUploadCancel(args)

	case "mobile-info":
		d.handleMobileInfo(args)
	}
}

// handleResizeCommand handles terminal resize from mobile
func (d *Daemon) handleResizeCommand(args string) {
	dims := strings.Split(args, ",")
	if len(dims) == 2 {
		cols := 0
		rows := 0
		fmt.Sscanf(dims[0], "%d", &cols)
		fmt.Sscanf(dims[1], "%d", &rows)
		if cols > 0 && rows > 0 {
			d.mu.Lock()
			d.mobileCols = cols
			d.mobileRows = rows
			currentClient := d.currentClient
			hasPTY := d.ptmx != nil
			d.mu.Unlock()

			// Always apply resize to PTY when in mobile mode or switching to mobile
			if hasPTY {
				if currentClient == "mobile" {
					// Already in mobile mode - just apply the new size
					d.resizePTY(uint16(rows), uint16(cols))
					// Send Ctrl+L to refresh display
					go func() {
						time.Sleep(50 * time.Millisecond)
						d.sendToPTY([]byte{0x0C})
					}()
				} else {
					// Not in mobile mode - switch to mobile
					d.switchToClient("mobile")
				}
			}
		}
	}
}
