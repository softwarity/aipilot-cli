package main

import (
	"fmt"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// forceResize forces PTY to match the local terminal size
func (d *Daemon) forceResize() {
	width, height, err := term.GetSize(d.stdinFd)
	if err != nil {
		fmt.Printf("%sError getting terminal size: %v%s\n", red, err, reset)
		return
	}

	d.mu.RLock()
	ptmx := d.ptmx
	d.mu.RUnlock()

	if ptmx != nil {
		pty.Setsize(ptmx, &pty.Winsize{
			Cols: uint16(width),
			Rows: uint16(height),
		})
		fmt.Printf("%sResized to %dx%d%s\n", green, width, height, reset)
	}
}

// schedulePCSwitch schedules a debounced switch to PC mode
func (d *Daemon) schedulePCSwitch() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Already on PC, nothing to do
	if d.currentClient == "pc" {
		return
	}

	// Cancel existing timer if any
	if d.pcSwitchTimer != nil {
		d.pcSwitchTimer.Stop()
	}

	// Schedule switch after 100ms of no input
	d.pcSwitchTimer = time.AfterFunc(100*time.Millisecond, func() {
		d.switchToClient("pc")
	})
}

// switchToClient switches the PTY to the specified client's dimensions
func (d *Daemon) switchToClient(client string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Already on this client
	if d.currentClient == client {
		return
	}

	var cols, rows int
	if client == "mobile" {
		cols, rows = d.mobileCols, d.mobileRows
	} else {
		cols, rows = d.pcCols, d.pcRows
	}

	// Don't switch if we don't have dimensions for target client
	if cols <= 0 || rows <= 0 {
		return
	}

	// Apply resize
	if d.ptmx != nil {
		pty.Setsize(d.ptmx, &pty.Winsize{
			Cols: uint16(cols),
			Rows: uint16(rows),
		})
		d.currentClient = client

		// Send Ctrl+L to force screen redraw only when switching TO mobile
		if client == "mobile" {
			go func() {
				time.Sleep(50 * time.Millisecond)
				d.sendToPTY([]byte{0x0C}) // Ctrl+L
			}()
		}

		// Notify mobile of mode change via control channel
		go func(c string) {
			d.sendControlMessage("mode:" + c)
		}(client)
	}
}

// sendToPTY sends data to the PTY (and thus to Claude)
func (d *Daemon) sendToPTY(data []byte) {
	d.mu.RLock()
	ptmx := d.ptmx
	d.mu.RUnlock()

	if ptmx != nil {
		ptmx.Write(data)
	}
}
