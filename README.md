# Laracasts Downloader

A robust Go application that downloads Laracasts topics, series and bits. This tool helps you organize and manage your Laracasts content offline for convenient access and learning.

#### Note: This project is still in beta mode, not fully implemented yet it downloads all topics, series and bits from Laracasts.

## Features

### Topic-Based Organization
- Automatically organizes downloads by topics (e.g., Laravel, Vue, Testing, etc.)
- Creates a clean directory structure for easy navigation
- Generates summary files for each topic and an overall download summary
- Maintains metadata for all downloaded content

### Concurrent Downloads
- Uses worker pools for efficient parallel downloading
- Handles multiple topics and series simultaneously
- Implements rate limiting to prevent server overload
- Optimizes bandwidth usage with configurable concurrency

### Smart Error Handling
- Retries failed downloads with exponential backoff
- Maintains download state to resume interrupted operations
- Creates detailed logs of successes and failures
- Validates downloaded files for integrity

### Progress Tracking
- Shows real-time download progress with ETA
- Provides detailed statistics for each topic and series
- Creates summary files with download status and metadata
- Displays bandwidth usage and download speeds

## Directory Structure

```
downloads/
├── topics/
│   ├── Laravel/
│   │   ├── Laravel Basics/
│   │   │   ├── 01-Introduction-to-Laravel.mp4
│   │   │   ├── 02-Routing-Basics.mp4
│   │   │   └── series-info.json
│   │   └── Advanced Laravel/
│   │       ├── 01-Service-Containers.mp4
│   │       └── series-info.json
│   ├── Vue/
│   │   └── Vue3-Essentials/
│   │       ├── 01-Getting-Started.mp4
│   │       └── series-info.json
│   └── Testing/
│       └── PHPUnit-Testing/
│           ├── 01-Introduction.mp4
│           └── series-info.json
└── .cache/
    ├── downloads/
    │   └── download-state.json
    ├── series/
    │   └── series-metadata.json
    └── state/
        └── app-state.json
```

## Installation

1. Ensure you have Go 1.18 or higher installed:
```bash
go version
```

2. Install ffmpeg (required for video processing):
```bash
# Ubuntu/Debian
sudo apt-get install ffmpeg

# macOS
brew install ffmpeg

# Windows
choco install ffmpeg
```

3. Clone the repository:
```bash
git clone https://github.com/sajjadanwar0/laracasts-downloader.git
cd laracasts-downloader
```

4. Install dependencies:
```bash
go mod download
```

## Configuration

1. Copy the example environment file:
```bash
cp .env.example .env
```

2. Configure your environment variables in .env:
```
EMAIL=your@email.com
PASSWORD=your_password
DOWNLOAD_PATH=/path/to/downloads
VIDEO_QUALITY=1080p  # Options: 360p, 540p, 720p, 1080p
CONCURRENT_DOWNLOADS=3
RETRY_ATTEMPTS=3
BUFFER_SIZE=8192
```

## Usage

### Basic Usage

Run the downloader to fetch all available content:
```bash
go run main.go
```

The application will:
1. Log in to your Laracasts account
2. Fetch all available topics
3. Create topic directories
4. Download all series for each topic
5. Generate summary files

### Download Specific Series

To download a specific series, use the series flag with the slug:
```bash
go run main.go -s the-definition-series
```

### Download All Topics

To download all topics:
```bash
go run main.go
```

## Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| EMAIL | Laracasts account email | Yes | - |
| PASSWORD | Laracasts account password | Yes | - |
| DOWNLOAD_PATH | Download directory path | Yes | - |
| VIDEO_QUALITY | Preferred video quality | No | 1080p |
| CONCURRENT_DOWNLOADS | Number of parallel downloads | No | 3 |
| RETRY_ATTEMPTS | Number of download retry attempts | No | 3 |
| BUFFER_SIZE | Download buffer size in bytes | No | 8192 |

## Performance Optimization

The downloader implements several performance optimization strategies:

### Buffered Downloads
- Uses buffered I/O for efficient file operations
- Implements configurable buffer sizes
- Minimizes system calls during downloads

### Concurrent Processing
- Parallel processing of topics and series
- Worker pools for download management
- Rate limiting to prevent overload

### Memory Management
- Efficient memory usage with buffer pools
- Garbage collection optimization
- Memory-conscious file handling

### State Management
- Persistent download state
- Resume capability for interrupted downloads
- Efficient metadata caching

## Error Handling

The application implements comprehensive error handling:

- **Download Retry**: Automatically retries failed downloads with exponential backoff
- **State Recovery**: Maintains download state for recovery after interruptions
- **Validation**: Checks file integrity after download
- **Logging**: Creates detailed logs for debugging and troubleshooting
- **Error Classification**: Categorizes errors for appropriate handling:
  - Network errors
  - Authentication failures
  - File system errors
  - Rate limiting issues

## Contributing

Contributions are welcome! Please follow these steps:

1. Fork the repository
2. Create your feature branch:
```bash
git checkout -b feature/my-new-feature
```
3. Commit your changes:
```bash
git commit -am 'Add some feature'
```
4. Push to the branch:
```bash
git push origin feature/my-new-feature
```
5. Submit a pull request


## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Support

For support, please:
1. Create a new issue if your problem isn't already reported
2. Provide detailed information about your problem
3. Include relevant logs and configuration
