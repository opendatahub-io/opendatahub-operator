package resolver

import (
	"bytes"
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/config"
)

// codeDefaultMarker tags a Go source comment that pins the first string
// literal of the following statement to an imageOverrides entry.
// resolve-digests rewrites the literal with the ODH image (base@digest)
// configured for that env var so code defaults cannot drift from
// manifests-config.yaml.
const codeDefaultMarker = "sync-digests:"

var (
	// codeDefaultMarkerComment matches the marker inside a line comment,
	// e.g. "// sync-digests: RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE".
	// It recognizes the sync-digests: prefix independently from its env
	// name field and captures the whole field so validation cannot be
	// satisfied by a prefix of it: a standalone directive with no field
	// is rejected by codeDefaultMarkerEnv instead of silently leaving the
	// file unmarked. Line-start anchoring is enforced separately by
	// codeDefaultAtLineStart so marker text in trailing comments, docs or
	// strings is never paired with a literal.
	codeDefaultMarkerComment = regexp.MustCompile(`^//[ \t]*` + codeDefaultMarker + `[ \t]*(\S*)`)
	// codeDefaultMarkerEnvName is the shape the marker field captured in
	// full must have: a field such as RELATED_IMAGE_PROXY_amd64 must be
	// rejected rather than resolved through its RELATED_IMAGE_PROXY
	// prefix.
	codeDefaultMarkerEnvName = regexp.MustCompile(`^RELATED_IMAGE_[A-Z0-9_]+$`)
	// codeDefaultRefPattern validates the current literal so a mispaired
	// marker cannot silently rewrite an unrelated string. Target refs are
	// validated more strictly by name.NewDigest in codeDefaultRef.
	codeDefaultRefPattern = regexp.MustCompile(`^[^\s"]+@sha256:[a-f0-9]{64}$`)

	// codeDefaultSkipDirs are never scanned for markers.
	codeDefaultSkipDirs = map[string]bool{
		".git":     true,
		"opt":      true,
		"vendor":   true,
		"testdata": true,
	}
)

// codeDefaultUpdate is a scanned Go file with its rewritten content,
// pending the atomic write done by applyCodeDefaultUpdates. orig holds
// the bytes the rewrite was computed from so the write can be aborted
// when the file changed in between.
type codeDefaultUpdate struct {
	path    string
	orig    []byte
	out     []byte
	changed bool
}

// codeDefaultSite is one marker and the string literal it pins.
type codeDefaultSite struct {
	envName          string
	literal          string
	litStart, litEnd int
}

// findCodeDefaultSites tokenizes data and pairs every line-start
// `// sync-digests: RELATED_IMAGE_*` marker with the first string literal
// of the statement that follows it. Tokenizing instead of quote matching
// keeps quoted text in intervening comments from being mistaken for the
// pinned literal. A semicolon — explicit or auto-inserted at a line end —
// closes the marked statement, so a literal in a later statement or
// declaration is out of scope and never paired with the marker. A marker
// whose statement ends without a string literal, or a file ending with
// one pending, is an error.
func findCodeDefaultSites(path string, data []byte) ([]codeDefaultSite, error) {
	fset := token.NewFileSet()
	file := fset.AddFile(path, fset.Base(), len(data))
	var s scanner.Scanner
	s.Init(file, data, nil, scanner.ScanComments)

	var sites []codeDefaultSite
	pending := -1 // index of the site still waiting for its literal
	for {
		pos, tok, lit := s.Scan()
		switch tok {
		case token.EOF:
			if pending >= 0 {
				return nil, fmt.Errorf("%s: %s marker without a string literal to rewrite", path, codeDefaultMarker)
			}
			return sites, nil
		case token.COMMENT:
			envName, isMarker, err := codeDefaultMarkerEnv(file, data, pos, lit)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			if !isMarker {
				continue
			}
			if pending >= 0 {
				return nil, fmt.Errorf("%s: %s marker without a string literal to rewrite", path, codeDefaultMarker)
			}
			sites = append(sites, codeDefaultSite{envName: envName})
			pending = len(sites) - 1
		case token.SEMICOLON:
			// The marked statement ended without a string literal: a
			// literal belonging to a later statement or declaration is
			// out of scope and must not be paired with the marker.
			if pending >= 0 {
				return nil, fmt.Errorf("%s: %s marker without a string literal to rewrite", path, codeDefaultMarker)
			}
		case token.STRING:
			if pending < 0 {
				continue
			}
			sites[pending].literal = lit
			sites[pending].litStart = file.Offset(pos)
			sites[pending].litEnd = sites[pending].litStart + len(lit)
			pending = -1
		default:
			// Only comments, string literals, semicolons and EOF affect
			// pairing.
		}
	}
}

// codeDefaultMarkerEnv reports whether the comment token lit is a marker
// that starts its line, returning the env var it names. The sync-digests:
// prefix is recognized independently from the env name field: a
// standalone directive with no field is an error, and the whole field is
// consumed before it is validated so a field such as
// RELATED_IMAGE_PROXY_amd64 is rejected instead of being resolved
// through its RELATED_IMAGE_PROXY prefix.
func codeDefaultMarkerEnv(file *token.File, data []byte, pos token.Pos, lit string) (string, bool, error) {
	if !strings.HasPrefix(lit, "//") {
		return "", false, nil
	}
	if !codeDefaultAtLineStart(data, file.Offset(pos)) {
		return "", false, nil
	}
	m := codeDefaultMarkerComment.FindStringSubmatch(lit)
	if m == nil {
		return "", false, nil
	}
	if m[1] == "" {
		return "", true, fmt.Errorf("%s marker without a RELATED_IMAGE_* env var name", codeDefaultMarker)
	}
	if !codeDefaultMarkerEnvName.MatchString(m[1]) {
		return "", true, fmt.Errorf("%s marker names %s, want a RELATED_IMAGE_* env var of uppercase letters, digits and underscores",
			codeDefaultMarker, strconv.Quote(m[1]))
	}
	return m[1], true, nil
}

// codeDefaultAtLineStart reports whether offset is preceded on its line
// only by spaces and tabs, i.e. the token at offset starts a line.
func codeDefaultAtLineStart(data []byte, offset int) bool {
	for i := offset - 1; i >= 0; i-- {
		switch data[i] {
		case '\n':
			return true
		case ' ', '\t':
		default:
			return false
		}
	}
	return true
}

// codeDefaultLiteralValue strips the double quotes from a Go string
// token. Escapes and raw string literals are not supported: pinned
// image refs never need them.
func codeDefaultLiteralValue(lit string) (string, error) {
	if len(lit) < 2 || lit[0] != '"' || lit[len(lit)-1] != '"' {
		return "", fmt.Errorf("pinned literal must be a double-quoted string, got %s", lit)
	}
	if strings.Contains(lit, `\`) {
		return "", fmt.Errorf("escapes in pinned literals are not supported, got %s", lit)
	}
	return lit[1 : len(lit)-1], nil
}

// collectCodeDefaults walks root for Go files carrying a
// `// sync-digests: RELATED_IMAGE_*` marker and computes the rewritten
// content for each, without touching the filesystem. Callers validate all
// markers (and resolve all references) this way BEFORE persisting the
// config the markers were validated against, so a failed validation
// leaves everything untouched.
//
// A marker naming an env var missing from imageOverrides, a marker with
// no rewritable literal, or a literal that does not look like a
// digest-pinned image ref is an error: staying loud is better than
// silently letting the code default drift again.
func collectCodeDefaults(cfg *config.ManifestsConfig, root string) ([]codeDefaultUpdate, error) {
	// Validate the root before walking: a symlinked root is rejected —
	// following it would silently scan whatever the link points at — and
	// a non-directory root would otherwise silently scan nothing.
	// filepath.Clean strips trailing separators first: Lstat on a path
	// ending in "/" resolves the link and reports its target, which
	// would let a symlinked root through the check below. Real
	// directories keep working with or without trailing separators.
	// Symlinked entries found inside the scanned tree are skipped by the
	// !d.Type().IsRegular() guard below.
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("stating code defaults root %s: %w", root, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("code defaults root %s is a symlink; pass the directory it points at instead", root)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("code defaults root %s is not a directory", root)
	}

	var updates []codeDefaultUpdate
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if codeDefaultSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip non-regular entries such as symlinks: rewriting through a
		// symlink would silently replace whatever it points at, and other
		// irregular types can block reads.
		if !d.Type().IsRegular() {
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			// Unreadable Go files fail the sync before the config is
			// saved: their markers cannot be validated, and silently
			// skipping such a file could let a code default drift.
			return err
		}
		if !bytes.Contains(data, []byte(codeDefaultMarker)) {
			return nil
		}

		out, fileChanged, err := syncCodeDefaultsIn(cfg, data, path)
		if err != nil {
			return err
		}
		updates = append(updates, codeDefaultUpdate{path: path, orig: data, out: out, changed: fileChanged})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	return updates, nil
}

// applyCodeDefaultUpdates is the second half of collectCodeDefaults: it
// atomically writes every changed file and reports whether at least one
// write happened. Run it only after the config the updates were computed
// from has been persisted.
func applyCodeDefaultUpdates(updates []codeDefaultUpdate) (bool, error) {
	changed := false
	for _, u := range updates {
		if !u.changed {
			continue
		}
		// The file must still be byte-identical to what was collected:
		// a concurrent edit would otherwise be silently clobbered.
		cur, err := os.ReadFile(u.path)
		if err != nil {
			return false, fmt.Errorf("re-reading %s: %w", u.path, err)
		}
		if !bytes.Equal(cur, u.orig) {
			return false, fmt.Errorf("%s changed while the sync was running, leaving it untouched; rerun the sync", u.path)
		}
		if err := writeFileAtomic(u.path, u.out); err != nil {
			return false, fmt.Errorf("writing %s: %w", u.path, err)
		}
		changed = true
		slog.Info("Synced code default image", slog.String("file", u.path))
	}
	return changed, nil
}

// writeFileAtomic replaces path with data via a temporary file in the same
// directory plus rename, so a crash mid-write cannot truncate the source
// file. The original file mode is preserved.
func writeFileAtomic(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return fmt.Errorf("setting temp file mode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("renaming into place: %w", err)
	}
	return nil
}

// syncCodeDefaultsIn rewrites every marked literal in data and returns the
// new content and whether anything changed.
func syncCodeDefaultsIn(cfg *config.ManifestsConfig, data []byte, path string) ([]byte, bool, error) {
	sites, err := findCodeDefaultSites(path, data)
	if err != nil {
		return nil, false, err
	}
	if len(sites) == 0 {
		return nil, false, nil
	}

	var buf bytes.Buffer
	last := 0
	changed := false
	for _, site := range sites {
		envName := site.envName
		current, err := codeDefaultLiteralValue(site.literal)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %s %s: %w", path, codeDefaultMarker, envName, err)
		}

		want, err := codeDefaultRef(cfg, envName)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", path, err)
		}
		if !codeDefaultRefPattern.MatchString(current) {
			return nil, false, fmt.Errorf("%s: literal after %s %s marker is %q, want a digest-pinned image ref",
				path, codeDefaultMarker, envName, current)
		}

		buf.Write(data[last:site.litStart])
		buf.WriteString(strconv.Quote(want))
		last = site.litEnd
		if current != want {
			changed = true
			slog.Info("Updating code default image",
				slog.String("env", envName),
				slog.String("file", path),
				slog.String("old", current),
				slog.String("new", want))
		}
	}
	buf.Write(data[last:])

	return buf.Bytes(), changed, nil
}

// codeDefaultRef builds the digest-pinned ref configured for envName on the
// ODH platform.
func codeDefaultRef(cfg *config.ManifestsConfig, envName string) (string, error) {
	override, ok := cfg.ImageOverrides[envName]
	if !ok {
		return "", fmt.Errorf("%s %s: env var not found in imageOverrides", codeDefaultMarker, envName)
	}
	img := override.PlatformImage("odh")
	if img == nil || !img.HasValidDigest() || img.Base == "" {
		return "", fmt.Errorf("%s %s: no ODH base+digest pinned in imageOverrides", codeDefaultMarker, envName)
	}
	ref := img.Base + "@" + img.Digest
	// name.NewDigest's repository check is a character class and accepts
	// repeated slashes, so reject empty path components explicitly.
	if slices.Contains(strings.Split(img.Base, "/"), "") {
		return "", fmt.Errorf("%s %s: imageOverrides resolves to invalid ref %s: empty path component in base",
			codeDefaultMarker, envName, strconv.Quote(ref))
	}
	// Validate the complete target — base and digest — with the container
	// name parser: codeDefaultRefPattern alone would accept a well-shaped
	// but malformed base (e.g. uppercase or illegal characters).
	if _, err := name.NewDigest(ref); err != nil {
		return "", fmt.Errorf("%s %s: imageOverrides resolves to invalid ref %s: %w",
			codeDefaultMarker, envName, strconv.Quote(ref), err)
	}
	return ref, nil
}
