## FileFerry — media organizer (short)

FileFerry is a small CLI that organizes image and video files into target folders using metadata and filename patterns.

### What it does
- Scan one or more source directories (profiles) for media files.
- Extract metadata from filenames, EXIF (images) or ffprobe (videos) when available.
- Render a per-profile target path template and move files (dry-run by default).

### Quick examples
Build and dry-run with your config file:

```bash
go build -o fileferry .
./fileferry config.yaml      # dry-run: shows what would move
./fileferry config.yaml --ack # actually move files
```

Example (anonymized) `config.yaml` excerpt:

```yaml
profiles:
  Videos:
    sources:
      - path: /path/to/videos
        recurse: true
        types: [video]
    patterns:
      - "{meta.taken.date} {meta.taken.time}.mkv"
      - "{meta.taken.date}-{meta.taken.time}.mkv"
    target:
      path: /organized/videos/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.{file.extension}

  Pictures:
    sources:
      - path: /path/to/pictures
        recurse: false
        types: [image]
    target:
      path: /organized/pictures/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.{file.extension}
```

### Config contract (short)
- `profiles` is a map of profile names -> profile config.
- A `ProfileConfig` contains: `sources` (list), optional `patterns` (filename patterns used to extract metadata), and `target.path` (template used to build destination path).
- `SourceConfig` has `path`, `recurse`, `types` and optional `filenames`.

### Template variables
- `{meta.taken.year}`, `{meta.taken.date}`, `{meta.taken.datetime}`
- `{meta.camera.maker}`, `{meta.camera.model}`
- `{file.extension}` (no leading dot)

Notes: filename patterns are anchored and must match the filename exactly (e.g. `2025-06-02 15-21-02.mkv`). Patterns support tokens like `{meta.taken.date}` and `{meta.taken.time}` which map to regex rules.

### Custom format specifiers
Some tokens support custom format specifiers to match different time formats. Format specifiers are specified after a colon in the token (e.g., `{meta.taken.time:hhmmss}`).

Supported format specifiers for `{meta.taken.time}`:
- `hh-mm-ss` (default): Time in format `HH-MM-SS` (e.g., `22-22-12`)
- `hhmmss`: Time in compact format `HHMMSS` (e.g., `222212`)
- `hhmm`: Time in hour-minute format `HHMM` (e.g., `1530`)

Example using custom format specifiers:
```yaml
profiles:
  Pictures:
    sources:
      - path: /path/to/pictures
        recurse: false
        types: [image]
    patterns:
      - "Still {meta.taken.date} {meta.taken.time:hhmmss}_1.1.1.jpg"
    target:
      path: /organized/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.{file.extension}
```
This pattern matches filenames like `Still 2026-01-23 222212_1.1.1.jpg` where the time `222212` represents `22:22:12`.

### Build & lint

```bash
go build -o fileferry .
gofmt -d .
go vet ./...
```

### External tools (optional)
- `ffprobe` (from ffmpeg) improves video metadata extraction.
- `exiftool` improves image metadata extraction.

### Validation
- The config loader validates that each profile has a non-empty `target.path` and that source paths are unique across profiles.

Short and to the point — see the source and `config.yaml` for details.
