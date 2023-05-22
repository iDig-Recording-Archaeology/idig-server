package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
)

type ListTrenchesResponse struct {
	Trenches []Trench `json:"trenches"`
}

type Trench struct {
	Name         string    `json:"name"`
	Version      string    `json:"version"`
	LastModified time.Time `json:"last_modified"`
}

func ListTrenches(w http.ResponseWriter, r *http.Request) {
	httpError := func(msg string, code int) {
		log.Printf("%s %s [%d %s]", r.Method, r.URL, code, msg)
		http.Error(w, msg, code)
	}

	vars := mux.Vars(r)
	project := vars["project"]
	if project == "" {
		httpError("Missing project", http.StatusNotFound)
		return
	}

	user, password, ok := r.BasicAuth()
	if !ok {
		httpError("Missing authorization header", http.StatusUnauthorized)
		return
	}
	if !hasAccess(project, user, password) {
		httpError("Invalid username or password", http.StatusUnauthorized)
		return
	}

	log.Printf("%s %s (%s)", r.Method, r.URL, user)

	projectDir := filepath.Join(RootDir, project)
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		httpError("Failed to list trenches", http.StatusInternalServerError)
		return
	}

	trenches := []Trench{}
	for _, e := range entries {
		b, err := NewBackend(projectDir, user, e.Name())
		if err != nil {
			continue
		}
		v, err := b.Version()
		if err != nil {
			log.Printf("Error getting version of %s", e.Name())
			continue
		}
		trench := Trench{
			Name:         e.Name(),
			Version:      v.Version,
			LastModified: v.Date,
		}
		trenches = append(trenches, trench)
	}
	resp := ListTrenchesResponse{Trenches: trenches}
	writeJSON(w, r, &resp)
}

type SyncRequest struct {
	Device      string   `json:"device"`      // Device name making the request
	Message     string   `json:"message"`     // Commit message (can be empty)
	Head        string   `json:"head"`        // Client's last sync version (can be empty)
	Preferences []byte   `json:"preferences"` // Preferences file serialized
	Surveys     []Survey `json:"surveys"`     // Surveys to be committed
}

type SyncResponse struct {
	Status      string   `json:"status"`                // One of: ok, pushed, missing, pull
	Version     string   `json:"version"`               // Current version of the server
	Preferences []byte   `json:"preferences,omitempty"` // Serialized preferences if different
	Missing     []string `json:"missing,omitempty"`     // List of missing attachments
	Updates     []Patch  `json:"updates,omitempty"`     // List of patches need to be applied on the client
}

// Sync Status
const (
	StatusOK      = "ok"      // Client is already in sync
	StatusPushed  = "pushed"  // New version committed
	StatusPull    = "pull"    // Client is in an older version and needs to update
	StatusMissing = "missing" // Some attachments are missing and need to be uploaded first
)

type Patch struct {
	Id  string `json:"id"`
	Old Survey `json:"old"`
	New Survey `json:"new"`
}

func SyncTrench(w http.ResponseWriter, r *http.Request, b *Backend) error {
	var req SyncRequest
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&req)
	if err != nil {
		return err
	}

	log.Printf("> SYNC %s %s", b.Trench, req)

	head := b.Head()

	// When our head is empty, we let the client push. This could happen if they
	// had synced before and we somehow reset our state, or when they are starting
	// from scratch.
	if head != "" && req.Head != head {
		// Ignore errors here, we just fallback to empty list
		old, _ := b.ReadSurveysAtVersion(req.Head)
		new, err := b.ReadSurveys()
		if err != nil {
			return err
		}

		// Generate patches
		var patches []Patch
		oldMap := NewSurveyMap(old)
		newMap := NewSurveyMap(new)
		for id := range oldMap.IDs().Union(newMap.IDs()) {
			oldSurvey := oldMap[id]
			newSurvey := newMap[id]
			if !oldSurvey.IsEqual(newSurvey) {
				patch := Patch{Id: id, Old: oldSurvey, New: newSurvey}
				patches = append(patches, patch)
			}
		}

		resp := SyncResponse{
			Status:  StatusPull,
			Version: head,
			Updates: patches,
		}

		oldPrefs, _ := b.ReadPreferencesAtVersion(req.Head)
		newPrefs, err := b.ReadPreferences()
		if err == nil && !bytes.Equal(oldPrefs, newPrefs) {
			resp.Preferences = newPrefs
		}

		log.Printf("< SYNC %s %s", b.Trench, resp)
		return writeJSON(w, r, &resp)
	}

	// The client is in the right version, but we need to check that we
	// have all required attachments first.

	missingAttachments := make(Set)
	for _, survey := range req.Surveys {
		for _, a := range survey.Attachments() {
			if !b.ExistsAttachment(a.Name, a.Checksum) {
				missingAttachments.Insert(a.Name)
			}
		}
	}
	if len(missingAttachments) > 0 {
		resp := SyncResponse{
			Status:  StatusMissing,
			Version: head,
			Missing: missingAttachments.Array(),
		}
		log.Printf("< SYNC %s %s", b.Trench, resp)
		return writeJSON(w, r, &resp)
	}

	newHead, err := b.WriteTrench(req.Device, req.Message, req.Preferences, req.Surveys)
	if err != nil {
		return err
	}

	resp := SyncResponse{Version: newHead}
	if newHead != head {
		resp.Status = StatusPushed
	} else {
		resp.Status = StatusOK
	}
	log.Printf("< SYNC %s %s", b.Trench, resp)
	return writeJSON(w, r, &resp)
}

func ReadAttachment(w http.ResponseWriter, r *http.Request, b *Backend) error {
	vars := mux.Vars(r)
	name := vars["name"]
	if name == "" {
		return fmt.Errorf("Missing attachment name")
	}
	checksum := r.URL.Query().Get("checksum")
	if checksum == "" {
		return fmt.Errorf("Missing attachment checksum")
	}

	data, err := b.ReadAttachment(name, checksum)
	if err != nil {
		return err
	}

	ctype := mime.TypeByExtension(filepath.Ext(name))
	if ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.WriteHeader(http.StatusOK)

	n, err := w.Write(data)
	// We can't really return any errors at this point, just report it
	if err != nil {
		log.Printf("Error sending attachment %s: %s", name, err)
	} else if n != len(data) {
		log.Printf("Incomplete write for attachment %s (%d/%d)", name, n, len(data))
	}
	return nil
}

func WriteAttachment(w http.ResponseWriter, r *http.Request, b *Backend) error {
	defer func() {
		// Drain any leftovers and close
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
	}()

	vars := mux.Vars(r)
	name := vars["name"]
	if name == "" {
		return fmt.Errorf("Missing attachment name")
	}
	checksum := r.URL.Query().Get("checksum")
	if checksum == "" {
		return fmt.Errorf("Missing attachment checksum")
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("Error uploading attachment %s/%s (%s): %w", b.Trench, name, b.User, err)
	}
	return b.WriteAttachment(name, checksum, data)
}

type ReadSurveysResponse struct {
	Version string   `json:"version"`
	Surveys []Survey `json:"surveys"`
}

func ReadSurveys(w http.ResponseWriter, r *http.Request, b *Backend) error {
	version := r.URL.Query().Get("version")
	if version == "" {
		version = b.Head()
	}
	surveys, err := b.ReadSurveysAtVersion(version)
	if err != nil {
		return err
	}

	resp := ReadSurveysResponse{
		Version: version,
		Surveys: surveys,
	}
	return writeJSON(w, r, resp)
}

func ReadSurveyVersions(w http.ResponseWriter, r *http.Request, b *Backend) error {
	vars := mux.Vars(r)
	id := vars["uuid"]
	versions, err := b.ReadAllSurveyVersions(id)
	if err != nil {
		return err
	}
	return writeJSON(w, r, versions)
}

func ListVersions(w http.ResponseWriter, r *http.Request, b *Backend) error {
	versions, err := b.ListVersions()
	if err != nil {
		return err
	}
	return writeJSON(w, r, versions)
}

func writeJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	w.Header().Set("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	if r.URL.Query().Has("debug") {
		enc.SetIndent("", "  ")
	}

	w.WriteHeader(http.StatusOK)
	// We can't really report any errors after this point
	enc.Encode(v)
	return nil
}

func (r SyncRequest) String() string {
	return fmt.Sprintf("{head: %s, device: %s, surveys: [%d surveys]}",
		Prefix(r.Head, 7), r.Device, len(r.Surveys))
}

func (r SyncResponse) String() string {
	version := Prefix(r.Version, 7)
	if version == "" {
		version = "-"
	}
	s := fmt.Sprintf("{status: %s, version: %s", r.Status, version)
	if len(r.Missing) > 0 {
		s += fmt.Sprintf(", missing: [%d attachments]", len(r.Missing))
	}
	if len(r.Preferences) > 0 {
		s += fmt.Sprintf(", preferences: <%d bytes>", len(r.Preferences))
	}
	if len(r.Updates) > 0 {
		s += fmt.Sprintf(", updates: [%d patches]", len(r.Updates))
	}
	return s + "}"
}
