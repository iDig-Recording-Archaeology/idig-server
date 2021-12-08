package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
)

type Server struct {
	Root string
}

type PushRequest struct {
	UID      string            `json:"uid"`
	UserName string            `json:"username"`
	Message  string            `json:"message"`
	Head     string            `json:"head"`
	Surveys  map[string]Survey `json:"surveys"`
}

type PushResponse struct {
	Status  string   `json:"status"`
	Version string   `json:"version"`
	Missing []string `json:"missing,omitempty"`
	Updates []Patch  `json:"updates,omitempty"`
}

const (
	StatusOK       = "ok"
	StatusPushed   = "pushed"
	StatusConflict = "conflict"
	StatusMissing  = "missing"
)

type PullResponse struct {
	Version string  `json:"version"`
	Updates []Patch `json:"updates"`
}

type Change struct {
	Key string `json:"key"`
	Old string `json:"old"`
	New string `json:"new"`
}

type Patch struct {
	Id  string `json:"id"`
	Old Survey `json:"old"`
	New Survey `json:"new"`
}

type Survey map[string]string

func (s Survey) IsEqual(t Survey) bool {
	keys := s.Keys()
	keys.FormUnion(t.Keys())
	for key := range keys {
		if s[key] != t[key] {
			return false
		}
	}
	return true
}

func (s Survey) Keys() Set {
	keys := make(Set, len(s))
	for key := range s {
		keys[key] = struct{}{}
	}
	return keys
}

type Version map[string]Survey

func (v Version) Keys() Set {
	keys := make(Set, len(v))
	for key := range v {
		keys[key] = struct{}{}
	}
	return keys
}

type Set map[string]struct{}

func (s Set) FormUnion(a Set) {
	for k := range a {
		s[k] = struct{}{}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("> %s %s", r.Method, r.URL)
	code, err := s.serve(w, r)
	if err != nil {
		log.Printf("< %d %s", code, err)
		http.Error(w, err.Error(), code)
	} else {
		log.Printf("< %d OK", code)
		if code != http.StatusOK {
			w.WriteHeader(code)
		}
	}
}

func (s Server) serve(w http.ResponseWriter, r *http.Request) (int, error) {
	t := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(t) == 0 {
		return http.StatusBadRequest, fmt.Errorf("Missing trench")
	}
	for _, s := range t[1:] {
		if s == ".." {
			return http.StatusBadRequest, fmt.Errorf("Invalid path")
		}
	}
	trench := t[0]
	name := strings.Join(t[1:], "/")

	dir := filepath.Join(s.Root, trench)
	repo, err := OpenRepository(dir)
	if err != nil {
		return http.StatusNotFound, fmt.Errorf("Failed to open trench '%s': %w", trench, err)
	}
	defer repo.Close()

	switch r.Method {
	case http.MethodGet:
		if name == "" {
			version := r.URL.Query().Get("v")
			return s.handlePull(w, r, repo, version)
		} else {
			return s.handleReadAttachment(w, r, repo, name)
		}
	case http.MethodPut:
		return s.handleWriteAttachment(w, r, repo, name)
	case http.MethodPost:
		return s.handlePush(w, r, repo)
	default:
		return http.StatusMethodNotAllowed, fmt.Errorf("%s not allowed", r.Method)
	}
}

func (s *Server) handleReadAttachment(w http.ResponseWriter, r *http.Request, repo *Repository, name string) (int, error) {
	f, err := repo.OpenAttachment(name)
	if err != nil {
		return http.StatusNotFound, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return http.StatusInternalServerError, err
	}
	log.Printf(">> read %s (%d bytes)", name, fi.Size())
	http.ServeContent(w, r, name, fi.ModTime(), f)
	return http.StatusOK, nil
}

func (s *Server) handleWriteAttachment(w http.ResponseWriter, r *http.Request, repo *Repository, name string) (int, error) {
	if name == "" {
		return http.StatusBadRequest, fmt.Errorf("Invalid attachment name")
	}
	log.Printf(">> write %s", name)
	err := repo.WriteAttachment(name, r.Body)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("Could not write attachment '%s': %w", name, err)
	} else {
		return http.StatusOK, nil
	}
}

func (s *Server) handlePull(w http.ResponseWriter, r *http.Request, repo *Repository, version string) (int, error) {
	head := repo.Head()
	if version == head {
		return http.StatusNoContent, nil
	}

	old := make(Version)
	new, err := repo.ReadSurveys()
	if err != nil {
		return http.StatusInternalServerError, err
	}

	if version != "" {
		err := repo.Checkout(version)
		if err != nil {
			return http.StatusBadRequest, fmt.Errorf("Invalid version: %w", err)
		}
		old, err = repo.ReadSurveys()
		if err != nil {
			return http.StatusInternalServerError, err
		}
	}

	resp := PushResponse{
		Status:  StatusConflict,
		Version: head,
		Updates: diffVersions(old, new),
	}
	log.Printf("< %s (%d updates)", resp.Version, len(resp.Updates))
	return s.writeJSON(w, r, &resp)
}

func diffVersions(old, new Version) []Patch {
	var patches []Patch
	ids := old.Keys()
	ids.FormUnion(new.Keys())
	for id := range ids {
		o := old[id]
		n := new[id]
		if !o.IsEqual(n) {
			patches = append(patches, Patch{Id: id, Old: o, New: n})
		}
	}
	return patches
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request, repo *Repository) (int, error) {
	var req PushRequest
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&req)
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("Invalid sync request: %w", err)
	}

	log.Printf(">> push %s {uid: %q, username: %q, message: %q, surveys: <%d surveys>}",
		req.Head, req.UID, req.UserName, req.Message, len(req.Surveys))

	head := repo.Head()

	if head != "" {
		if req.Head == "" || req.Head != head {
			// We are not on the same version, client should pull
			_, err := s.handlePull(w, r, repo, req.Head)
			return http.StatusOK, err
		}
	}

	w.Header().Set("Content-Type", "application/json")

	missing := s.missingAttachments(repo, req.Surveys)
	if len(missing) > 0 {
		// Missing attachments
		resp := PushResponse{
			Status:  StatusMissing,
			Version: head,
			Missing: missing,
		}
		_, err := s.writeJSON(w, r, &resp)
		return http.StatusOK, err
	}

	newHead, err := repo.Commit(req.UID, req.UserName, req.Message, req.Surveys)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	resp := PushResponse{
		Status:  StatusPushed,
		Version: newHead,
	}
	return s.writeJSON(w, r, &resp)
}

func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, v interface{}) (int, error) {
	w.Header().Set("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	if r.URL.Query().Has("debug") {
		enc.SetIndent("", "  ")
	}
	err := enc.Encode(v)
	if err != nil {
		return http.StatusInternalServerError, err
	} else {
		return http.StatusOK, nil
	}
}

func (s *Server) missingAttachments(repo *Repository, surveys map[string]Survey) []string {
	var missing []string
	for _, survey := range surveys {
		name := survey["FormatImage"]
		if name == "" {
			continue
		}
		if !repo.ExistsAttachment(name) {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}
