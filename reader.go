// Package ttbin provides functionality for reading and processing SITT format timetag files (.ttbin)
package ttbin

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
)

// Constants for ttbin file processing
const (
	TTBinExtension    = "*.ttbin"
	MaxConcurrency    = 4
	DefaultBufferSize = 64 * 1024
	LargeBufferSize   = 1024 * 1024
	MaxSampleEvents   = 1000
	RecordSize        = 10
	SITTHeaderSize    = 16
)

// TimeTag represents a single time tag entry
type TimeTag struct {
	Timestamp uint64
	Channel   uint16
}

// SITTBlockInfo holds information about a SITT block
type SITTBlockInfo struct {
	Position uint64
	Type     uint32
	Length   uint32
}

// Reader provides functionality for reading ttbin files
type Reader struct {
	DataDirectory string
}

// NewReader creates a new ttbin Reader
func NewReader(dataDirectory string) *Reader {
	return &Reader{
		DataDirectory: dataDirectory,
	}
}

// GetTimeTaFiles returns all .ttbin files sorted naturally (numeric-aware)
func (r *Reader) GetTimeTaFiles() ([]string, error) {
	// Check if directory exists
	if _, err := os.Stat(r.DataDirectory); os.IsNotExist(err) {
		return nil, fmt.Errorf("directory '%s' does not exist", r.DataDirectory)
	}

	pattern := filepath.Join(r.DataDirectory, TTBinExtension)
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to search for .ttbin files: %v", err)
	}

	// Use natural sorting to handle numeric parts correctly
	sort.Slice(files, func(i, j int) bool {
		return r.compareNatural(files[i], files[j]) < 0
	})
	return files, nil
}

// compareNatural compares two strings using natural/numeric ordering
// Simplified approach: extract numbers from filenames and compare them numerically
func (r *Reader) compareNatural(a, b string) int {
	// Simple regex to find the first numeric sequence in each filename
	re := regexp.MustCompile(`\.(\d+)\.ttbin$`)

	matchA := re.FindStringSubmatch(a)
	matchB := re.FindStringSubmatch(b)

	// If both have numeric parts, compare numerically
	if len(matchA) > 1 && len(matchB) > 1 {
		numA, errA := strconv.Atoi(matchA[1])
		numB, errB := strconv.Atoi(matchB[1])

		if errA == nil && errB == nil {
			if numA != numB {
				if numA < numB {
					return -1
				}
				return 1
			}
		}
	}

	// Fallback to lexicographical comparison
	if a < b {
		return -1
	} else if a > b {
		return 1
	}
	return 0
}

// ScanAvailableChannels scans files in parallel to discover which channels contain events with progress logging
func (r *Reader) ScanAvailableChannels(files []string) (map[uint16]int, error) {
	totalFiles := len(files)

	fmt.Printf("Scanning %d files to discover available channels (parallel processing)...\n", totalFiles)

	// For small numbers of files, use sequential processing to avoid overhead
	if totalFiles <= 4 {
		return r.scanAvailableChannelsSequential(files)
	}

	// Use parallel processing for larger file sets
	return r.scanAvailableChannelsParallel(files)
}

// scanAvailableChannelsSequential processes files one by one (for small file sets)
func (r *Reader) scanAvailableChannelsSequential(files []string) (map[uint16]int, error) {
	channelCounts := make(map[uint16]int)
	totalFiles := len(files)

	for i, filepath := range files {
		fmt.Printf("Scanning file: %s [%d/%d]\n", filepath, i+1, totalFiles)

		localCounts, err := r.ScanSingleFileOptimized(filepath)
		if err != nil {
			fmt.Printf("  ✗ Error scanning %s: %v\n", filepath, err)
			continue // Skip files with errors
		}

		// Merge results
		channelsFound := 0
		for channel, count := range localCounts {
			channelCounts[channel] += count
			if count > 0 {
				channelsFound++
			}
		}

		if channelsFound > 0 {
			fmt.Printf("  ✓ Found %d channels with data in %s\n", channelsFound, filepath)
		} else {
			fmt.Printf("  ○ No channels found in %s\n", filepath)
		}
	}

	fmt.Printf("Scanning complete: found %d unique channels across all files\n\n", len(channelCounts))
	return channelCounts, nil
}

// scanAvailableChannelsParallel processes files in parallel batches
func (r *Reader) scanAvailableChannelsParallel(files []string) (map[uint16]int, error) {
	channelCounts := make(map[uint16]int)
	totalFiles := len(files)

	// Process files in parallel with limited concurrency
	const maxWorkers = 6 // Optimal for most systems
	semaphore := make(chan struct{}, maxWorkers)
	resultChan := make(chan map[uint16]int, totalFiles)
	errorChan := make(chan error, totalFiles)

	var wg sync.WaitGroup

	// Start workers
	for i, filepath := range files {
		wg.Add(1)
		go func(filePath string, fileIndex int) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			fmt.Printf("Scanning file: %s [%d/%d]\n", filePath, fileIndex+1, totalFiles)

			localCounts, err := r.ScanSingleFileOptimized(filePath)
			if err != nil {
				fmt.Printf("  ✗ Error scanning %s: %v\n", filePath, err)
				errorChan <- err
				return
			}

			// Check if we found channels
			channelsFound := 0
			for _, count := range localCounts {
				if count > 0 {
					channelsFound++
				}
			}

			if channelsFound > 0 {
				fmt.Printf("  ✓ Found %d channels with data in %s\n", channelsFound, filePath)
			} else {
				fmt.Printf("  ○ No channels found in %s\n", filePath)
			}

			resultChan <- localCounts
		}(filepath, i)
	}

	// Close channels when all workers are done
	go func() {
		wg.Wait()
		close(resultChan)
		close(errorChan)
	}()

	// Merge results
	for localCounts := range resultChan {
		for channel, count := range localCounts {
			channelCounts[channel] += count
		}
	}

	fmt.Printf("Scanning complete: found %d unique channels across all files\n\n", len(channelCounts))
	return channelCounts, nil
}

// ProcessFileOptimized processes a single file with streaming and filtering for better performance
func (r *Reader) ProcessFileOptimized(filename string, channelFilter []uint16, resultChan chan<- TimeTag) (int, error) {
	file, err := r.openAndValidateFile(filename)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	allBlocks, err := r.findAllSITTBlocks(file)
	if err != nil {
		return 0, err
	}

	return r.processBlocksInParallel(file, allBlocks, channelFilter, resultChan)
}

// ProcessFileUnfiltered processes a single file extracting ALL time tags without any channel filtering
func (r *Reader) ProcessFileUnfiltered(filename string, resultChan chan<- TimeTag) (int, error) {
	file, err := r.openAndValidateFile(filename)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	// Use the same logic as scanning but read ALL events instead of just a sample
	timeTagBlocks, err := r.findTimeTagBlocks(file)
	if err != nil {
		return 0, err
	}

	if len(timeTagBlocks) == 0 {
		return 0, nil
	}

	return r.processAllEventsInBlocks(file, timeTagBlocks, resultChan)
}

// ProcessFiles processes multiple files sequentially in lexicographical order with channel filtering
func (r *Reader) ProcessFiles(files []string, channelFilter []uint16, resultChan chan<- TimeTag) error {
	var processingErrors []error
	totalFiles := len(files)

	fmt.Printf("Processing %d files in lexicographical order...\n", totalFiles)

	for i, file := range files {
		fmt.Printf("Processing file: %s [%d/%d]\n", file, i+1, totalFiles)
		count, err := r.ProcessFileOptimized(file, channelFilter, resultChan)
		if err != nil {
			processingErrors = append(processingErrors, fmt.Errorf("error reading %s: %v", file, err))
			fmt.Printf("  ✗ Error processing %s: %v\n", file, err)
			continue
		}

		if count > 0 {
			fmt.Printf("  ✓ Extracted %d relevant time tags from %s\n", count, file)
		} else {
			fmt.Printf("  ○ No relevant time tags found in %s\n", file)
		}
	}

	// Close channel when all files are processed
	close(resultChan)

	// Check for processing errors
	if len(processingErrors) > 0 {
		return fmt.Errorf("processing errors occurred: %d files had errors", len(processingErrors))
	}

	return nil
}

// ProcessAllFiles processes multiple files sequentially in lexicographical order without any channel filtering - extracts ALL time tags
func (r *Reader) ProcessAllFiles(files []string, resultChan chan<- TimeTag) error {
	var processingErrors []error
	totalFiles := len(files)

	fmt.Printf("Processing %d files in lexicographical order...\n", totalFiles)

	for i, file := range files {
		fmt.Printf("Processing file: %s [%d/%d]\n", file, i+1, totalFiles)
		count, err := r.ProcessFileUnfiltered(file, resultChan)
		if err != nil {
			processingErrors = append(processingErrors, fmt.Errorf("error reading %s: %v", file, err))
			fmt.Printf("  ✗ Error processing %s: %v\n", file, err)
			continue
		}

		if count > 0 {
			fmt.Printf("  ✓ Extracted %d time tags from %s\n", count, file)
		} else {
			fmt.Printf("  ○ No time tags found in %s\n", file)
		}
	}

	// Close channel when all files are processed
	close(resultChan)

	// Check for processing errors
	if len(processingErrors) > 0 {
		return fmt.Errorf("processing errors occurred: %d files had errors", len(processingErrors))
	}

	return nil
}

// ScanSingleFileOptimized scans a single file for channel information with optimizations
func (r *Reader) ScanSingleFileOptimized(filepath string) (map[uint16]int, error) {
	localCounts := make(map[uint16]int)

	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	timeTagBlocks, err := r.findTimeTagBlocks(file)
	if err != nil {
		return localCounts, nil // No TIME_TAG blocks in this file
	}

	if len(timeTagBlocks) == 0 {
		return localCounts, nil
	}

	// For Method #1, use the original comprehensive scanning approach
	return r.processBlocksForScanning(filepath, timeTagBlocks, localCounts)
}

// ScanSingleFile scans a single file for channel information (legacy method)
func (r *Reader) ScanSingleFile(filepath string) (map[uint16]int, error) {
	return r.ScanSingleFileOptimized(filepath)
}

// QuickDiscoverChannels - ultra-fast channel discovery for CSV export (no counting needed)
func (r *Reader) QuickDiscoverChannels(filepath string) (map[uint16]bool, error) {
	channels := make(map[uint16]bool)

	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	timeTagBlocks, err := r.findTimeTagBlocks(file)
	if err != nil {
		return channels, nil // No TIME_TAG blocks in this file
	}

	if len(timeTagBlocks) == 0 {
		return channels, nil
	}

	// Scan first 3 blocks for better channel discovery
	maxBlocksToScan := 3
	if len(timeTagBlocks) < maxBlocksToScan {
		maxBlocksToScan = len(timeTagBlocks)
	}

	for i := 0; i < maxBlocksToScan; i++ {
		r.quickDiscoverChannelsInBlock(file, timeTagBlocks[i], channels)

		// Stop early if we found enough channels
		if len(channels) >= 30 {
			break
		}
	}

	return channels, nil
}

func (r *Reader) quickDiscoverChannelsInBlock(file *os.File, block SITTBlockInfo, channels map[uint16]bool) {
	dataStart := block.Position + SITTHeaderSize
	_, err := file.Seek(int64(dataStart), io.SeekStart)
	if err != nil {
		return
	}

	reader := bufio.NewReader(file)

	// Scan more events per block to find more channels
	maxEvents := 200 // Increased from 50
	dataSize := block.Length - SITTHeaderSize
	maxPossibleEvents := int(dataSize / RecordSize)

	if maxEvents > maxPossibleEvents {
		maxEvents = maxPossibleEvents
	}

	buffer := make([]byte, RecordSize)

	for eventCount := 0; eventCount < maxEvents; eventCount++ {
		n, err := reader.Read(buffer)
		if err != nil || n < RecordSize {
			break
		}

		// Parse channel only (we don't need timestamp for discovery)
		channel := binary.LittleEndian.Uint16(buffer[8:10])

		// Quick validation and store
		if channel < 65535 {
			channels[channel] = true
		}
	}
}

func (r *Reader) openAndValidateFile(filename string) (*os.File, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}

	fileInfo, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to get file info: %v", err)
	}

	if fileInfo.Size() == 0 {
		file.Close()
		return nil, fmt.Errorf("file is empty")
	}

	if fileInfo.Size() < 32 {
		file.Close()
		return nil, fmt.Errorf("file too small to contain valid SITT data (size: %d bytes)", fileInfo.Size())
	}

	return file, nil
}

func (r *Reader) findAllSITTBlocks(file *os.File) ([]SITTBlockInfo, error) {
	blockTypes := []uint32{0x01, 0x02, 0x03}
	var allBlocks []SITTBlockInfo
	foundAnyBlock := false

	for _, blockType := range blockTypes {
		blocks, err := r.findSITTBlocks(file, blockType)
		if err != nil {
			return nil, fmt.Errorf("failed to find SITT blocks: %v", err)
		}

		if len(blocks) > 0 {
			foundAnyBlock = true
			allBlocks = append(allBlocks, blocks...)
		}
	}

	if !foundAnyBlock {
		return nil, fmt.Errorf("no SITT blocks found - file may not be in SITT format")
	}

	return allBlocks, nil
}

func (r *Reader) processBlocksInParallel(file *os.File, allBlocks []SITTBlockInfo, channelFilter []uint16, resultChan chan<- TimeTag) (int, error) {
	const maxBlockWorkers = 8
	blockChan := make(chan SITTBlockInfo, len(allBlocks))
	var blockWg sync.WaitGroup
	var totalCount int64
	var countMu sync.Mutex

	numWorkers := maxBlockWorkers
	if len(allBlocks) < numWorkers {
		numWorkers = len(allBlocks)
	}

	// Create channel filter map for efficient lookup
	filterMap := make(map[uint16]bool)
	for _, ch := range channelFilter {
		filterMap[ch] = true
	}

	for i := 0; i < numWorkers; i++ {
		blockWg.Add(1)
		go func() {
			defer blockWg.Done()
			localCount := 0

			for block := range blockChan {
				count, err := r.processBlockOptimized(file, block, filterMap, resultChan)
				if err != nil {
					continue // Skip blocks with errors
				}
				localCount += count
			}

			countMu.Lock()
			totalCount += int64(localCount)
			countMu.Unlock()
		}()
	}

	for _, block := range allBlocks {
		blockChan <- block
	}
	close(blockChan)

	blockWg.Wait()
	return int(totalCount), nil
}

func (r *Reader) processBlocksUnfiltered(file *os.File, allBlocks []SITTBlockInfo, resultChan chan<- TimeTag) (int, error) {
	const maxBlockWorkers = 8
	blockChan := make(chan SITTBlockInfo, len(allBlocks))
	var blockWg sync.WaitGroup
	var totalCount int64
	var countMu sync.Mutex

	numWorkers := maxBlockWorkers
	if len(allBlocks) < numWorkers {
		numWorkers = len(allBlocks)
	}

	for i := 0; i < numWorkers; i++ {
		blockWg.Add(1)
		go func() {
			defer blockWg.Done()
			localCount := 0

			for block := range blockChan {
				count, err := r.processBlockUnfiltered(file, block, resultChan)
				if err != nil {
					continue // Skip blocks with errors
				}
				localCount += count
			}

			countMu.Lock()
			totalCount += int64(localCount)
			countMu.Unlock()
		}()
	}

	for _, block := range allBlocks {
		blockChan <- block
	}
	close(blockChan)

	blockWg.Wait()
	return int(totalCount), nil
}

// processBlockOptimized processes a single SITT block with filtering
func (r *Reader) processBlockOptimized(file *os.File, block SITTBlockInfo, channelFilter map[uint16]bool, resultChan chan<- TimeTag) (int, error) {
	// Create a separate file handle for this worker to avoid race conditions
	workerFile, err := os.Open(file.Name())
	if err != nil {
		return 0, err
	}
	defer workerFile.Close()

	dataStart := block.Position + SITTHeaderSize
	if _, err := workerFile.Seek(int64(dataStart), io.SeekStart); err != nil {
		return 0, err
	}

	dataSize := r.calculateDataSize(block.Length)
	if dataSize == 0 {
		return 0, nil
	}

	// Use buffered reader for better performance
	reader := bufio.NewReader(workerFile)
	count := 0
	eventCount := 0
	maxEvents := int(dataSize / RecordSize)

	// Read all records in the block using binary.Read for consistency with scanning
	for eventCount < maxEvents {
		var timestamp uint64
		var channel uint16

		// Read timestamp (8 bytes, little endian)
		err := binary.Read(reader, binary.LittleEndian, &timestamp)
		if err != nil {
			if err == io.EOF {
				break // End of data
			}
			return count, err
		}

		// Read channel (2 bytes, little endian)
		err = binary.Read(reader, binary.LittleEndian, &channel)
		if err != nil {
			if err == io.EOF {
				break // End of data
			}
			return count, err
		}

		eventCount++

		// Validate and filter the time tag
		if r.isValidTimeTag(channel, timestamp) && channelFilter[channel] {
			tag := TimeTag{Timestamp: timestamp, Channel: channel}
			select {
			case resultChan <- tag:
				count++
			default:
				// Channel is full, this shouldn't happen with proper buffering
				// but we handle it gracefully
				resultChan <- tag
				count++
			}
		}
	}

	return count, nil
}

// processBlockUnfiltered processes a single SITT block without any channel filtering - extracts ALL time tags
func (r *Reader) processBlockUnfiltered(file *os.File, block SITTBlockInfo, resultChan chan<- TimeTag) (int, error) {
	// Create a separate file handle for this worker to avoid race conditions
	workerFile, err := os.Open(file.Name())
	if err != nil {
		return 0, err
	}
	defer workerFile.Close()

	dataStart := block.Position + SITTHeaderSize
	if _, err := workerFile.Seek(int64(dataStart), io.SeekStart); err != nil {
		return 0, err
	}

	dataSize := r.calculateDataSize(block.Length)
	if dataSize == 0 {
		return 0, nil
	}

	// Use buffered reader for better performance
	reader := bufio.NewReader(workerFile)
	count := 0
	eventCount := 0
	maxEvents := int(dataSize / RecordSize)

	// Read all records in the block using binary.Read
	for eventCount < maxEvents {
		var timestamp uint64
		var channel uint16

		// Read timestamp (8 bytes, little endian)
		err := binary.Read(reader, binary.LittleEndian, &timestamp)
		if err != nil {
			if err == io.EOF {
				break // End of data
			}
			return count, err
		}

		// Read channel (2 bytes, little endian)
		err = binary.Read(reader, binary.LittleEndian, &channel)
		if err != nil {
			if err == io.EOF {
				break // End of data
			}
			return count, err
		}

		eventCount++

		// Validate the time tag but don't filter by channel - extract ALL time tags
		if r.isValidTimeTag(channel, timestamp) {
			tag := TimeTag{Timestamp: timestamp, Channel: channel}
			select {
			case resultChan <- tag:
				count++
			default:
				// Channel is full, this shouldn't happen with proper buffering
				// but we handle it gracefully
				resultChan <- tag
				count++
			}
		}
	}

	return count, nil
}

// findSITTBlocks finds SITT blocks of a specific type in the file
func (r *Reader) findSITTBlocks(file *os.File, blockType uint32) ([]SITTBlockInfo, error) {
	var blocks []SITTBlockInfo

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := fileInfo.Size()

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	reader := bufio.NewReaderSize(file, LargeBufferSize)

	var position int64 = 0
	buffer := make([]byte, DefaultBufferSize)
	overlap := SITTHeaderSize

	for position < fileSize {
		n, err := reader.Read(buffer[overlap:])
		if err != nil && err != io.EOF {
			return nil, err
		}
		if n == 0 {
			break
		}

		totalBytes := n + overlap
		newBlocks := r.searchSITTInBuffer(buffer, totalBytes, blockType, position, overlap)
		blocks = append(blocks, newBlocks...)

		// Move overlap data to beginning
		if totalBytes >= overlap {
			copy(buffer[:overlap], buffer[totalBytes-overlap:totalBytes])
		}
		position += int64(n)
	}

	return blocks, nil
}

// Helper function to search for SITT patterns in buffer
func (r *Reader) searchSITTInBuffer(buffer []byte, totalBytes int, blockType uint32, position int64, overlap int) []SITTBlockInfo {
	var blocks []SITTBlockInfo

	for i := 0; i < totalBytes-SITTHeaderSize; i += 4 {
		if r.isSITTMagic(buffer, i) && i+SITTHeaderSize <= totalBytes {
			length := binary.LittleEndian.Uint32(buffer[i+4 : i+8])
			btype := binary.LittleEndian.Uint32(buffer[i+8 : i+12])

			if btype == blockType {
				blocks = append(blocks, SITTBlockInfo{
					Position: uint64(position + int64(i) - int64(overlap)),
					Type:     btype,
					Length:   length,
				})
			}
		}
	}

	return blocks
}

// Helper function to check SITT magic bytes
func (r *Reader) isSITTMagic(buffer []byte, index int) bool {
	return buffer[index] == 'S' &&
		buffer[index+1] == 'I' &&
		buffer[index+2] == 'T' &&
		buffer[index+3] == 'T'
}

// Helper functions to reduce complexity
func (r *Reader) calculateDataSize(length uint32) uint32 {
	dataSize := length - 4 // Length includes magic but not rest of header
	if dataSize < 12 {
		return 0
	}
	return dataSize - 12 // Rest of header
}

func (r *Reader) readDataBlock(file *os.File, dataSize uint32) ([]byte, error) {
	reader := bufio.NewReader(file)
	data := make([]byte, dataSize)
	_, err := io.ReadFull(reader, data)
	return data, err
}

func (r *Reader) tryParseTimeTag(chunk []byte) *TimeTag {
	// Use the same format as the scanner: timestamp(8 bytes) + channel(2 bytes)
	// This matches the format used in scanSingleFile
	if len(chunk) >= RecordSize {
		// Read timestamp (8 bytes, little endian)
		timestamp := binary.LittleEndian.Uint64(chunk[0:8])
		// Read channel (2 bytes, little endian)
		channel := binary.LittleEndian.Uint16(chunk[8:10])

		if r.isValidTimeTag(channel, timestamp) {
			return &TimeTag{Timestamp: timestamp, Channel: channel}
		}
	}

	return nil
}

func (r *Reader) isValidTimeTag(channel uint16, timestamp uint64) bool {
	// Removed arbitrary channel limit - allow all valid uint16 channels
	return timestamp > 0
}

func (r *Reader) findTimeTagBlocks(file *os.File) ([]SITTBlockInfo, error) {
	blockTypes := []uint32{0x01, 0x03, 0x04} // Based on analysis: type 3 and 4 contain time tag data
	var timeTagBlocks []SITTBlockInfo

	for _, blockType := range blockTypes {
		blocks, err := r.findSITTBlocks(file, blockType)
		if err == nil {
			timeTagBlocks = append(timeTagBlocks, blocks...)
		}
	}

	return timeTagBlocks, nil
}

func (r *Reader) processBlocksForScanning(filepath string, timeTagBlocks []SITTBlockInfo, localCounts map[uint16]int) (map[uint16]int, error) {
	blockChan := make(chan SITTBlockInfo, len(timeTagBlocks))
	var blockWg sync.WaitGroup
	var blockMu sync.Mutex

	// Limit concurrent block processing
	maxBlockWorkers := MaxConcurrency
	if len(timeTagBlocks) < maxBlockWorkers {
		maxBlockWorkers = len(timeTagBlocks)
	}

	for i := 0; i < maxBlockWorkers; i++ {
		blockWg.Add(1)
		go func() {
			defer blockWg.Done()
			blockLocalCounts := r.scanBlocksWorker(filepath, blockChan)

			// Merge block results
			blockMu.Lock()
			for channel, count := range blockLocalCounts {
				localCounts[channel] += count
			}
			blockMu.Unlock()
		}()
	}

	// Send blocks to workers
	for _, block := range timeTagBlocks {
		blockChan <- block
	}
	close(blockChan)

	blockWg.Wait()
	return localCounts, nil
}

// processBlocksForScanningOptimized - faster scanning with strategic sampling
func (r *Reader) processBlocksForScanningOptimized(file *os.File, timeTagBlocks []SITTBlockInfo, localCounts map[uint16]int) (map[uint16]int, error) {
	// For method #1 channel discovery, we need to be more thorough than the quick scan
	// We'll scan more blocks but with optimized reading per block

	maxBlocksToScan := len(timeTagBlocks) // Scan all blocks for method #1
	if maxBlocksToScan > 10 {
		maxBlocksToScan = 10 // But limit to first 10 blocks for performance
	}

	for i := 0; i < maxBlocksToScan; i++ {
		block := timeTagBlocks[i]
		counts := r.scanSingleBlockOptimized(file, block)

		for channel, count := range counts {
			localCounts[channel] += count
		}
	}

	return localCounts, nil
}

func (r *Reader) scanSingleBlockOptimized(file *os.File, block SITTBlockInfo) map[uint16]int {
	blockCounts := make(map[uint16]int)

	dataStart := block.Position + SITTHeaderSize
	_, err := file.Seek(int64(dataStart), io.SeekStart)
	if err != nil {
		return blockCounts
	}

	reader := bufio.NewReader(file)

	// For method #1: scan more events to ensure we find all channels
	maxEvents := 500 // Increased from 100 to find more channels
	dataSize := block.Length - SITTHeaderSize
	maxPossibleEvents := int(dataSize / RecordSize)

	if maxEvents > maxPossibleEvents {
		maxEvents = maxPossibleEvents
	}

	// Pre-allocate buffer for better performance
	buffer := make([]byte, RecordSize)

	for eventCount := 0; eventCount < maxEvents; eventCount++ {
		n, err := reader.Read(buffer)
		if err != nil || n < RecordSize {
			break
		}

		// Parse directly from buffer
		timetag := binary.LittleEndian.Uint64(buffer[0:8])
		channel := binary.LittleEndian.Uint16(buffer[8:10])

		// Quick validation
		if timetag > 0 && channel < 65535 {
			blockCounts[channel]++
		}
	}

	return blockCounts
}

func (r *Reader) scanBlocksWorker(filepath string, blockChan <-chan SITTBlockInfo) map[uint16]int {
	blockLocalCounts := make(map[uint16]int)

	for block := range blockChan {
		// Create separate file handle for this worker
		workerFile, err := os.Open(filepath)
		if err != nil {
			continue
		}

		counts := r.scanSingleBlock(workerFile, block)
		for channel, count := range counts {
			blockLocalCounts[channel] += count
		}

		workerFile.Close()
	}

	return blockLocalCounts
}

func (r *Reader) scanSingleBlock(workerFile *os.File, block SITTBlockInfo) map[uint16]int {
	blockCounts := make(map[uint16]int)

	dataStart := block.Position + SITTHeaderSize
	_, err := workerFile.Seek(int64(dataStart), io.SeekStart)
	if err != nil {
		return blockCounts
	}

	reader := bufio.NewReader(workerFile)
	eventCount := 0
	dataSize := block.Length - SITTHeaderSize

	for eventCount < MaxSampleEvents && uint32(eventCount*RecordSize) < dataSize {
		var timetag uint64
		var channel uint16

		// Read timetag (8 bytes, little endian)
		err := binary.Read(reader, binary.LittleEndian, &timetag)
		if err != nil {
			break
		}

		// Read channel (2 bytes, little endian)
		err = binary.Read(reader, binary.LittleEndian, &channel)
		if err != nil {
			break
		}

		blockCounts[channel]++
		eventCount++
	}

	return blockCounts
}

// processAllEventsInBlocks processes all events in the blocks (not just a sample like scanning)
func (r *Reader) processAllEventsInBlocks(file *os.File, timeTagBlocks []SITTBlockInfo, resultChan chan<- TimeTag) (int, error) {
	totalCount := 0

	for _, block := range timeTagBlocks {
		dataStart := block.Position + SITTHeaderSize
		_, err := file.Seek(int64(dataStart), io.SeekStart)
		if err != nil {
			continue // Skip this block
		}

		reader := bufio.NewReader(file)
		eventCount := 0
		dataSize := block.Length - SITTHeaderSize

		// Read ALL events in this block (not limited like scanning)
		maxEvents := int(dataSize / RecordSize)
		for eventCount < maxEvents {
			var timestamp uint64
			var channel uint16

			// Read timestamp (8 bytes, little endian)
			err := binary.Read(reader, binary.LittleEndian, &timestamp)
			if err != nil {
				break // End of data or error
			}

			// Read channel (2 bytes, little endian)
			err = binary.Read(reader, binary.LittleEndian, &channel)
			if err != nil {
				break // End of data or error
			}

			eventCount++

			// Validate and send the time tag (no channel filtering)
			if r.isValidTimeTag(channel, timestamp) {
				tag := TimeTag{Timestamp: timestamp, Channel: channel}
				select {
				case resultChan <- tag:
					totalCount++
				default:
					// Channel is full, but handle gracefully
					resultChan <- tag
					totalCount++
				}
			}
		}
	}

	return totalCount, nil
}
