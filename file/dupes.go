package file

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ffcfg "github.com/dkarlovi/fileferry/config"
)

// TargetRoot returns the fixed directory prefix of a target path template: the
// part before the first template token, cut back to a whole directory. For
// "/photos/{meta.taken.year}/{meta.taken.date}/{x}.jpg" that is "/photos"; for
// "/photos/set-{meta.taken.year}/{x}.jpg" it is "/photos", because "set-" is
// only part of a directory name and not a directory of its own.
//
// This is the tree a profile owns: everything the profile has ever written
// lives under it, so it is what maintenance commands scan.
func TargetRoot(tmpl string) string {
	prefix := tmpl
	if i := strings.IndexByte(prefix, '{'); i >= 0 {
		prefix = prefix[:i]
	}
	if prefix == "" {
		return ""
	}
	if os.IsPathSeparator(prefix[len(prefix)-1]) {
		trimmed := strings.TrimRight(prefix, string(os.PathSeparator))
		if trimmed == "" {
			return string(os.PathSeparator)
		}
		return trimmed
	}
	return filepath.Dir(prefix)
}

// KeepReason explains why one file of a duplicate set was chosen to survive.
type KeepReason string

const (
	// KeepCanonical means the kept file already sits exactly where the current
	// config's target template says this content belongs, so the others are
	// redundant copies under other names.
	KeepCanonical KeepReason = "canonical"
	// KeepFallback means no copy sits at its target path — either the config
	// changed since they were written, or their metadata no longer resolves. One
	// copy is kept by name order and the set is reported as needing a rename.
	KeepFallback KeepReason = "fallback"
)

// DupeSet is a group of byte-identical files sitting in the same directory of a
// profile's target tree. Because the target path is derived from the file's own
// content and metadata, identical content always resolves to the same
// directory, so duplicates can only ever be siblings.
type DupeSet struct {
	Dir  string
	Size int64
	Hash string
	// Canonical is where the current config says this content belongs, or ""
	// when it cannot be derived (unreadable metadata, changed template).
	Canonical string
	// Keep is the copy that survives; Delete holds the redundant ones, sorted.
	Keep   string
	Delete []string
	Reason KeepReason
}

// ProfileDupes is the outcome of scanning a single profile's target tree.
type ProfileDupes struct {
	Profile string
	Root    string
	// Scanned is how many files of the profile's types were considered.
	Scanned int
	Sets    []DupeSet
	// Errors are non-fatal problems (an unreadable file, a missing root) that
	// did not stop the scan.
	Errors []error
}

// FindDupes scans the target tree of each profile (or only profileName, when
// non-empty) and reports sets of byte-identical sibling files.
//
// The scan is deliberately cheap before it is thorough: files are first grouped
// by directory and size, and only same-size siblings — a tiny fraction of a
// media library — are hashed. Nothing is modified; deciding what to do with a
// set is the caller's job.
func FindDupes(cfg *ffcfg.Config, profileName string) ([]ProfileDupes, error) {
	if profileName != "" {
		if _, ok := cfg.Profiles[profileName]; !ok {
			return nil, fmt.Errorf("profile %q not found in config", profileName)
		}
	}

	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		if profileName != "" && name != profileName {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var out []ProfileDupes
	for _, name := range names {
		out = append(out, findProfileDupes(cfg, name))
	}
	return out, nil
}

func findProfileDupes(cfg *ffcfg.Config, profileName string) ProfileDupes {
	prof := cfg.Profiles[profileName]
	root := TargetRoot(prof.Target.Path)
	res := ProfileDupes{Profile: profileName, Root: root}
	if root == "" {
		res.Errors = append(res.Errors, fmt.Errorf("profile %q: target path %q has no fixed directory prefix to scan", profileName, prof.Target.Path))
		return res
	}

	types := profileTypes(prof)
	if len(types) == 0 {
		res.Errors = append(res.Errors, fmt.Errorf("profile %q: no source types configured, nothing to scan", profileName))
		return res
	}

	// Group candidates by (directory, size): identical content is always the
	// same size, and always belongs in the same directory.
	type groupKey struct {
		dir  string
		size int64
	}
	groups := map[groupKey][]string{}
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// A single unreadable directory must not abort the whole tree.
			res.Errors = append(res.Errors, err)
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		if !isFileType(path, types) {
			return nil
		}
		res.Scanned++
		key := groupKey{dir: filepath.Dir(path), size: info.Size()}
		groups[key] = append(groups[key], path)
		return nil
	})
	if walkErr != nil {
		res.Errors = append(res.Errors, fmt.Errorf("scan %s: %w", root, walkErr))
		return res
	}

	keys := make([]groupKey, 0, len(groups))
	for key, paths := range groups {
		if len(paths) > 1 {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].dir != keys[j].dir {
			return keys[i].dir < keys[j].dir
		}
		return keys[i].size < keys[j].size
	})

	for _, key := range keys {
		paths := groups[key]
		sort.Strings(paths)

		// Same size is only a hint; content decides.
		byHash := map[string][]string{}
		var hashOrder []string
		for _, p := range paths {
			sum, err := hashFile(p)
			if err != nil {
				res.Errors = append(res.Errors, fmt.Errorf("hash %s: %w", p, err))
				continue
			}
			if _, seen := byHash[sum]; !seen {
				hashOrder = append(hashOrder, sum)
			}
			byHash[sum] = append(byHash[sum], p)
		}

		for _, sum := range hashOrder {
			dupes := byHash[sum]
			if len(dupes) < 2 {
				continue
			}
			res.Sets = append(res.Sets, buildDupeSet(cfg, profileName, prof, key.dir, key.size, sum, dupes))
		}
	}

	return res
}

// buildDupeSet decides which copy of a set survives. The rule mirrors what a
// fresh `run` of the current config would produce: the copy that already sits
// where the config says this content belongs stays, and every other copy goes.
// When no copy is where the config wants it (typically because the target
// template changed since they were written), one is kept by name order and the
// set is flagged as still needing a rename.
func buildDupeSet(cfg *ffcfg.Config, profileName string, prof ffcfg.ProfileConfig, dir string, size int64, sum string, dupes []string) DupeSet {
	set := DupeSet{Dir: dir, Size: size, Hash: sum, Reason: KeepFallback}
	set.Canonical = setCanonical(cfg, profileName, prof, dupes)

	if set.Canonical != "" {
		for _, p := range dupes {
			if sameFilePath(p, set.Canonical) {
				set.Keep = p
				set.Reason = KeepCanonical
				break
			}
		}
	}
	if set.Keep == "" {
		set.Keep = dupes[0]
	}
	for _, p := range dupes {
		if p != set.Keep {
			set.Delete = append(set.Delete, p)
		}
	}
	return set
}

// setCanonical resolves the single path the current config wants this content
// at, or "" when that cannot be settled.
//
// The copies are byte-identical, so their content answers the question the same
// way whichever one is read — that reading is therefore authoritative and is
// tried first. Filename patterns are only consulted when the content says
// nothing, and then only if every copy agrees: differing filenames that each
// parse cleanly are exactly the case where the names cannot be trusted to pick
// a winner, since at most one of them can be right.
func setCanonical(cfg *ffcfg.Config, profileName string, prof ffcfg.ProfileConfig, dupes []string) string {
	for _, p := range dupes {
		if canonical, err := canonicalPath(cfg, profileName, prof, p, fromContent); err == nil {
			return canonical
		}
	}

	agreed := ""
	for _, p := range dupes {
		canonical, err := canonicalPath(cfg, profileName, prof, p, fromFilename)
		if err != nil {
			continue
		}
		if agreed == "" {
			agreed = canonical
		} else if !sameFilePath(agreed, canonical) {
			return ""
		}
	}
	return agreed
}

// metadataSource selects which evidence canonicalPath is allowed to use.
type metadataSource int

const (
	// fromContent ignores filename patterns, forcing metadata out of the file
	// itself (EXIF, ffprobe).
	fromContent metadataSource = iota
	// fromFilename allows the profile's filename patterns, exactly as a run does.
	fromFilename
)

// canonicalPath resolves where the current config would put the file at path,
// reusing the same metadata extraction and template rendering a `run` performs
// — that is what makes the kept file indistinguishable from one this config had
// just processed.
func canonicalPath(cfg *ffcfg.Config, profileName string, prof ffcfg.ProfileConfig, path string, from metadataSource) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	entry := &localEntry{path: path, info: info}
	src := ffcfg.SourceConfig{Path: filepath.Dir(path), Filenames: profileFilenames(prof)}
	if from == fromContent {
		// Strip every filename pattern so processFile cannot take its fast path
		// and must read the file. Only this profile is looked up, so a one-entry
		// config is enough.
		src.Filenames = nil
		prof.Patterns = nil
		cfg = &ffcfg.Config{Profiles: map[string]ffcfg.ProfileConfig{profileName: prof}}
	}
	file := processFile(entry, src, profileName, cfg)
	if file.Error != nil {
		return "", file.Error
	}
	return file.NewPath, nil
}

func sameFilePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return absA == absB
}

// profileTypes is the union of the types the profile's sources claim: exactly
// the kinds of file the profile could have written into its target tree.
func profileTypes(prof ffcfg.ProfileConfig) []string {
	var types []string
	seen := map[string]bool{}
	for _, src := range prof.Sources {
		for _, t := range src.Types {
			if !seen[t] {
				seen[t] = true
				types = append(types, t)
			}
		}
	}
	return types
}

// profileFilenames is the union of the per-source filename patterns, so a file
// in the target tree is offered the same filename parsing its source had.
func profileFilenames(prof ffcfg.ProfileConfig) []string {
	var pats []string
	seen := map[string]bool{}
	for _, src := range prof.Sources {
		for _, p := range src.Filenames {
			if !seen[p] {
				seen[p] = true
				pats = append(pats, p)
			}
		}
	}
	return pats
}
