# TTBin Reader

A high-performance Go library for reading and processing SITT (Simple Time Tag) format files (.ttbin). This library is designed for efficient processing of time-tagged events from scientific instruments or data acquisition systems.

## What is it for?

The TTBin Reader is specifically designed to:

- Read and process .ttbin files containing time-tagged events
- Handle large files (GB+) efficiently through streaming processing
- Filter and process specific channels of interest
- Provide both synchronous and asynchronous processing options
- Support parallel processing of multiple files

## Installation

\`\`\`bash
go get github.com/FabianSieper/ttbinReader
\`\`\`

## Quick Start

\`\`\`go
package main

import (
    "fmt"
    "log"
    "github.com/FabianSieper/ttbinReader"
)

func main() {
    // Create a processor pointing to your data directory
    processor := ttbin.NewProcessor("data")

    // Get list of .ttbin files
    files, err := processor.GetFiles()
    if err != nil {
        log.Fatal(err)
    }

    // Find available channels in the files
    channels, err := processor.ScanChannels(files)
    if err != nil {
        log.Fatal(err)
    }

    // Process specific channels (e.g., 258 and 259)
    timeTags := make(chan ttbin.TimeTag, 1000)
    go processor.ProcessFiles(files, []uint16{258, 259}, timeTags)

    // Handle the time tags
    for tag := range timeTags {
        fmt.Printf("Channel %d: Timestamp %d\n", tag.Channel, tag.Timestamp)
    }
}
\`\`\`

## Features

- **Efficient Processing**: Stream-based processing that does not load entire files into memory
- **Channel Filtering**: Process only the channels you need
- **Parallel Processing**: Process multiple files concurrently
- **Simple API**: High-level interface for common operations
- **Flexible Usage**: Both synchronous and asynchronous processing options

## Command Line Tool

The package includes a command-line tool for quick file inspection:

\`\`\`bash
# Install the CLI tool
go install github.com/FabianSieper/ttbinReader/cmd/ttbinreader

# Run it on your data directory
ttbinreader -dir /path/to/data
\`\`\`

## Key Concepts

### TimeTag
A TimeTag represents a single time-tagged event with:
- Channel number (which detector/input it came from)
- Timestamp (when the event occurred)

### Processor
The main interface for working with ttbin files. It provides methods for:
- Finding and validating ttbin files
- Discovering available channels
- Processing files with channel filtering
- Both synchronous and asynchronous processing options

## Common Use Cases

1. **Channel Discovery**
   \`\`\`go
   channels, _ := processor.ScanChannels(files)
   processor.DisplayChannels(channels) // Print available channels
   \`\`\`

2. **Async Processing with Channel Filter**
   \`\`\`go
   timeTags := make(chan ttbin.TimeTag, 1000)
   go processor.ProcessFiles(files, []uint16{258, 259}, timeTags)
   \`\`\`

3. **Simple Callback Processing**
   \`\`\`go
   processor.ProcessFilesWithCallback(files, []uint16{258}, func(tag TimeTag) {
       // Handle each time tag
   })
   \`\`\`

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

[Add your license here]
