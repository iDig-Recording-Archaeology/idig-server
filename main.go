package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"

	"github.com/gorilla/mux"
)

type SyncRequest struct {
	UID         string   `json:"uid"`         // User or device identifier
	UserName    string   `json:"username"`    // User name
	Message     string   `json:"message"`     // Commit message (can be empty)
	Head        string   `json:"head"`        // Client's last sync version (can be empty)
	Preferences []byte   `json:"preferences"` // Preferences file serialized
	Surveys     []Survey `json:"surveys"`     // Surveys to be committed
}

type SyncResponse struct {
	Status      string   `json:"status"`                // One of: ok, pushed, missing, conflict
	Version     string   `json:"version"`               // Current version of the server
	Preferences []byte   `json:"preferences,omitempty"` // Serialized preferences if different
	Missing     []string `json:"missing,omitempty"`     // List of missing attachments
	Updates     []Patch  `json:"updates,omitempty"`     // List of patches need to be applied on the client
}

// Sync Status
const (
	StatusOK       = "ok"       // Client is already in sync
	StatusPushed   = "pushed"   // New version committed
	StatusConflict = "conflict" // Client is in an older version
	StatusMissing  = "missing"  // Some attachments are missing and need to be uploaded first
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
			Status:  StatusConflict,
			Version: head,
			Updates: patches,
		}

		oldPrefs, _ := b.ReadPreferencesAtVersion(req.Head)
		newPrefs, err := b.ReadPreferences()
		if err != nil {
			return err
		}
		if bytes.Compare(oldPrefs, newPrefs) != 0 {
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

	newHead, err := b.WriteTrench(req.UID, req.UserName, req.Message, req.Preferences, req.Surveys)
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
		return err
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
	return fmt.Sprintf("{head: %s, uid: %s, username: %s, surveys: [%d surveys]}",
		Prefix(r.Head, 7), Prefix(r.UID, 8), r.UserName, len(r.Surveys))
}

func (r SyncResponse) String() string {
	s := fmt.Sprintf("{status: %s, version: %s", r.Status, Prefix(r.Version, 7))
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

type ServerHandler func(http.ResponseWriter, *http.Request, *Backend) error

func addRoute(r *mux.Router, method, path, rootDir string, handler ServerHandler) {
	// Wrapper function to turn handler into http.HandleFunc compatible form
	h := func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL)

		vars := mux.Vars(r)
		trench := vars["trench"]
		if trench == "" {
			http.Error(w, "Missing trench", http.StatusNotFound)
			return
		}

		b, err := NewBackend(rootDir, trench)
		if err != nil {
			msg := fmt.Sprintf("Error initializing backend for %s: %s", trench, err)
			log.Println(msg)
			http.Error(w, msg, http.StatusBadRequest)
			return
		}

		err = handler(w, r, b)
		if err != nil {
			log.Printf("ERROR %s", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	r.HandleFunc(path, h).Methods(method)
}

func main() {
	rootFlag := flag.String("r", ".", "Root dir of Git repositories")
	portFlag := flag.Int("p", 9000, "Listen on this port")
	flag.Parse()

	rootDir := *rootFlag
	r := mux.NewRouter()

	addRoute(r, "POST", "/{trench}", rootDir, SyncTrench)
	addRoute(r, "GET", "/{trench}/attachments/{name}", rootDir, ReadAttachment)
	addRoute(r, "PUT", "/{trench}/attachments/{name}", rootDir, WriteAttachment)
	addRoute(r, "GET", "/{trench}/surveys", rootDir, ReadSurveys)
	addRoute(r, "GET", "/{trench}/surveys/{uuid}/versions", rootDir, ReadSurveyVersions)
	addRoute(r, "GET", "/{trench}/versions", rootDir, ListVersions)

	addr := fmt.Sprintf(":%d", *portFlag)
	s := &http.Server{
		Addr:    fmt.Sprintf(":%d", *portFlag),
		Handler: r,
	}

	log.Printf("Listening on %s", addr)
	log.Fatal(s.ListenAndServe())
}
