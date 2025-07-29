// Example demonstrating how to use the ttbin module
package main

import (
	"fmt"
	"log"

	ttbin "github.com/FabianSieper/ttbinReader"
)

func main() {
	// Create a processor for ttbin files in the "data" directory
	processor := ttbin.NewProcessor("data")

	// Validate data directory exists
	if err := processor.ValidateDataDirectory(); err != nil {
		log.Fatalf("Data directory validation failed: %v", err)
	}

	// Get all .ttbin files
	files, err := processor.GetFiles()
	if err != nil {
		log.Fatalf("Failed to get files: %v", err)
	}

	fmt.Printf("Found %d .ttbin files:\n", len(files))
	for i, file := range files {
		fmt.Printf("  %d. %s\n", i+1, file)
	}

	// Scan for available channels
	fmt.Println("\nScanning files for available channels...")
	channels, err := processor.ScanChannels(files)
	if err != nil {
		log.Fatalf("Failed to scan channels: %v", err)
	}

	// Display available channels
	fmt.Printf("\nFound channels in files:\n")
	processor.DisplayChannels(channels)

	// Example: Process files for specific channels (if they exist)
	if len(channels) >= 2 {
		// Get first two available channels
		var targetChannels []uint16
		for ch := range channels {
			targetChannels = append(targetChannels, ch)
			if len(targetChannels) == 2 {
				break
			}
		}

		if len(targetChannels) == 2 {
			fmt.Printf("Example: Processing files for channels %d and %d...\n", targetChannels[0], targetChannels[1])

			// Process files with callback function
			err := processor.ProcessFilesWithCallback(files, targetChannels, func(tag ttbin.TimeTag) {
				fmt.Printf("  Channel %d: Timestamp %d\n", tag.Channel, tag.Timestamp)
			})

			if err != nil {
				log.Printf("Error processing files: %v", err)
			} else {
				fmt.Println("Processing completed successfully!")
			}
		}
	}

	// Example: Quick scan of individual files
	fmt.Println("\nQuick scan of individual files:")
	for _, file := range files {
		info, err := processor.QuickScan(file)
		if err != nil {
			fmt.Printf("  %s: Error - %v\n", file, err)
			continue
		}

		fmt.Printf("  %s: Size=%d bytes, Channels=%d\n",
			info.Filename, info.Size, len(info.Channels))

		// Show first few channels
		count := 0
		for ch, events := range info.Channels {
			if count >= 3 {
				fmt.Printf("    ... and %d more channels\n", len(info.Channels)-3)
				break
			}
			fmt.Printf("    Channel %d: %d events\n", ch, events)
			count++
		}
	}
}
