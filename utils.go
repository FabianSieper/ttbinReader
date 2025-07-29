// Package ttbin provides functionality for reading and processing SITT format timetag files (.ttbin)
package ttbin

import (
	"fmt"
	"os"
	"sort"
)

// TimeDiff represents a time difference between two channels
type TimeDiff struct {
	Timestamp1 uint64
	Timestamp2 uint64
	Channel1   uint16
	Channel2   uint16
	Diff       int64
}

// FileValidator provides validation functionality for ttbin files and directories
type FileValidator struct{}

// NewFileValidator creates a new FileValidator
func NewFileValidator() *FileValidator {
	return &FileValidator{}
}

// ValidateDataDirectory checks if the data directory exists
func (v *FileValidator) ValidateDataDirectory(dataDirectory string) error {
	if _, err := os.Stat(dataDirectory); os.IsNotExist(err) {
		return fmt.Errorf("'%s' directory not found! Please create a '%s' directory and place your .ttbin files in it", dataDirectory, dataDirectory)
	}
	return nil
}

// ValidateFiles checks if files exist and are accessible
func (v *FileValidator) ValidateFiles(files []string) error {
	if len(files) == 0 {
		return fmt.Errorf("no .ttbin files found. Please place your .ttbin files in the data folder")
	}

	for _, file := range files {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", file)
		}
	}

	return nil
}

// ChannelHelper provides utilities for working with channels
type ChannelHelper struct{}

// NewChannelHelper creates a new ChannelHelper
func NewChannelHelper() *ChannelHelper {
	return &ChannelHelper{}
}

// DisplayAvailableChannels prints available channels in a formatted way
func (c *ChannelHelper) DisplayAvailableChannels(availableChannels map[uint16]int) {
	if len(availableChannels) == 0 {
		fmt.Println("No channels found.")
		return
	}

	fmt.Printf("Available channels found in files:\n")
	var sortedChannels []uint16
	for ch := range availableChannels {
		sortedChannels = append(sortedChannels, ch)
	}
	sort.Slice(sortedChannels, func(i, j int) bool { return sortedChannels[i] < sortedChannels[j] })

	for _, ch := range sortedChannels {
		count := availableChannels[ch]
		fmt.Printf("  Channel %d: %d events\n", ch, count)
	}
	fmt.Println()
}

// ValidateChannel checks if a channel exists in the available channels
func (c *ChannelHelper) ValidateChannel(channel uint16, availableChannels map[uint16]int) error {
	if _, exists := availableChannels[channel]; !exists {
		return fmt.Errorf("channel %d not found in available channels", channel)
	}
	return nil
}

// GetChannelCount returns the number of events for a specific channel
func (c *ChannelHelper) GetChannelCount(channel uint16, availableChannels map[uint16]int) int {
	return availableChannels[channel]
}

// SortTimeTagsByTimestamp sorts a slice of TimeTags by timestamp
func SortTimeTagsByTimestamp(tags []TimeTag) {
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Timestamp < tags[j].Timestamp
	})
}

// CalculateTimeDifference calculates the time difference between two timestamps
func CalculateTimeDifference(timestamp1, timestamp2 uint64) int64 {
	return int64(timestamp2) - int64(timestamp1)
}

// CreateTimeDiff creates a TimeDiff struct from two TimeTags
func CreateTimeDiff(tag1, tag2 TimeTag) TimeDiff {
	return TimeDiff{
		Timestamp1: tag1.Timestamp,
		Timestamp2: tag2.Timestamp,
		Channel1:   tag1.Channel,
		Channel2:   tag2.Channel,
		Diff:       CalculateTimeDifference(tag1.Timestamp, tag2.Timestamp),
	}
}

// FilterTimeTagsByChannels filters TimeTags to only include specified channels
func FilterTimeTagsByChannels(tags []TimeTag, channels []uint16) []TimeTag {
	channelMap := make(map[uint16]bool)
	for _, ch := range channels {
		channelMap[ch] = true
	}

	var filtered []TimeTag
	for _, tag := range tags {
		if channelMap[tag.Channel] {
			filtered = append(filtered, tag)
		}
	}

	return filtered
}

// Statistics provides statistics about ttbin file processing
type Statistics struct {
	TotalFiles       int
	ProcessedFiles   int
	TotalTimeTags    int
	ChannelCounts    map[uint16]int
	ProcessingErrors []error
}

// NewStatistics creates a new Statistics struct
func NewStatistics() *Statistics {
	return &Statistics{
		ChannelCounts:    make(map[uint16]int),
		ProcessingErrors: make([]error, 0),
	}
}

// AddError adds an error to the statistics
func (s *Statistics) AddError(err error) {
	s.ProcessingErrors = append(s.ProcessingErrors, err)
}

// AddTimeTag updates statistics with a new TimeTag
func (s *Statistics) AddTimeTag(tag TimeTag) {
	s.TotalTimeTags++
	s.ChannelCounts[tag.Channel]++
}

// HasErrors returns true if there are processing errors
func (s *Statistics) HasErrors() bool {
	return len(s.ProcessingErrors) > 0
}

// GetErrorCount returns the number of processing errors
func (s *Statistics) GetErrorCount() int {
	return len(s.ProcessingErrors)
}

// DisplaySummary prints a summary of the statistics
func (s *Statistics) DisplaySummary() {
	fmt.Printf("Processing Summary:\n")
	fmt.Printf("  Total files: %d\n", s.TotalFiles)
	fmt.Printf("  Successfully processed: %d\n", s.ProcessedFiles)
	fmt.Printf("  Total time tags: %d\n", s.TotalTimeTags)

	if s.HasErrors() {
		fmt.Printf("  Errors encountered: %d\n", s.GetErrorCount())
	}

	if len(s.ChannelCounts) > 0 {
		fmt.Printf("  Channels found:\n")
		helper := NewChannelHelper()
		helper.DisplayAvailableChannels(s.ChannelCounts)
	}
}
