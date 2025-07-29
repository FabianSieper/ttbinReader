// Example demonstrating how to use the ttbin module
package main

import (
	"fmt"

	ttbin "github.com/FabianSieper/ttbinReader"
)

func readInTtbinFolder() string {
	fmt.Println("Enter path to folder containing ttbin files")
	var ttbinFolder string

	fmt.Scanln(&ttbinFolder)
	fmt.Println("Processing ttbin files in folder:", ttbinFolder)

	return ttbinFolder
}

func main() {
	ttbinFolder := readInTtbinFolder()
	processor := ttbin.NewProcessor(ttbinFolder)

	// Check if directory exists
	if err := processor.ValidateDataDirectory(); err != nil {
		fmt.Printf("Data directory validation failed: %v\n", err)
		return
	}

	fmt.Println("Data directory validated successfully.")

	// Get all .ttbin files from files
	files, err := processor.GetFiles()
	if err != nil {
		fmt.Printf("Failed to get files: %v", err)
		return
	}

	fmt.Printf("Found %d .ttbin files:\n", len(files))
	for i, file := range files {
		fmt.Printf("  %d. %s\n", i+1, file)
	}

	// Scan for available channels
	fmt.Println("\nScanning files for available channels...")
	// TODO: improve scanChannels method
	channels, err := processor.ScanChannels(files)
	if err != nil {
		fmt.Printf("Failed to scan channels: %v\n", err)
		return
	}

	// Display available channels
	fmt.Printf("\nFound channels in files:\n")
	processor.DisplayChannels(channels)
}
