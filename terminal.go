package main

import (
	"time"
)

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

	// Already on this client
	if d.currentClient == client {
		d.mu.Unlock()
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
		d.mu.Unlock()
		return
	}

	// Check if PTY exists
	hasPTY := d.ptmx != nil
	d.mu.Unlock()

	if !hasPTY {
		return
	}

	// Apply resize using thread-safe method
	if err := d.resizePTY(uint16(rows), uint16(cols)); err == nil {
		d.mu.Lock()
		d.currentClient = client
		d.mu.Unlock()

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
// This method is thread-safe.
func (d *Daemon) sendToPTY(data []byte) {
	d.mu.RLock()
	ptmx := d.ptmx
	d.mu.RUnlock()

	if ptmx != nil {
		d.ptyMu.Lock()
		ptmx.Write(data)
		d.ptyMu.Unlock()
	}
}

// readFromPTY reads data from the PTY.
// Note: No mutex here - Read() is blocking and would deadlock everything.
// PTY reads are only done from one goroutine, so no sync needed.
func (d *Daemon) readFromPTY(buf []byte) (int, error) {
	d.mu.RLock()
	ptmx := d.ptmx
	d.mu.RUnlock()

	if ptmx == nil {
		return 0, nil
	}

	return ptmx.Read(buf)
}

// resizePTY resizes the PTY with proper synchronization.
// Returns any error from the resize operation.
func (d *Daemon) resizePTY(rows, cols uint16) error {
	d.mu.RLock()
	ptmx := d.ptmx
	d.mu.RUnlock()

	if ptmx == nil {
		return nil
	}

	d.ptyMu.Lock()
	err := ptmx.Resize(int(cols), int(rows))
	d.ptyMu.Unlock()
	return err
}
