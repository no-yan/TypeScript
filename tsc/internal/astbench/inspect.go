package astbench

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Inspection struct {
	ArtifactSet    string `json:"artifact_set"`
	IdentityStatus string `json:"identity_status"`
	StageStatus    string `json:"stage_status"`
	Reason         string `json:"reason,omitempty"`
}

func Inspect(repo, artifacts, pattern string) ([]Inspection, error) {
	if repo == "" || artifacts == "" {
		return nil, errors.New("inspect requires repo and artifacts")
	}
	repo, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	rev, err := gitRevision(repo, "HEAD")
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(artifacts)
	if errors.Is(err, os.ErrNotExist) {
		return []Inspection{{artifacts, "missing", "missing", "artifact path does not exist"}}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("artifacts must be a directory")
	}
	roots := []string{artifacts}
	if _, err := os.Stat(filepath.Join(artifacts, "identity.json")); errors.Is(err, os.ErrNotExist) {
		entries, err := os.ReadDir(artifacts)
		if err != nil {
			return nil, err
		}
		var found []string
		for _, e := range entries {
			if e.IsDir() {
				candidate := filepath.Join(artifacts, e.Name())
				if _, err := os.Stat(filepath.Join(candidate, "identity.json")); err == nil {
					found = append(found, candidate)
				}
			}
		}
		if len(found) > 0 {
			roots = found
		}
	}
	var out []Inspection
	for _, root := range roots {
		x := Inspection{ArtifactSet: root, IdentityStatus: "missing", StageStatus: "unsupported"}
		raw, err := os.ReadFile(filepath.Join(root, "identity.json"))
		if err == nil {
			var id map[string]json.RawMessage
			if err := json.Unmarshal(raw, &id); err != nil {
				return nil, fmt.Errorf("%s identity: %w", root, err)
			}
			var gotRepo, gotRev string
			var tsgolint *string
			_ = json.Unmarshal(id["repo_root"], &gotRepo)
			_ = json.Unmarshal(id["typescript_go_git_rev"], &gotRev)
			tg, hasTG := id["tsgolint_git_rev"]
			tgErr := json.Unmarshal(tg, &tsgolint)
			x.IdentityStatus = "stale"
			if gotRepo == repo && gotRev == rev && hasTG && tgErr == nil && tsgolint == nil {
				x.IdentityStatus = "current"
			} else {
				x.Reason = "repo_root or revisions differ/missing; historical evidence only"
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		matched := false
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if d.Name() == "snapshots" || d.Name() == "build" {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() != "bench.txt" {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 4096), 1024*1024)
			for scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) >= 4 && strings.HasPrefix(fields[0], "Benchmark") && re.MatchString(fields[0]) {
					matched = true
				}
			}
			return scanner.Err()
		})
		if err != nil {
			return nil, err
		}
		if matched {
			x.StageStatus = x.IdentityStatus
		} else {
			if x.Reason != "" {
				x.Reason += "; "
			}
			x.Reason += "no Benchmark line matches requested regex"
		}
		out = append(out, x)
	}
	return out, nil
}
