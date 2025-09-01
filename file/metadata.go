package file

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

func extractImageMetadata(path string) (*FileMetadata, error) {
	meta := &FileMetadata{
		Extension: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
	return meta, nil
}

func extractVideoMetadata(path string) (*FileMetadata, error) {
	meta := &FileMetadata{
		Extension: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", path)
	out, err := cmd.Output()
	if err != nil {
		return meta, nil
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

	lookup := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := tags[k]; ok && v != "" {
				return v
			}
		}
		return ""
	}

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
	input := filename
	regexPattern := pattern
	groupMap := make(map[string]string)
	for _, rule := range FilenameMetaRules {
		grp := strings.ReplaceAll(rule.Path, ".", "_")
		regexPattern = strings.ReplaceAll(regexPattern, "{"+rule.Path+"}", "(?P<"+grp+">"+rule.Exp+")")
		groupMap[grp] = rule.Path
	}
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
	if meta.TakenTime != nil {
		return meta
	}
	return nil
}
