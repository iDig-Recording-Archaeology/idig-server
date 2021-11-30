package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Repository struct {
	gitDir  string
	workDir string
}

// If `dir` doesn't exist, we initializes a new one
func OpenRepository(dir string) (*Repository, error) {
	if !fileExists(dir) {
		if err := run("git", "init", "--bare", dir); err != nil {
			return nil, err
		}
	}

	// We clone the repository to a temp location
	tmpDir, err := os.MkdirTemp("", "idig-server")
	if err != nil {
		return nil, err
	}
	workDir := filepath.Join(tmpDir, filepath.Base(dir))
	repo := &Repository{
		gitDir:  dir,
		workDir: workDir,
	}

	err = run("git", "clone", dir, workDir)
	return repo, err
}

func (repo *Repository) Close() error {
	return os.RemoveAll(repo.workDir)
}

func (repo *Repository) Head() string {
	head, err := repo.gitOutput("rev-parse", "HEAD")
	if err != nil || head == "HEAD" {
		return ""
	}
	return head
}

func (repo *Repository) Commit(uid, username, message string, surveys map[string]Survey) (string, error) {
	if err := repo.removeSurveys(); err != nil {
		return "", err
	}
	for id, survey := range surveys {
		if err := repo.writeSurvey(id, survey); err != nil {
			return "", err
		}
	}

	err := repo.git("add", "--all")
	if err != nil {
		return "", err
	}

	// Make sure there are changes to commit
	err = repo.git("diff-index", "--quiet", "HEAD")
	if err == nil {
		return "", nil // nothing to commit
	}

	author := fmt.Sprintf("%s <%s>", username, uid)
	err = repo.git("commit", "--quiet", "--allow-empty-message",
		"--author", author, "--message", message)
	if err != nil {
		return "", err
	}
	err = repo.git("push", "--quiet")
	return repo.Head(), err
}

func (repo *Repository) ReadSurveys() (map[string]Survey, error) {
	pattern := filepath.Join(repo.workDir, "*.survey")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	surveys := make(map[string]Survey)
	for _, name := range matches {
		survey, err := repo.readSurvey(name)
		if err != nil {
			return nil, err
		}
		id, exists := survey["IdentifierUUID"]
		if !exists {
			return nil, fmt.Errorf("Encountered invalid survey: %s", name)
		}
		surveys[id] = survey
	}
	return surveys, nil
}

func (repo *Repository) OpenAttachment(name string) (*os.File, error) {
	file := filepath.Join(repo.gitDir, "attachments", name)
	return os.Open(file)
}

func (repo *Repository) WriteAttachment(name string, r io.ReadCloser) error {
	dir := filepath.Join(repo.gitDir, "attachments")
	os.MkdirAll(dir, 0o755)
	attachment := filepath.Join(dir, name)
	f, err := os.Create(attachment)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (repo *Repository) ExistsAttachment(name string) bool {
	file := filepath.Join(repo.gitDir, "attachments", name)
	return fileExists(file)
}

func (repo *Repository) removeSurveys() error {
	pattern := filepath.Join(repo.workDir, "*.survey")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, name := range matches {
		if err := os.Remove(name); err != nil {
			return err
		}
	}
	return nil
}

func (repo *Repository) readSurvey(name string) (Survey, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var s Survey
	dec := json.NewDecoder(f)
	err = dec.Decode(&s)
	return s, err
}

func (repo *Repository) writeSurvey(id string, survey Survey) error {
	name := filepath.Join(repo.workDir, id+".survey")
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(survey)
}

func (repo *Repository) git(args ...string) error {
	gitArgs := []string{"git", "-C", repo.workDir}
	gitArgs = append(gitArgs, args...)
	return run(gitArgs...)
}

func (repo *Repository) gitOutput(args ...string) (string, error) {
	gitArgs := append([]string{"-C", repo.workDir}, args...)
	cmd := exec.Command("git", gitArgs...)
	out, err := cmd.Output()
	s := string(bytes.TrimSpace(out))
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		err = fmt.Errorf("%s", string(exitErr.Stderr))
	}
	return s, err
}

func fileExists(name string) bool {
	_, err := os.Stat(name)
	return !os.IsNotExist(err)
}

func run(args ...string) error {
	// log.Printf("RUN %v", args)
	cmd := exec.Command(args[0], args[1:]...)
	stdout, err := cmd.Output()
	out := strings.TrimSpace(string(stdout))

	// Log any output
	if len(out) > 0 {
		for _, line := range strings.Split(out, "\n") {
			log.Println(line)
		}
	}

	if err != nil {
		err = fmt.Errorf("%s", out)
	}
	return err
}
