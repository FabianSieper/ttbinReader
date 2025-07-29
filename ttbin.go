// Package ttbin provides functionality for reading and processing SITT format timetag files (.ttbin)
//
// This package provides a high-level interface for working with timetag files in the SITT format.
// It supports parallel processing, streaming reads, and channel filtering for efficient processing
// of large ttbin files.
//
// Basic usage:
//
//	// Create a processor for ttbin files
//	processor := ttbin.NewProcessor("data")
//
//	// Get available files
//	files, err := processor.GetFiles()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Scan for available channels
//	channels, err := processor.ScanChannels(files)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Process files for specific channels
//	timeTags := make(chan ttbin.TimeTag, 1000)
//	go processor.ProcessFiles(files, []uint16{258, 259}, timeTags)
//
//	// Process the received time tags
//	for tag := range timeTags {
//		// Handle each time tag
//		fmt.Printf("Channel %d: %d\n", tag.Channel, tag.Timestamp)
//	}
package ttbin

import (
	"fmt"
)

// Processor provides a high-level interface for processing ttbin files
type Processor struct {
	reader    *Reader
	validator *FileValidator
	helper    *ChannelHelper
}

// NewProcessor creates a new ttbin Processor for the specified data directory
func NewProcessor(dataDirectory string) *Processor {
	return &Processor{
		reader:    NewReader(dataDirectory),
		validator: NewFileValidator(),
		helper:    NewChannelHelper(),
	}
}

// ValidateDataDirectory checks if the data directory exists and is accessible
func (p *Processor) ValidateDataDirectory() error {
	return p.validator.ValidateDataDirectory(p.reader.DataDirectory)
}

// GetFiles returns all .ttbin files in the data directory, sorted lexicographically
func (p *Processor) GetFiles() ([]string, error) {
	files, err := p.reader.GetTimeTaFiles()
	if err != nil {
		return nil, err
	}

	if err := p.validator.ValidateFiles(files); err != nil {
		return nil, err
	}

	return files, nil
}

// ScanChannels scans all files to discover available channels and their event counts
func (p *Processor) ScanChannels(files []string) (map[uint16]int, error) {
	return p.reader.ScanAvailableChannels(files)
}

// QuickScanChannels scans only the first file to discover available channels for display purposes
// This is useful for CSV export where we don't need precise counts, just channel discovery
func (p *Processor) QuickScanChannels(files []string) (map[uint16]int, error) {
	if len(files) == 0 {
		return make(map[uint16]int), nil
	}

	fmt.Printf("Quick scanning first file to discover available channels...\n")

	// Use ultra-fast channel discovery - just find channels, no counting
	channels, err := p.reader.QuickDiscoverChannels(files[0])
	if err != nil {
		return nil, err
	}

	// Convert set to map with dummy counts for compatibility
	channelCounts := make(map[uint16]int)
	for channel := range channels {
		channelCounts[channel] = 1 // Dummy count - we don't need actual counts for CSV export
	}

	fmt.Printf("Found %d unique channels in first file\n\n", len(channelCounts))
	return channelCounts, nil
}

// DisplayChannels prints the available channels in a formatted way
func (p *Processor) DisplayChannels(channels map[uint16]int) {
	p.helper.DisplayAvailableChannels(channels)
}

// ValidateChannel checks if a channel exists in the available channels
func (p *Processor) ValidateChannel(channel uint16, availableChannels map[uint16]int) error {
	return p.helper.ValidateChannel(channel, availableChannels)
}

// GetChannelCount returns the number of events for a specific channel
func (p *Processor) GetChannelCount(channel uint16, availableChannels map[uint16]int) int {
	return p.helper.GetChannelCount(channel, availableChannels)
}

// ProcessFiles processes multiple files in parallel, filtering for specified channels
// The results are sent to the provided channel. The channel will be closed when processing is complete.
func (p *Processor) ProcessFiles(files []string, channelFilter []uint16, resultChan chan<- TimeTag) error {
	return p.reader.ProcessFiles(files, channelFilter, resultChan)
}

// ProcessAllFiles processes multiple files in parallel, extracting ALL time tags without any channel filtering
// The results are sent to the provided channel. The channel will be closed when processing is complete.
func (p *Processor) ProcessAllFiles(files []string, resultChan chan<- TimeTag) error {
	return p.reader.ProcessAllFiles(files, resultChan)
}

// ProcessFile processes a single file, filtering for specified channels
func (p *Processor) ProcessFile(filename string, channelFilter []uint16, resultChan chan<- TimeTag) (int, error) {
	return p.reader.ProcessFileOptimized(filename, channelFilter, resultChan)
}

// GetDataDirectory returns the data directory path
func (p *Processor) GetDataDirectory() string {
	return p.reader.DataDirectory
}

// SetDataDirectory changes the data directory path
func (p *Processor) SetDataDirectory(dataDirectory string) {
	p.reader.DataDirectory = dataDirectory
}

// ProcessFilesWithCallback processes files and calls a callback function for each TimeTag
// This is a convenience method for simple processing without channels
func (p *Processor) ProcessFilesWithCallback(files []string, channelFilter []uint16, callback func(TimeTag)) error {
	timeTagChan := make(chan TimeTag, 1000)

	// Start processing in a goroutine
	go func() {
		err := p.ProcessFiles(files, channelFilter, timeTagChan)
		if err != nil {
			// Log error or handle it appropriately
			fmt.Printf("Error processing files: %v\n", err)
		}
	}()

	// Process received time tags
	for tag := range timeTagChan {
		callback(tag)
	}

	return nil
}

// GetFileInfo returns information about ttbin files without processing them
type FileInfo struct {
	Filename string
	Size     int64
	Channels map[uint16]int
}

// QuickScan performs a quick scan of a single file to get basic information
func (p *Processor) QuickScan(filename string) (*FileInfo, error) {
	channels, err := p.reader.ScanSingleFile(filename)
	if err != nil {
		return nil, err
	}

	file, err := p.reader.openAndValidateFile(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	return &FileInfo{
		Filename: filename,
		Size:     stat.Size(),
		Channels: channels,
	}, nil
}
