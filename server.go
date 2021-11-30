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

type SyncRequest struct {
	UID      string            `json:"uid"`
	UserName string            `json:"username"`
	Message  string            `json:"message"`
	Head     string            `json:"head"`
	Surveys  map[string]Survey `json:"surveys,omitempty"`
}

type SyncResponse struct {
	Status      string            `json:"status"`
	Head        string            `json:"head"`
	Surveys     map[string]Survey `json:"surveys,omitempty"`
	Attachments []string          `json:"attachments,omitempty"`
}

const (
	StatusOK      = "ok"
	StatusPushed  = "pushed"
	StatusPull    = "pull"
	StatusMissing = "missing"
	StatusError   = "error"
)

type Survey map[string]string

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("> %s %s", r.Method, r.URL)
	code, err := s.serve(w, r)
	if err != nil {
		log.Printf("< %d %s", code, err)
		http.Error(w, err.Error(), code)
	} else {
		log.Printf("< 200 OK")
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
		return http.StatusInternalServerError, fmt.Errorf("Failed to open trench '%s': %w", trench, err)
	}
	defer repo.Close()

	switch r.Method {
	case http.MethodGet:
		return s.handleReadAttachment(w, r, repo, name)
	case http.MethodPut:
		return s.handleWriteAttachment(w, r, repo, name)
	case http.MethodPost:
		return s.handleSync(w, r, repo)
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

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request, repo *Repository) (int, error) {
	var req SyncRequest
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&req)
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("Invalid sync request: %w", err)
	}

	log.Printf(">> sync %s {uid: %q, username: %q, message: %q, surveys: <%d surveys>}",
		req.Head, req.UID, req.UserName, req.Message, len(req.Surveys))

	head := repo.Head()
	surveys, err := repo.ReadSurveys()
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("Failed to read surveys: %w", err)
	}

	if head != "" {
		if req.Head == "" || req.Head != head {
			// We are not on the same version, client should pull
			resp := SyncResponse{
				Status:  StatusPull,
				Head:    head,
				Surveys: surveys,
			}
			return s.writeSyncResponse(w, &resp)
		}
	}

	missing := s.missingAttachments(repo, req.Surveys)
	if len(missing) > 0 {
		// Missing attachments
		resp := SyncResponse{
			Status:      StatusMissing,
			Head:        head,
			Attachments: missing,
		}
		return s.writeSyncResponse(w, &resp)
	}

	newHead, err := repo.Commit(req.UID, req.UserName, req.Message, req.Surveys)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	if newHead == "" {
		// No changes
		resp := SyncResponse{
			Status: StatusOK,
			Head:   head,
		}
		return s.writeSyncResponse(w, &resp)
	}

	// Pushed
	resp := SyncResponse{
		Status: StatusPushed,
		Head:   newHead,
	}
	return s.writeSyncResponse(w, &resp)
}

func (s *Server) writeSyncResponse(w http.ResponseWriter, resp *SyncResponse) (int, error) {
	log.Printf("<< %s %s {surveys: <%d surveys>}", resp.Status, resp.Head, len(resp.Surveys))
	w.Header().Set("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	err := enc.Encode(resp)
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
