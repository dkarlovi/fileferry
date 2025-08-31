# FileFerry

FileFerry is a Go-based command-line application for organizing media files (images and videos) based on metadata extraction. It scans source directories and moves files to organized target directories using configurable path templates that incorporate metadata like creation dates, camera information, and file extensions.

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

## Working Effectively

### Bootstrap and Build
- Verify Go installation: `go version` (requires Go 1.21+)
- Download dependencies: `go mod download` -- takes ~0.5 seconds
- Clean dependencies: `go mod tidy` -- takes ~1 second  
- Build application: `go build -o fileferry .` -- takes ~6 seconds. NEVER CANCEL.
- Verify build: `./fileferry` should show usage information

### Required External Tools (Optional but Recommended)
Install multimedia tools for full metadata extraction functionality:
```bash
sudo apt-get update  # takes ~7 seconds
sudo apt-get install -y ffmpeg libimage-exiftool-perl  # takes ~25 seconds. NEVER CANCEL.
```
- `ffprobe` (from ffmpeg): Video metadata extraction
- `exiftool`: Enhanced image metadata extraction
- Application works without these tools but metadata extraction will be limited

### Linting and Code Quality
- Format check: `gofmt -d .` -- takes ~0.1 seconds
- Basic vet: `go vet ./...` -- takes ~1 second
- Install staticcheck: `go install honnef.co/go/tools/cmd/staticcheck@latest` -- takes ~18 seconds. NEVER CANCEL.
- Advanced linting: `~/go/bin/staticcheck ./...` -- takes ~1.5 seconds
- ALWAYS run `gofmt -d .`, `go vet ./...`, and `staticcheck ./...` before committing

### Testing
- Run tests: `go test -v ./...` -- no test files currently exist
- Application has no existing test infrastructure
- Manual testing is required (see Validation section)

## Application Usage

### Basic Usage
```bash
./fileferry <config.yaml> [--ack]
```
- Without `--ack`: Dry run mode (shows what would be moved)
- With `--ack`: Actually moves files

### Configuration Format
Create a YAML config file:
```yaml
sources:
  - path: "/path/to/source"
    recurse: true
    types: ["image", "video"]

target:
  image:
    path: "/organized/{meta.taken.year}/{meta.taken.date}/{file.extension}"
  video:
    path: "/organized_video/{meta.taken.year}/{meta.taken.date}/{file.extension}"
```

### Template Variables
- `{meta.taken.year}`: Year from metadata
- `{meta.taken.date}`: Date from metadata (YYYY-MM-DD format)
- `{meta.taken.datetime}`: Full datetime from metadata
- `{file.extension}`: File extension without dot
- `{meta.camera.maker}`: Camera manufacturer
- `{meta.camera.model}`: Camera model

## Validation

### Manual Testing Workflow
ALWAYS test application functionality after making changes:

1. **Create test environment**:
```bash
mkdir -p /tmp/test_images
cd /tmp/test_images
echo "test content" > test.jpg
echo "test content" > test.mp4
touch -d "2023-05-15 10:30:45" test.jpg
```

2. **Create test config**:
```bash
cat > /tmp/test_config.yaml << 'EOF'
sources:
  - path: "/tmp/test_images"
    recurse: true
    types: ["image", "video"]

target:
  image:
    path: "/tmp/organized/{meta.taken.year}/{meta.taken.date}/{file.extension}"
  video:
    path: "/tmp/organized_video/{meta.taken.year}/{meta.taken.date}/{file.extension}"
EOF
```

3. **Test dry run**:
```bash
./fileferry /tmp/test_config.yaml
```
Should show files that would be moved without actually moving them.

4. **Test actual execution**:
```bash
./fileferry /tmp/test_config.yaml --ack
```
Should move files to organized directories.

5. **Verify results**:
```bash
find /tmp/organized* -type f 2>/dev/null || echo "Check file organization"
```

6. **Clean up**:
```bash
rm -rf /tmp/test_images /tmp/organized*
```

### Expected Behavior
- Application loads YAML configuration successfully
- Scans source directories for supported file types
- Extracts metadata when possible (requires external tools for full functionality)
- Creates target directory structure as needed
- Moves files according to template paths
- Provides summary of moved/skipped files

## Code Structure

### Key Files
- `main.go`: Entry point, CLI parsing, main processing loop
- `config.go`: YAML configuration loading and structure definitions  
- `filesystem.go`: File scanning, path resolution, and file operations
- `metadata.go`: Metadata extraction from files (partially implemented)

### Supported File Types
**Images**: `.jpg`, `.jpeg`, `.png`, `.gif`, `.bmp`, `.tiff`, `.webp`
**Videos**: `.mp4`, `.mov`, `.avi`, `.mkv`, `.webm`, `.flv`, `.wmv`

### Dependencies
- `gopkg.in/yaml.v3`: YAML configuration parsing
- `github.com/rwcarlsen/goexif`: EXIF metadata extraction (indirect)

## Development Guidelines

### Making Changes
- ALWAYS build and test after changes: `go build -o fileferry . && ./fileferry` (shows usage)
- Run full linting suite before committing
- Test with sample files to verify functionality
- Metadata extraction functionality depends on external tools

### Known Limitations
- EXIF extraction is incomplete (TODO comments in metadata.go)
- Video metadata extraction requires ffprobe
- No existing test suite - manual testing required
- Template variables may not resolve properly without metadata

### Timing Expectations
- Dependency download: ~0.5 seconds
- Build: ~6 seconds (NEVER CANCEL)
- Linting: ~2 seconds total
- External tool installation: ~25 seconds (NEVER CANCEL)

## Common Tasks

### Quick Development Cycle
```bash
# After making changes
go build -o fileferry .
gofmt -d .
go vet ./...
~/go/bin/staticcheck ./...
./fileferry /path/to/test_config.yaml
```

### Full Validation Cycle  
```bash
go mod tidy
go build -o fileferry .
gofmt -d .
go vet ./...
~/go/bin/staticcheck ./...
# Run manual test scenario (see Validation section)
```

### Repository Root Structure
```
.
├── .git/
├── .gitignore          # Excludes config.yaml and fileferry binary
├── config.go           # Configuration structure and loading
├── filesystem.go       # File operations and path handling  
├── go.mod             # Go module definition
├── go.sum             # Dependency checksums
├── main.go            # Application entry point
└── metadata.go        # Metadata extraction logic
```