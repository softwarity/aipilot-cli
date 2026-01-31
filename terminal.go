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

	if err := d.resizePTY(uint16(height), uint16(width)); err == nil {
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

// writeToPTY writes data to the PTY with proper synchronization.
// Returns the number of bytes written and any error.
func (d *Daemon) writeToPTY(data []byte) (int, error) {
	d.mu.RLock()
	ptmx := d.ptmx
	d.mu.RUnlock()

	if ptmx == nil {
		return 0, nil
	}

	d.ptyMu.Lock()
	n, err := ptmx.Write(data)
	d.ptyMu.Unlock()
	return n, err
}

// readFromPTY reads data from the PTY with proper synchronization.
// Returns the number of bytes read and any error.
func (d *Daemon) readFromPTY(buf []byte) (int, error) {
	d.mu.RLock()
	ptmx := d.ptmx
	d.mu.RUnlock()

	if ptmx == nil {
		return 0, nil
	}

	d.ptyMu.Lock()
	n, err := ptmx.Read(buf)
	d.ptyMu.Unlock()
	return n, err
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
	err := pty.Setsize(ptmx, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	d.ptyMu.Unlock()
	return err
}

// getPTYSize returns the current PTY size with proper synchronization.
// Returns cols, rows, and any error.
func (d *Daemon) getPTYSize() (cols uint16, rows uint16, err error) {
	d.mu.RLock()
	ptmx := d.ptmx
	d.mu.RUnlock()

	if ptmx == nil {
		return 0, 0, nil
	}

	d.ptyMu.Lock()
	size, err := pty.GetsizeFull(ptmx)
	d.ptyMu.Unlock()

	if err != nil {
		return 0, 0, err
	}
	return size.Cols, size.Rows, nil
}
