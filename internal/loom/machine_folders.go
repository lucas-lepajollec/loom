package loom

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Favourite work folders per machine ("local" or a remote machine id):
// offered when choosing where a harness works, where a terminal opens, etc.

const localFoldersState = "local_folders"

func machineFolders(target string) []string {
	if target == "" || target == "local" {
		list := []string{}
		_ = getStoreJSON(bkState, localFoldersState, &list)
		return list
	}
	for _, m := range loadRemoteMachines() {
		if m.ID == target {
			return m.Folders
		}
	}
	return nil
}

func cleanMachineFolders(target string, folders []string) ([]string, error) {
	if len(folders) > 32 {
		return nil, errors.New("maximum 32 directories")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, f := range folders {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		var clean string
		if target == "" || target == "local" {
			if !filepath.IsAbs(f) {
				return nil, errors.New("absolute path required: " + f)
			}
			if info, err := os.Stat(f); err != nil || !info.IsDir() {
				return nil, errors.New("directory not found on this machine: " + f)
			}
			clean = filepath.Clean(f)
		} else {
			c, err := remoteWorkdir(f)
			if err != nil {
				return nil, err
			}
			clean = c
		}
		if !seen[clean] {
			seen[clean] = true
			out = append(out, clean)
		}
	}
	return out, nil
}

// GET ?machine=: folders. POST {machine, folders}: replace them.
func handleMachineFolders(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sendJSON(w, 200, map[string]any{"ok": true, "folders": machineFolders(r.URL.Query().Get("machine"))})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Machine string   `json:"machine"`
		Folders []string `json:"folders"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	folders, err := cleanMachineFolders(req.Machine, req.Folders)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if req.Machine == "" || req.Machine == "local" {
		err = putStoreJSON(bkState, localFoldersState, folders)
	} else {
		machines := loadRemoteMachines()
		found := false
		for i := range machines {
			if machines[i].ID == req.Machine {
				machines[i].Folders, found = folders, true
			}
		}
		if !found {
			sendJSON(w, 404, map[string]any{"ok": false, "error": "machine not found"})
			return
		}
		err = putStoreJSON(bkState, remoteMachinesState, machines)
	}
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "folders": folders})
}

// GET: this machine as the Machines page shows it.
func handleLocalMachine(w http.ResponseWriter, r *http.Request) {
	host, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	sendJSON(w, 200, map[string]any{"ok": true, "hostname": host, "user": user, "home": home, "os": map[string]string{"linux": "Linux", "darwin": "macOS", "windows": "Windows"}[runtime.GOOS], "folders": machineFolders("local")})
}
