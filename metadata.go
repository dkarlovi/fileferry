package main

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type FileMetadata struct {
	TakenTime   *time.Time
	Extension   string
	CameraMaker string
	CameraModel string
}

type FilenameMetaRule struct {
	Path string
	Exp  string
}

var FilenameMetaRules = []FilenameMetaRule{
	{Path: "meta.taken.date", Exp: `\d{4}-\d{2}-\d{2}`},
	{Path: "meta.taken.time", Exp: `\d{2}-\d{2}-\d{2}`},
}

// extractImageMetadata should be implemented elsewhere, but for now provide a stub.
func extractImageMetadata(path string) (*FileMetadata, error) {
	// TODO: Implement actual EXIF extraction logic
	meta := &FileMetadata{
		Extension: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
	return meta, nil
}

func extractVideoMetadata(path string) (*FileMetadata, error) {
	meta := &FileMetadata{
		Extension: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
	// Call ffprobe to get all format tags
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", path)
	out, err := cmd.Output()
	if err != nil {
		return meta, nil // Return what we have, but no date
	}
	var ffprobe struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &ffprobe); err != nil {
		return meta, nil
	}
	tags := ffprobe.Format.Tags

	// Helper to look up the first non-empty value from a list of possible tag keys
	lookup := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := tags[k]; ok && v != "" {
				return v
			}
		}
		return ""
	}

	// Arrays of possible tag keys for each field
	makerKeys := []string{"com.android.manufacturer", "make", "manufacturer"}
	modelKeys := []string{"com.android.model", "model"}
	creationTimeKeys := []string{"creation_time"}

	meta.CameraMaker = lookup(makerKeys...)
	meta.CameraModel = lookup(modelKeys...)

	ct := lookup(creationTimeKeys...)
	if ct != "" {
		layouts := []string{
			time.RFC3339,
			"2006-01-02T15:04:05.000000Z",
			"2006-01-02 15:04:05",
		}
		var tm time.Time
		var parseErr error
		for _, layout := range layouts {
			tm, parseErr = time.Parse(layout, ct)
			if parseErr == nil {
				localTm := tm.Local()
				meta.TakenTime = &localTm
				break
			}
		}
	}
	return meta, nil
}

func parseMetadataFromFilenamePattern(filename, pattern string) *FileMetadata {
	ext := filepath.Ext(filename)
	// We'll match against the full filename (including extension) so patterns that include
	// the extension (e.g. "{meta.taken.date} {meta.taken.time}.mkv") work as expected.
	input := filename
	// Build a regex from the pattern and the rules
	regexPattern := pattern
	// Replace pattern tokens with named groups; group names must be valid identifiers,
	// so convert dots to underscores (meta.taken.date -> meta_taken_date) and map back later.
	groupMap := make(map[string]string) // groupName -> original path
	for _, rule := range FilenameMetaRules {
		grp := strings.ReplaceAll(rule.Path, ".", "_")
		regexPattern = strings.ReplaceAll(regexPattern, "{"+rule.Path+"}", "(?P<"+grp+">"+rule.Exp+")")
		groupMap[grp] = rule.Path
	}
	// Anchor the pattern to match the full string
	regexPattern = "^" + regexPattern + "$"
	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return nil
	}
	match := re.FindStringSubmatch(input)
	if match == nil {
		return nil
	}
	groups := make(map[string]string)
	for i, n := range re.SubexpNames() {
		if i > 0 && n != "" {
			// Map group name back to dotted path if we converted it
			if orig, ok := groupMap[n]; ok {
				groups[orig] = match[i]
			} else {
				groups[n] = match[i]
			}
		}
	}
	meta := &FileMetadata{
		Extension: strings.TrimPrefix(ext, "."),
	}
	if date, ok := groups["meta.taken.date"]; ok {
		if t, ok2 := groups["meta.taken.time"]; ok2 {
			// the pattern uses hyphens in time (15-04-05)
			tm, err := time.ParseInLocation("2006-01-02 15-04-05", date+" "+t, time.Local)
			if err == nil {
				meta.TakenTime = &tm
			}
		} else {
			tm, err := time.ParseInLocation("2006-01-02", date, time.Local)
			if err == nil {
				meta.TakenTime = &tm
			}
		}
	}
	// Add more fields as needed
	if meta.TakenTime != nil {
		return meta
	}
	return nil
}
