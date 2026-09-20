package typesafe

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path"
	"strings"
)

//go:embed assets/typesafe-ai/*
var skillFiles embed.FS

func SkillHash() string {
	h := sha256.New()
	_ = fs.WalkDir(skillFiles, "assets/typesafe-ai", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := skillFiles.ReadFile(name)
		if err != nil {
			return err
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(data)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

const installedSkillPath = ".agents/skills/typesafe-ai"

// InstallSkill writes only to this run's checkout and refuses an existing skill.
// os.Root prevents a repository-provided symlink from escaping the checkout.
func InstallSkill(directory, runID string) error {
	if !safeID.MatchString(runID) {
		return errors.New("TYPESAFE_INVALID_RUN")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return errors.New("TYPESAFE_SKILL_UNAVAILABLE")
	}
	defer root.Close()
	if err = root.MkdirAll(".agents/skills", 0o755); err != nil {
		return errors.New("TYPESAFE_SKILL_CONFLICT")
	}
	if err = root.Mkdir(installedSkillPath, 0o755); err != nil {
		// A repository may already carry the exact pinned official skill. It
		// can be used without taking ownership or deleting it after the run.
		if existingSkillMatches(root, runID) {
			return nil
		}
		return errors.New("TYPESAFE_SKILL_CONFLICT")
	}
	success := false
	defer func() {
		if !success {
			_ = root.RemoveAll(installedSkillPath)
		}
	}()
	err = fs.WalkDir(skillFiles, "assets/typesafe-ai", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(name, "assets/typesafe-ai/")
		if err := root.MkdirAll(path.Dir(installedSkillPath+"/"+rel), 0o755); err != nil {
			return err
		}
		data, err := skillFiles.ReadFile(name)
		if err != nil {
			return err
		}
		file, err := root.OpenFile(installedSkillPath+"/"+rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		return errors.New("TYPESAFE_SKILL_UNAVAILABLE")
	}
	file, err := root.OpenFile(installedSkillPath+"/.repomesh-run", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("TYPESAFE_SKILL_UNAVAILABLE")
	}
	_, err = file.WriteString(runID)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return errors.New("TYPESAFE_SKILL_UNAVAILABLE")
	}
	success = true
	return nil
}

func existingSkillMatches(root *os.Root, runID string) bool {
	marker, err := root.ReadFile(installedSkillPath + "/.repomesh-run")
	if err == nil && string(marker) != runID {
		return false
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false
	}
	err = fs.WalkDir(skillFiles, "assets/typesafe-ai", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		expected, err := skillFiles.ReadFile(name)
		if err != nil {
			return err
		}
		actual, err := root.ReadFile(installedSkillPath + "/" + strings.TrimPrefix(name, "assets/typesafe-ai/"))
		if err != nil || string(actual) != string(expected) {
			return errors.New("skill differs")
		}
		return nil
	})
	return err == nil
}

func CleanupSkill(directory, runID string) error {
	if !safeID.MatchString(runID) {
		return errors.New("TYPESAFE_INVALID_RUN")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	data, err := root.ReadFile(installedSkillPath + "/.repomesh-run")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || string(data) != runID {
		return errors.New("TYPESAFE_SKILL_OWNERSHIP_MISMATCH")
	}
	return root.RemoveAll(installedSkillPath)
}

// RuntimePrompt is deliberately free of shell metacharacters: the legacy
// coordinator serializes this prompt into a quoted command.
const RuntimePrompt = `
Optional TypeSafe verification: if REPOMESH_TYPESAFE_GRANT is available, explicitly use the typesafe-ai skill at .agents/skills/typesafe-ai/SKILL.md. Read the official skill before using it. This environment supplies a governed RepoMesh tool instead of a raw TYPESAFE_API_KEY; do not request or print credentials and do not call TypeSafe directly.
After collecting the relevant real evidence, write a JSON request file with requestId (a unique stable ID), claims (one to eight objects with id and text), evidence (relevant real test output), and optionally commit (the full git commit). Use the executable named by REPOMESH_TYPESAFE_HELPER with arguments: typesafe evaluate <request-file>. It returns a recorded evaluation with supported, contradicted or insufficient judgments and probabilities. It sends only the supplied evidence and claims to TypeSafe. Keep secrets out of that text. A retry must reuse the same requestId and content.
The result is advisory: retain the actual test exit code and verdict even if Jev disagrees. Report unavailable or unknown honestly. Evidence and commit descriptions are supplied by you, not independently verified platform facts. Never change the requirement or fabricate evidence to obtain supported.
`

// PrepareScript is inserted after entering the checkout, for test runs only.
// An optional-tool setup failure must not prevent running the actual tests.
const PrepareScript = `if [ -n "${REPOMESH_TYPESAFE_HELPER:-}" ]; then
  if ! "$REPOMESH_TYPESAFE_HELPER" typesafe install; then
    unset REPOMESH_TYPESAFE_GRANT
  fi
fi
`
