package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// uploadCleanupStarted tracks whether the cleanup goroutine has been started
var uploadCleanupStarted bool
var uploadCleanupMu sync.Mutex

// cleanupAbandonedUploads removes uploads that have been inactive for more than 5 minutes
func (d *Daemon) cleanupAbandonedUploads() {
	d.uploadMu.Lock()
	defer d.uploadMu.Unlock()

	if d.chunkedUploads == nil {
		return
	}

	timeout := UploadTimeout
	now := time.Now()
	for uploadId, upload := range d.chunkedUploads {
		if now.Sub(upload.ReceivedAt) > timeout {
			delete(d.chunkedUploads, uploadId)
		}
	}
}

// handleChunkedUploadStart handles start of chunked upload
func (d *Daemon) handleChunkedUploadStart(args string) {
	startParts := strings.SplitN(args, ":", 4)
	if len(startParts) == 4 {
		uploadId := startParts[0]
		fileName := startParts[1]
		totalChunks := 0
		totalSize := int64(0)
		fmt.Sscanf(startParts[2], "%d", &totalChunks)
		fmt.Sscanf(startParts[3], "%d", &totalSize)

		d.uploadMu.Lock()
		if d.chunkedUploads == nil {
			d.chunkedUploads = make(map[string]*ChunkedUpload)
			// Start cleanup goroutine on first upload (lazy initialization)
			uploadCleanupMu.Lock()
			if !uploadCleanupStarted {
				uploadCleanupStarted = true
				go func() {
					ticker := time.NewTicker(UploadCleanupInterval)
					defer ticker.Stop()
					for range ticker.C {
						d.cleanupAbandonedUploads()
					}
				}()
			}
			uploadCleanupMu.Unlock()
		}
		d.chunkedUploads[uploadId] = &ChunkedUpload{
			FileName:    fileName,
			TotalChunks: totalChunks,
			TotalSize:   totalSize,
			Chunks:      make(map[int][]byte),
			ReceivedAt:  time.Now(),
		}
		d.uploadMu.Unlock()

		d.sendControlMessage(fmt.Sprintf("file-upload-ack:%s:started", uploadId))
	}
}

// handleChunkedUploadChunk handles a chunk of upload
func (d *Daemon) handleChunkedUploadChunk(args string) {
	chunkParts := strings.SplitN(args, ":", 3)
	if len(chunkParts) == 3 {
		uploadId := chunkParts[0]
		chunkIndex := 0
		fmt.Sscanf(chunkParts[1], "%d", &chunkIndex)
		chunkBase64 := chunkParts[2]

		chunkData, err := base64.StdEncoding.DecodeString(chunkBase64)
		if err != nil {
			d.sendControlMessage(fmt.Sprintf("file-upload-result:error:Invalid chunk data for %s", uploadId))
			return
		}

		d.uploadMu.Lock()
		upload, exists := d.chunkedUploads[uploadId]
		if !exists {
			d.uploadMu.Unlock()
			d.sendControlMessage(fmt.Sprintf("file-upload-result:error:Unknown upload %s", uploadId))
			return
		}

		upload.Chunks[chunkIndex] = chunkData
		upload.ReceivedAt = time.Now() // Update activity time

		// Check if all chunks received
		if len(upload.Chunks) == upload.TotalChunks {
			var fullData []byte
			for i := 0; i < upload.TotalChunks; i++ {
				if chunk, ok := upload.Chunks[i]; ok {
					fullData = append(fullData, chunk...)
				} else {
					d.uploadMu.Unlock()
					d.sendControlMessage(fmt.Sprintf("file-upload-result:error:Missing chunk %d for %s", i, uploadId))
					return
				}
			}

			fileName := upload.FileName
			delete(d.chunkedUploads, uploadId)
			d.uploadMu.Unlock()

			go d.saveUploadedFileBytes(fileName, fullData)
		} else {
			d.uploadMu.Unlock()
			d.sendControlMessage(fmt.Sprintf("file-upload-ack:%s:%d", uploadId, chunkIndex))
		}
	}
}

// handleChunkedUploadCancel handles cancellation of a chunked upload
func (d *Daemon) handleChunkedUploadCancel(uploadId string) {
	d.uploadMu.Lock()
	_, exists := d.chunkedUploads[uploadId]
	if exists {
		delete(d.chunkedUploads, uploadId)
	}
	d.uploadMu.Unlock()

	if exists {
		d.sendControlMessage(fmt.Sprintf("file-upload-ack:%s:cancelled", uploadId))
	}
}

// saveUploadedFile saves a base64-encoded file to /tmp
func (d *Daemon) saveUploadedFile(fileName, fileBase64 string) {
	fileData, err := base64.StdEncoding.DecodeString(fileBase64)
	if err != nil {
		d.sendControlMessage("file-upload-result:error:Invalid file encoding")
		return
	}

	d.saveUploadedFileBytes(fileName, fileData)
}

// saveUploadedFileBytes saves file bytes to /tmp
func (d *Daemon) saveUploadedFileBytes(fileName string, fileData []byte) {
	fileName = filepath.Base(fileName)
	if fileName == "" || fileName == "." || fileName == ".." {
		d.sendControlMessage("file-upload-result:error:Invalid filename")
		return
	}

	timestamp := time.Now().UnixMilli()
	remotePath := filepath.Join(os.TempDir(), fmt.Sprintf("aipilot_%d_%s", timestamp, fileName))

	if err := os.WriteFile(remotePath, fileData, FilePermissions); err != nil {
		d.sendControlMessage(fmt.Sprintf("file-upload-result:error:Failed to write file: %v", err))
		return
	}

	d.sendControlMessage(fmt.Sprintf("file-upload-result:success:%s", remotePath))

	// Auto-insert file reference based on agent type
	d.insertFileReference(remotePath)
}

// insertFileReference inserts a file reference into the PTY based on agent type
func (d *Daemon) insertFileReference(filePath string) {
	var insertCmd string

	switch d.agentType {
	case AgentGemini:
		// Gemini: use @ prefix to reference files
		insertCmd = fmt.Sprintf("@%s ", filePath)
	case AgentCodex:
		// Codex: use /mention command
		insertCmd = fmt.Sprintf("/mention %s ", filePath)
	default:
		// Claude and others: just output the path
		insertCmd = filePath + " "
	}

	d.sendToPTY([]byte(insertCmd))
}
